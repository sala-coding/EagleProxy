package main

import (
	"eagle/internal/core"
	"eagle/internal/util"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ExitIP reports the address the world sees this machine as while a node is up — the
// one number that answers "where do I look like I am from" — together with the country
// that address is registered in, as the code the bar draws its flag from.
//
// This is not the node's own address. The address in the profile says which server the
// traffic is handed to; this says what that server makes the traffic look like when it
// comes out the other side, and the two are different numbers, countries and all (a node
// in Frankfurt can exit in Singapore).
//
// Asked through the node rather than around it, by the core's own mixed inbound in
// whichever mode is up: asked directly it would go out unproxied and answer with the
// reader's own address, which is exactly the number this is not. (See exitClient for
// why the port answers under TUN as well as under the system proxy.)
//
// A map rather than a struct, and empty rather than an error when there is nothing to
// ask: the bar has a corner for this and nothing to say about failures (see GetAbout for
// the same arrangement). Empty also covers the first seconds after a start, when the
// core is up but the tunnel is not carrying anything yet.
func (a *App) ExitIP() map[string]string {
	client, ok := a.exitClient()
	if !ok {
		return nil
	}
	// The same backoff as the corner's poll (see exitProbeDue): this is the reading
	// the panel asks for, and the panel is opened by a person who wants the truth
	// now. What it must not do is be the reason the corner's own poll keeps asking
	// a node that is down.
	if !a.exitProbeDue() {
		return nil
	}
	// One flight at a time, the same gate the corner's poll keeps (see
	// ExitAddress): the panel's ask and a timer tick could otherwise run two
	// probes at once, each writing its own answer — and the slower one, which
	// started first, landed last and was then handed out as fresh.
	a.exitProbeMu.Lock()
	if a.exitProbeBusy {
		a.exitProbeMu.Unlock()
		return a.exitProbeCached()
	}
	a.exitProbeBusy = true
	a.exitProbeMu.Unlock()
	defer func() {
		a.exitProbeMu.Lock()
		a.exitProbeBusy = false
		a.exitProbeMu.Unlock()
	}()
	ip := util.AskIP(client)
	if ip == "" {
		a.noteExitFailure()
		a.noteExitFailLog()
		return nil
	}
	a.noteExitSuccess()
	// One line per read. The corner shows the answer and discards it, so without this
	// there is no record of when it was asked or what came back — and that is exactly what
	// tells a corner that is stale (the request never ran) apart from one that is wrong
	// (it ran and got the old answer).
	a.logf("[exitip] " + ip)
	// Three questions about the same address, asked at once: the country it is
	// registered in, what the address itself is (ASN, organisation, region, the purity
	// score), and the domain and ISP behind its ASN. They used to be asked one after
	// another, so the first reading after a start paid their sum — which is the several
	// seconds the corner spent empty. Every one of them is best-effort: a failure costs
	// the panel a field, never the address.
	info := map[string]string{"ip": ip}
	details, whois := map[string]string{}, map[string]string{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.mergeExitDetails(client, ip, details) }()
	go func() { defer wg.Done(); a.mergeWhois(client, ip, whois) }()
	cc := a.ipCountry(client, ip)
	wg.Wait()
	if cc != "" {
		info["cc"] = cc
	}
	for k, v := range details {
		info[k] = v
	}
	for k, v := range whois {
		if _, seen := info[k]; !seen {
			info[k] = v
		}
	}
	// The next poll is the same question: let it have this answer rather than opening a
	// second set of connections through the node for it.
	a.exitProbeStore(info)
	return info
}

// ExitAddress reports only the two things the status bar draws — the address the world
// sees this machine as, and the country it is registered in — and it is asked on a timer.
//
// Split from ExitIP over what the two cost. This one is a single request for the address
// plus a country lookup that is already cached per address (see ipCountry). The full
// reading adds an ASN, a registrar and a purity score from two more services, which is the
// right price for a panel the reader opens and the wrong one for a corner that refreshes
// itself: those requests go out through the tunnel, and several of them every second is
// the reader's bandwidth and somebody's quota spent on fields nothing on screen shows.
//
// Silent on failure, deliberately. A poll that comes back empty is not news — the corner
// keeps what it has — and a line per attempt would fill the log with the same sentence.
func (a *App) ExitAddress() map[string]string {
	client, ok := a.exitClient()
	if !ok {
		return nil
	}
	// A node that is not carrying traffic is not asked again straight away: the
	// answer cannot have changed, and the ask is a request to each of several echo
	// services, each of which hangs until its own timeout, through a tunnel that is
	// not moving (see exitProbeDue).
	if !a.exitProbeDue() {
		return nil
	}
	// A warm-up that has just answered is the answer this poll came for: it was asked
	// after the tunnel was ready, and asking the same question again a second later is
	// another round trip through the node for nothing.
	if r := a.exitProbeCached(); r != nil {
		return r
	}
	// One flight at a time (see exitProbeBusy): the poll is every two seconds and a
	// tunnel that is still coming up can hold a request for its whole timeout, so the
	// asks used to queue up behind one another and the corner showed the oldest answer
	// last. An ask that arrives during a flight answers "nothing new"; the corner keeps
	// what it has for one beat, which is what it does with any other failure.
	a.exitProbeMu.Lock()
	if a.exitProbeBusy {
		a.exitProbeMu.Unlock()
		return nil
	}
	a.exitProbeBusy = true
	a.exitProbeMu.Unlock()
	defer func() {
		a.exitProbeMu.Lock()
		a.exitProbeBusy = false
		a.exitProbeMu.Unlock()
	}()
	ip := util.AskIP(client)
	if ip == "" {
		a.noteExitFailure()
		return nil
	}
	a.noteExitSuccess()
	r := map[string]string{"ip": ip, "cc": a.ipCountry(client, ip)}
	a.exitProbeStore(r)
	return r
}

// exitProbeFresh is how long a warm-up's answer is handed out as the current one. It
// only has to cover one turn of the window's two-second poll.
const exitProbeFresh = 2 * time.Second

// exitProbeCached answers the last successful probe, when it is still fresh.
func (a *App) exitProbeCached() map[string]string {
	a.exitProbeMu.Lock()
	defer a.exitProbeMu.Unlock()
	if a.exitProbeLast == nil || time.Since(a.exitProbeAt) > exitProbeFresh {
		return nil
	}
	return a.exitProbeLast
}

// exitProbeStore remembers an answer. The map is handed out as it was stored and is
// never mutated afterwards, so a reader cannot be shown a half-written one.
func (a *App) exitProbeStore(r map[string]string) {
	a.exitProbeMu.Lock()
	a.exitProbeLast, a.exitProbeAt = r, time.Now()
	a.exitProbeMu.Unlock()
}

// exitProbeClear forgets the cached answer — it belonged to the node that answered it.
func (a *App) exitProbeClear() {
	a.exitProbeMu.Lock()
	a.exitProbeLast, a.exitProbeAt = nil, time.Time{}
	// And the failure count: the next node is a different story, and this one's
	// ending is not to be told twice.
	a.exitFailStreak, a.exitFailTold = 0, false
	a.exitFailAt, a.exitFailLoggedAt = time.Time{}, time.Time{}
	a.exitProbeMu.Unlock()
}

// exitFailThreshold is how many probes in a row have to come back empty before the
// reader is told the node is not carrying traffic.
//
// One is not enough: the echo services are other people's servers, and one of them
// being unreachable is a fact about them. Two in a row, each asked through the node
// and each against several services at once, is a fact about the node.
const exitFailThreshold = 2

// exitFailBackoff is how long the exit probe waits after one came back empty.
//
// A node that is not carrying traffic does not need to be asked every two seconds:
// the answer will be the same, and the ask is not free — it is a request to each of
// several echo services, each of which hangs until its own timeout, through a tunnel
// that is not moving. A corner that refreshes itself twice a second has no use for
// an answer that cannot change.
const exitFailBackoff = 15 * time.Second

// exitProbeDue says whether the exit probe may be asked now.
//
// The corner's own poll comes every two seconds, and that is the right cadence for
// a node that is working. After a probe has come back empty the answer cannot be
// different for a while, so the next one waits.
func (a *App) exitProbeDue() bool {
	a.exitProbeMu.Lock()
	defer a.exitProbeMu.Unlock()
	if a.exitFailStreak == 0 {
		return true
	}
	return time.Since(a.exitFailAt) >= exitFailBackoff
}

// noteExitFailure counts a probe that came back empty, and says so once the node has
// failed enough of them.
//
// The reader's own machine is where this cannot be seen. The corner keeps the last
// address it had, the proxy answers on its port, and the only sign is that nothing
// loads — which reads as "the app is broken" rather than "this node is dead". A
// node that cannot carry traffic is worth a sentence, said once and not on every
// poll.
func (a *App) noteExitFailure() {
	a.exitProbeMu.Lock()
	a.exitFailStreak++
	a.exitFailAt = time.Now()
	streak := a.exitFailStreak
	already := a.exitFailTold
	if streak >= exitFailThreshold {
		a.exitFailTold = true
	}
	a.exitProbeMu.Unlock()
	if streak < exitFailThreshold || already {
		return
	}
	a.notifyMessage("这个节点连不通：出口探测连续失败，请换一个节点", true)
}

// exitFailLogGap is how long the log stays quiet after a probe came back empty.
//
// The corner is asked every two seconds, and a node that is down answers nothing
// every time. A line per attempt is a log that says the same thing fifty times
// and buries the lines that are about something else.
const exitFailLogGap = 30 * time.Second

// noteExitFailLog writes the failure line, unless the last one is recent enough
// to still be standing for this one. It says whether it wrote.
func (a *App) noteExitFailLog() bool {
	a.exitProbeMu.Lock()
	last := a.exitFailLoggedAt
	a.exitFailLoggedAt = time.Now()
	a.exitProbeMu.Unlock()
	if !last.IsZero() && time.Since(last) < exitFailLogGap {
		return false
	}
	a.logf("[exitip] 没有服务回答，角落保持上一次的地址")
	return true
}

// noteExitSuccess forgets the failures: the node is answering, and the streak that
// led to telling the reader is over.
func (a *App) noteExitSuccess() {
	a.exitProbeMu.Lock()
	a.exitFailStreak, a.exitFailTold = 0, false
	a.exitProbeMu.Unlock()
}

// dropExitConns puts the pooled probe connections away. A keep-alive connection is bound
// to the outbound it was opened through, and a hot switch only moves the selector: the
// old connections keep answering from the old node until something closes them.
func (a *App) dropExitConns() {
	a.exitMu.Lock()
	c := a.exitHTTP
	a.exitMu.Unlock()
	if c != nil {
		c.CloseIdleConnections()
	}
}

// warmExitProbe is the start's own first question: "what address does the world see me
// as" — asked as soon as the mixed inbound is listening, with the answer kept.
//
// Why it exists. The first probe after a start is the expensive one: the proxy hop, the
// tunnel to the node and the TLS handshake to the echo service are all cold, and the
// window asks the moment the running id moves — which is before the inbound is even
// listening. That first ask fails, the retry two seconds later pays the whole handshake,
// and the corner sits empty for it. Asking here instead — after a short wait for the
// port, on the same client the poll uses — leaves the connection in the pool and the
// answer in the cache, so the retry reuses both.
//
// Best-effort in every direction: no mode up, no port, a node that cannot carry it — the
// function returns and the poll does what it always did.
func (a *App) warmExitProbe() {
	if a.quitting.Load() {
		return
	}
	client, ok := a.exitClient()
	if !ok {
		return
	}
	s := a.curSetting()
	if s == nil {
		return
	}
	host := s.InboundAddress
	if host == "" || host == "0.0.0.0" || host == "::" {
		// A wildcard is where the inbound listens, not an address to dial.
		host = "127.0.0.1"
	}
	if s.InboundSocksPort <= 0 {
		return
	}
	addr := net.JoinHostPort(host, strconv.Itoa(s.InboundSocksPort))
	deadline := time.Now().Add(3 * time.Second)
	for {
		if a.quitting.Load() {
			return
		}
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
	ip := util.AskIP(client)
	if ip == "" {
		return
	}
	// Address only: the country is a flag the corner can draw a beat later, and asking
	// it here would keep three more connections busy while the first real poll arrives.
	a.exitProbeStore(map[string]string{"ip": ip})
	a.logf("[exitip] 预热完成: " + ip)
}

// exitClient builds the client that asks the world what address it sees, and reports
// whether there is any point in asking at all.
//
// Asked through the node rather than around it, and since the split there is exactly
// one door for that: the core's own mixed inbound. The main core runs the plain config
// in whichever mode is up (the TUN front-end is a separate process that holds nothing
// but the adapter, see BuildVPNConfig's note — the single-core shape with no inbound is
// dead code), the inbound listens under both, and loopback is not something the
// adapter's routes capture. So a request handed to the port answers with the node's
// exit under TUN as surely as under the system proxy, and how to ask stops depending
// on the machine's routing at all.
//
// It used to be asked straight out under TUN — "the whole machine is already routed, a
// direct request is a request through the node" — and the evidence for that routing was
// the running front-end process. The process outlives the routing it stands for: a
// switch from TUN to the system proxy hands the routes back first and takes the
// front-end down last, and every read inside that window went out unproxied and answered
// with the reader's own address and its own flag — a flash on every switch, and a
// permanent lie whenever the teardown failed. Whether to ask still reads all three
// pieces of evidence (core up, mode flag, front-end alive — the flag is runtime-only and
// a reload can clear it while the capture is up, see InheritRuntime); how to ask is the
// port, always. With the core down there is nothing to ask through: silence, which is
// the corner keeping what it showed rather than being told something new and wrong.
func (a *App) exitClient() (*http.Client, bool) {
	if a.curSetting() == nil {
		return nil, false
	}
	if a.RunningProfile() == core.StoppedID {
		return nil, false
	}
	frontEnd := false
	if pid, _ := a.vpnState(); pid > 0 {
		frontEnd = true
	}
	if !frontEnd && !a.curSetting().VPNMode() && !a.curSetting().SPModeSystemProxy() {
		// No mode on and no front-end standing: a request through the port would
		// answer with the node's exit, but the machine is not using that node for
		// anything, and a corner that shows an exit nobody rides is its own kind of
		// lie. Nothing to ask.
		return nil, false
	}
	host := a.curSetting().InboundAddress
	if host == "" {
		host = "127.0.0.1"
	}
	port := a.curSetting().InboundSocksPort
	if port <= 0 {
		return nil, false
	}
	// The mixed inbound speaks HTTP as well as SOCKS, so its address can be handed
	// to the transport as an HTTP proxy as it stands.
	u := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	key := u.String()
	// Five seconds, not eight: every service is now asked at once (see util.AskIP), so
	// this bounds one flight rather than every service in turn. A tunnel that cannot
	// carry the probe in five seconds answers nothing useful at eight, and the extra
	// three were the difference between a corner that filled in on the first restart
	// attempt and one that needed two.
	client := &http.Client{Timeout: 5 * time.Second}
	client.Transport = &http.Transport{
		Proxy:               http.ProxyURL(u),
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	// The poll calls this on a timer; reusing the built client keeps its
	// keep-alive pool on one handle instead of parking a new transport's idle
	// sockets behind every call.
	a.exitMu.Lock()
	defer a.exitMu.Unlock()
	if a.exitHTTP == nil || a.exitKey != key {
		a.exitKey = key
		a.exitHTTP = client
	}
	return a.exitHTTP, true
}

// mergeExitDetails adds what IPPure's public endpoint says about an address the reader
// is already looking at: the ASN and its organisation, the region, city, timezone, post
// code and coordinates, the purity score the site draws as a band, and whether the
// address is residential and native.
//
// Enrichment, not foundation: everything the bar shows without it is already in info,
// and a failure here is dropped on the floor.
//
// The endpoint is marked "in testing, may change" by its own page, so every field is
// read defensively — absent, null, or the wrong type all land as an empty string.
func (a *App) mergeExitDetails(client *http.Client, ip string, info map[string]string) {
	resp, err := client.Get("https://my.ippure.com/v1/info")
	if err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	resp.Body.Close()
	if err != nil {
		return
	}
	var raw map[string]interface{}
	if json.Unmarshal(data, &raw) != nil || asString(raw["ip"]) != ip {
		// answered for a different address than the one asked about, or answered with
		// something that is not this API: the panel falls back to what it already has
		return
	}
	if asn := asString(raw["asn"]); asn != "" && asn != "0" {
		info["asn"] = "AS" + asn
		if org := asString(raw["asOrganization"]); org != "" {
			info["asOrg"] = org
		}
	}
	if c := asString(raw["country"]); c != "" {
		info["country"] = c
	}
	for key, field := range map[string]string{
		"region": "region", "city": "city", "timezone": "timezone",
		"postalCode": "postal", "latitude": "lat", "longitude": "lon",
	} {
		if v := asString(raw[key]); v != "" {
			info[field] = v
		}
	}
	if f, ok := raw["fraudScore"].(float64); ok {
		info["purity"] = strconv.Itoa(int(f))
	}
	if r, ok := raw["isResidential"].(bool); ok && r {
		info["residential"] = "1"
	}
	if b, ok := raw["isBroadcast"].(bool); ok && b {
		info["broadcast"] = "1"
	}
}

// mergeWhois adds the domain and ISP behind the address's ASN, from a second source.
// IPPure names the organisation the ASN is registered to; ipwho.is also carries the
// domain that organisation answers on — the one line of the panel the first source
// cannot say. Same best-effort contract as above.
func (a *App) mergeWhois(client *http.Client, ip string, info map[string]string) {
	resp, err := client.Get("https://ipwho.is/" + url.PathEscape(ip))
	if err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if err != nil {
		return
	}
	var body struct {
		Connection struct {
			Domain string `json:"domain"`
			ISP    string `json:"isp"`
		} `json:"connection"`
	}
	if json.Unmarshal(data, &body) != nil {
		return
	}
	if d := strings.TrimSpace(body.Connection.Domain); d != "" {
		info["asDomain"] = d
	}
	if s := strings.TrimSpace(body.Connection.ISP); s != "" {
		info["isp"] = s
	}
}

// asString reads a JSON value as a trimmed string, accepting numbers as well as text —
// IPPure returns the coordinates as strings but the ASN as a number.
func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

// Country codes by address, so the status bar's lookup happens once per node rather than
// once per render. Failures are cached too: a node is asked about once, and a bar that
// retried on every render would spend the reader's quota on nothing.
var (
	ccMu    sync.Mutex
	ccCache = map[string]string{}
)

// ipCountry reports the two-letter code of the country an address is registered in, or
// "" when that cannot be found.
//
// The code is what the front end draws as a flag: the pictures live in
// frontend/public/fa/flag, one file per country, taken from the flag-icons set (see the
// note beside them). A code is data and a flag is artwork, which is why this side stops
// at the code rather than composing one out of letters — the letters only look like a
// flag in a font that has rules for them, and the window's own font does not.
//
// Asked of public services over the open internet (see util.AskCountry, which tries
// several in turn and keeps the first code that answers) because the alternative is
// shipping a geo database of tens of megabytes, and this project deliberately has none
// (it uses sing-box's rule sets for routing instead, see addRuleSet). Only the node's
// address is sent to them, never the reader's own.
//
// The client is the caller's rather than one made here, so that the lookup travels the
// same way the address it is about did: straight out under TUN, through the core under
// the system proxy. A lookup that went its own way would be asking from a network that
// may not be able to reach any of these services at all.
func (a *App) ipCountry(client *http.Client, ip string) string {
	if ip == "" {
		return ""
	}
	ccMu.Lock()
	if v, ok := ccCache[ip]; ok {
		ccMu.Unlock()
		return v
	}
	ccMu.Unlock()

	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	// One service used to be enough. It answered nothing but a timeout from the machine
	// this was written on, and a country that cannot be looked up is a flag that never
	// appears — which is what a bar with an empty corner looks like.
	cc := util.AskCountry(client, ip)

	ccMu.Lock()
	ccCache[ip] = cc
	ccMu.Unlock()
	return cc
}
