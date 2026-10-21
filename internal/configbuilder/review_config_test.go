package configbuilder

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"eagle/internal/bean"
	"eagle/internal/db"
	"eagle/internal/setting"
)

// --- helpers ---------------------------------------------------------------

func singBoxPath(t *testing.T) string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	p := filepath.Join(root, "sing-box", "sing-box.exe")
	if runtime.GOOS != "windows" {
		p = filepath.Join(root, "sing-box", "sing-box")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no sing-box binary at %s", p)
	}
	return p
}

// checkWithCore runs `sing-box check -c` on a generated config and returns the
// core's own complaint, if any.
func checkWithCore(t *testing.T, cfg map[string]interface{}) string {
	t.Helper()
	exe := singBoxPath(t)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := exec.Command(exe, "check", "-c", f).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func newMgr(t *testing.T) (*db.Manager, *db.Group) {
	t.Helper()
	dir := t.TempDir()
	mgr := db.NewManager(dir)
	g := &db.Group{ID: -1, FrontProxyID: -1, Name: "g"}
	if err := mgr.AddGroup(g); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	return mgr, mgr.GetGroup(g.ID)
}

func addProfile(t *testing.T, mgr *db.Manager, gid int, typ string, b bean.Bean) *db.ProxyEntity {
	t.Helper()
	e := &db.ProxyEntity{Type: typ, ID: -1, GID: gid, Bean: b}
	if err := mgr.AddProfile(e, gid); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}
	return e
}

func baseBean() bean.Bean {
	b := bean.New()
	b.Set("name", "n")
	b.Set("addr", "example.com")
	b.Set("port", 443)
	return b
}

func vmessBean() bean.Bean {
	b := baseBean()
	b.Set("id", "11111111-1111-1111-1111-111111111111")
	b.Set("aid", 0)
	b.Set("sec", "auto")
	b.Set("stream", map[string]interface{}{"net": "tcp", "sec": "tls", "sni": "example.com"})
	return b
}

func trojanBean() bean.Bean {
	b := baseBean()
	b.Set("pass", "secret")
	b.Set("stream", map[string]interface{}{"net": "tcp", "sec": "tls", "sni": "example.com"})
	return b
}

func vlessBean() bean.Bean {
	b := baseBean()
	b.Set("pass", "11111111-1111-1111-1111-111111111111")
	b.Set("flow", "")
	b.Set("stream", map[string]interface{}{"net": "tcp", "sec": "tls", "sni": "example.com"})
	return b
}

func ssBean() bean.Bean {
	b := baseBean()
	b.Set("method", "aes-256-gcm")
	b.Set("pass", "secret")
	return b
}

func hysteria2Bean() bean.Bean {
	b := baseBean()
	b.Set("password", "secret")
	b.Set("uploadMbps", 0)
	b.Set("downloadMbps", 0)
	b.Set("sni", "example.com")
	b.Set("alpn", "")
	b.Set("caText", "")
	return b
}

func tuicBean() bean.Bean {
	b := baseBean()
	b.Set("uuid", "11111111-1111-1111-1111-111111111111")
	b.Set("password", "secret")
	b.Set("congestionControl", "")
	b.Set("udpRelayMode", "")
	b.Set("heartbeat", "")
	b.Set("sni", "example.com")
	b.Set("alpn", "")
	b.Set("caText", "")
	return b
}

func snellBean() bean.Bean {
	b := baseBean()
	b.Set("psk", "secret")
	b.Set("version", 4)
	b.Set("obfsMode", "")
	b.Set("obfsHost", "")
	return b
}

func anytlsBean() bean.Bean {
	b := baseBean()
	b.Set("pass", "secret")
	b.Set("stream", map[string]interface{}{"net": "tcp", "sec": "tls", "sni": "example.com"})
	return b
}

func socksBean() bean.Bean {
	b := baseBean()
	b.Set("v", 5)
	return b
}

func hysteria1Bean() bean.Bean {
	b := baseBean()
	b.Set("auth", "secret")
	b.Set("uploadMbps", 50)
	b.Set("downloadMbps", 100)
	b.Set("sni", "example.com")
	return b
}

// --- tests -----------------------------------------------------------------

// Every protocol the app can produce must yield a config the bundled core
// accepts. The core is the only authority on what it will decode.
func TestGeneratedConfigPassesCoreCheck(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID

	cases := []struct {
		name string
		typ  string
		bean bean.Bean
	}{
		{"vmess", "vmess", vmessBean()},
		{"trojan", "trojan", trojanBean()},
		{"vless", "vless", vlessBean()},
		{"shadowsocks", "shadowsocks", ssBean()},
		{"hysteria2", "hysteria2", hysteria2Bean()},
		{"tuic", "tuic", tuicBean()},
		{"snell", "snell", snellBean()},
		{"anytls", "anytls", anytlsBean()},
		{"socks", "socks", socksBean()},
		{"hysteria1", "hysteria", hysteria1Bean()},
	}
	for _, c := range cases {
		e := addProfile(t, mgr, g.ID, c.typ, c.bean)
		res, err := BuildConfig(e, mgr, s, false)
		if err != nil {
			t.Errorf("%s: BuildConfig: %v", c.name, err)
			continue
		}
		if msg := checkWithCore(t, res.Config); msg != "" {
			t.Errorf("%s: core rejected the config:\n%s", c.name, msg)
		}
	}
}

// A mode switch reaches the config in one place only: the mapping inbound a
// first-profile naive / hysteria2 / tuic node is reached through. Under TUN a
// direct connection to such a node would fall back into the tunnel it is
// serving, so the node is reached through a local listener that dials out on
// its own.
//
// Everything else about the config is the same in both modes — and that is what
// lets a mode switch leave the node running instead of rebuilding the core
// around it. The first test below is the ordinary node (the same config in
// both modes); the second is the one that is not.
func TestReviewOrdinaryNodeBuildsTheSameConfigInBothModes(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	// The IPv6 measurement reaches the network; a test has no business doing
	// that, and the reader's own switch is the answer this test wants.
	s.IPv6AutoDetect = false

	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	s.SetVPNMode(false)
	plain, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig (plain): %v", err)
	}
	s.SetVPNMode(true)
	tunnelled, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig (tun): %v", err)
	}
	if !reflect.DeepEqual(plain.Config, tunnelled.Config) {
		t.Error("the two modes built different configs for an ordinary node, so a mode switch would have to rebuild the core")
	}
}

// The mapping inbound is the exception, and it is there for a reason: under TUN
// the node's own server address is behind the tunnel, and a direct connection
// to it would be a connection the tunnel has to carry to reach itself.
func TestReviewMappingNodeChangesWithTheMode(t *testing.T) {
	s := setting.Load(t.TempDir())
	e := &db.ProxyEntity{Type: "naive", ID: 1, GID: 1, Bean: bean.New()}
	e.Bean.Set("addr", "example.com")
	e.Bean.Set("port", 443)

	s.SetVPNMode(false)
	if got := (&builder{s: s}).needExternal(e, true); got != 2 {
		t.Errorf("without TUN a first-profile naive node is reached directly, got %d", got)
	}
	s.SetVPNMode(true)
	if got := (&builder{s: s}).needExternal(e, true); got != 1 {
		t.Errorf("under TUN a first-profile naive node is reached through the mapping inbound, got %d", got)
	}
	// A node that is not the first one is reached the same way in both modes:
	// it is not the one the tunnel has to reach.
	s.SetVPNMode(false)
	if got := (&builder{s: s}).needExternal(e, false); got != 1 {
		t.Errorf("a naive node that is not first is always reached through the mapping inbound, got %d", got)
	}
}

// A group with several nodes must produce a selector whose members all exist,
// and every outbound tag must be unique.
// TestReviewHopPortRangeTakesASinglePort pins the shape of a hopped port list.
//
// The field the subscriptions carry spells a range with a dash ("22000-27000")
// and joins several with commas; sing-box's "server_ports" is a list of colon
// ranges. A piece that names ONE port is not a range, and a bare number was
// refused with "bad port range: 443" — the whole config went down over a hop
// list that named one port.
func TestReviewHopPortRangeTakesASinglePort(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"443", []string{"443:443"}},
		{"22000-27000", []string{"22000:27000"}},
		{"1000-2000,3000-4000", []string{"1000:2000", "3000:4000"}},
		{"443,500", []string{"443:443", "500:500"}},
		// A piece that is already written the second way goes through unchanged.
		{"22000:27000", []string{"22000:27000"}},
	}
	for _, tc := range cases {
		got := hopPortRanges(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("hopPortRanges(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("hopPortRanges(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestSelectorMembersAllExist(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID

	first := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	addProfile(t, mgr, g.ID, "trojan", trojanBean())
	addProfile(t, mgr, g.ID, "vless", vlessBean())

	res, err := BuildConfig(first, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Fatalf("core rejected the config:\n%s", msg)
	}

	tags := map[string]bool{}
	for _, ob := range res.Config["outbounds"].([]interface{}) {
		m := ob.(map[string]interface{})
		tag := m["tag"].(string)
		if tags[tag] {
			t.Errorf("duplicate outbound tag %q", tag)
		}
		tags[tag] = true
	}

	sel := findOutbound(res.Config, ProxyTag)
	if sel == nil {
		t.Fatalf("no selector named %q; outbounds=%v", ProxyTag, tags)
	}
	for _, mem := range sel["outbounds"].([]interface{}) {
		if !tags[mem.(string)] {
			t.Errorf("selector names member %q which is not an outbound in the config", mem)
		}
	}
	if len(res.HotNodes) != 3 {
		t.Errorf("HotNodes = %v, want all three nodes", res.HotNodes)
	}
}

func findOutbound(cfg map[string]interface{}, tag string) map[string]interface{} {
	obs, _ := cfg["outbounds"].([]interface{})
	for _, ob := range obs {
		m, _ := ob.(map[string]interface{})
		if m["tag"] == tag {
			return m
		}
	}
	return nil
}

// A routing profile that names a DNS strategy must still produce a config the
// core accepts: the strategy lands on the DNS rule that picks the server.
func TestDNSStrategyInRule(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	s.Routing().RemoteDNSStrategy = "ipv4_only"
	s.Routing().DirectDNSStrategy = "prefer_ipv4"

	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	res, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Errorf("core rejected the config:\n%s", msg)
	}
}

// The same for the outbound domain strategy, which lands on the route's
// default_domain_resolver.
func TestOutboundDomainStrategy(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	s.Routing().OutboundDomainStrategy = "prefer_ipv6"

	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	res, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Errorf("core rejected the config:\n%s", msg)
	}
}

// The routing card's "domain strategy" lands on the resolve route action.
func TestDomainStrategyResolveAction(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	s.Routing().DomainStrategy = "prefer_ipv4"

	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	res, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Errorf("core rejected the config:\n%s", msg)
	}
}

// Is the legacy DNS-rule strategy rejected on its own, or only in combination
// with the query_type rule the builder always writes?
func TestLegacyDNSStrategyAlone(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	s.Routing().RemoteDNSStrategy = "ipv4_only"

	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	res, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	// drop every DNS rule that carries a query_type
	dns := res.Config["dns"].(map[string]interface{})
	rules := dns["rules"].([]interface{})
	kept := []interface{}{}
	for _, r := range rules {
		m := r.(map[string]interface{})
		if _, has := m["query_type"]; has {
			continue
		}
		kept = append(kept, m)
	}
	dns["rules"] = kept
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Errorf("core rejected the config (legacy strategy alone):\n%s", msg)
	}
}

// Where the address family of a DNS query lives in sing-box 1.14.2.
//
// The candidates were measured against the bundled core, one config each:
//
//   - the client's own `dns.strategy` — accepted;
//   - the per-rule `strategy` (the rule action's fields are flattened onto the
//     rule) — accepted on its own, but it cannot live in the same DNS
//     configuration as a rule that matches by query_type;
//   - a rule item, `dns.rules[].rule_items` — NOT accepted: that is the 1.16
//     form, and 1.14.2 answers "unknown field rule_items";
//   - a per-server `dns.servers[].strategy` — NOT accepted: 1.14 has no such
//     field, and the core answers "unknown field strategy".
//
// This holds the two forms that DO work down, so a core upgrade that takes one
// of them away is noticed here rather than in a config the reader cannot start.
func TestReviewDNSStrategyForms(t *testing.T) {
	// The client's own strategy.
	cfg := map[string]interface{}{
		"dns": map[string]interface{}{
			"strategy": "ipv4_only",
			"servers": []interface{}{
				map[string]interface{}{"type": "udp", "server": "1.1.1.1", "tag": "r"},
			},
		},
		"outbounds": []interface{}{
			map[string]interface{}{"type": "direct", "tag": "proxy"},
		},
	}
	if msg := checkWithCore(t, cfg); msg != "" {
		t.Errorf("core rejected the client-level strategy form:\n%s", msg)
	}

	// The per-rule strategy — the action's fields, flattened onto the rule.
	cfg2 := map[string]interface{}{
		"dns": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{"type": "udp", "server": "1.1.1.1", "tag": "r"},
			},
			"rules": []interface{}{
				map[string]interface{}{
					"domain_suffix": []interface{}{"example.com"},
					"action":        "route",
					"server":        "r",
					"strategy":      "ipv4_only",
				},
			},
		},
		"outbounds": []interface{}{
			map[string]interface{}{"type": "direct", "tag": "proxy"},
		},
	}
	if msg := checkWithCore(t, cfg2); msg != "" {
		t.Errorf("core rejected the per-rule strategy form:\n%s", msg)
	}
}

// And the two forms the bundled core does NOT take. They are the natural
// places to look for the strategy — the rule item is what sing-box 1.16 uses,
// and a strategy on the server is what the settings ask for — and a reader who
// reads the migration notes would reach for both. If a future core takes one
// of them, this test says so, and the builder can move to it.
func TestReviewDNSStrategyFormsTheCoreDoesNotTake(t *testing.T) {
	ruleItems := map[string]interface{}{
		"dns": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{"type": "udp", "server": "1.1.1.1", "tag": "r"},
			},
			"rules": []interface{}{
				map[string]interface{}{
					"domain_suffix": []interface{}{"example.com"},
					"server":        "r",
					"rule_items": []interface{}{
						map[string]interface{}{"strategy": "ipv4_only"},
					},
				},
			},
		},
		"outbounds": []interface{}{
			map[string]interface{}{"type": "direct", "tag": "proxy"},
		},
	}
	if msg := checkWithCore(t, ruleItems); msg == "" {
		t.Errorf("the bundled core now takes the rule_items form; the builder may move to it")
	}

	serverStrategy := map[string]interface{}{
		"dns": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{"type": "udp", "server": "1.1.1.1", "tag": "r", "strategy": "ipv4_only"},
			},
		},
		"outbounds": []interface{}{
			map[string]interface{}{"type": "direct", "tag": "proxy"},
		},
	}
	if msg := checkWithCore(t, serverStrategy); msg == "" {
		t.Errorf("the bundled core now takes a per-server strategy; the builder may move to it")
	}
}

// The TUN front-end's own config must be accepted by the core as well.
func TestTunFrontConfigPassesCoreCheck(t *testing.T) {
	s := setting.Load(t.TempDir())
	cfg := BuildTunFront(s)
	if msg := checkWithCore(t, cfg); msg != "" {
		t.Errorf("core rejected the tun front config:\n%s", msg)
	}
}

// The preset routing profiles ("Bypass LAN and Mainland" and the country ones)
// are what the settings card writes; a config built from one must be accepted.
func TestPresetRoutingPassesCoreCheck(t *testing.T) {
	for preset := 1; preset <= 3; preset++ {
		mgr, g := newMgr(t)
		s := setting.Load(t.TempDir())
		s.CurrentGroup = g.ID
		r := setting.PresetRouting(preset)
		live := s.Routing()
		live.DirectIP = r.DirectIP
		live.DirectDomain = r.DirectDomain
		live.BlockDomain = r.BlockDomain
		e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
		res, err := BuildConfig(e, mgr, s, false)
		if err != nil {
			t.Fatalf("preset %d: BuildConfig: %v", preset, err)
		}
		if msg := checkWithCore(t, res.Config); msg != "" {
			t.Errorf("preset %d: core rejected the config:\n%s", preset, msg)
		}
	}
}

// The single-process TUN config (BuildVPNConfig) carries a tun inbound and the
// fakeip server; it must be accepted too.
func TestVPNConfigPassesCoreCheck(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	s.FakeDNS = true
	s.VPNIPv6 = true
	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	res, err := BuildVPNConfig(e, mgr, s)
	if err != nil {
		t.Fatalf("BuildVPNConfig: %v", err)
	}
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Errorf("core rejected the vpn config:\n%s", msg)
	}
}

// A chain profile carries no address of its own; the selector's membership test
// must still let it in.
func TestChainJoinsSelector(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID

	entry := addProfile(t, mgr, g.ID, "trojan", trojanBean())
	exit := addProfile(t, mgr, g.ID, "vmess", vmessBean())

	ch := bean.New()
	ch.Set("name", "chain")
	ch.Set("list", []interface{}{entry.ID, exit.ID})
	chainEnt := addProfile(t, mgr, g.ID, "chain", ch)

	first := addProfile(t, mgr, g.ID, "vmess", vmessBean())

	res, err := BuildConfig(first, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if msg := checkWithCore(t, res.Config); msg != "" {
		t.Fatalf("core rejected the config:\n%s", msg)
	}
	sel := findOutbound(res.Config, ProxyTag)
	if sel == nil {
		t.Fatalf("no selector")
	}
	found := false
	for _, mem := range sel["outbounds"].([]interface{}) {
		if mem.(string) == res.HotNodes[chainEnt.ID] {
			found = true
		}
	}
	if !found {
		t.Errorf("the chain node is not a member of the selector; members=%v hotNodes=%v",
			sel["outbounds"], res.HotNodes)
	}
}

// The builder's own rule order: the sniff rule has to come first.
//
// sing-box only knows the domain after the sniff action has run, and the rules
// that stood in front of the sniff rule are passed over and never tried again.
// The builder used to put the reader's own rules there — they were "priority"
// — so every one of them matched nothing, and the core's log said the
// connection went to the fallback outbound. The measurement is in
// TestRuleOrderSniffVsCustom; this holds the builder's side of it.
func TestReviewBuilderPutsTheSniffRuleFirst(t *testing.T) {
	mgr, g := newMgr(t)
	s := setting.Load(t.TempDir())
	s.CurrentGroup = g.ID
	// A reader's own rule, the kind the routing card writes.
	s.Routing().Custom = `{"rules":[{"domain_suffix":["example.com"],"outbound":"proxy"}]}`

	e := addProfile(t, mgr, g.ID, "vmess", vmessBean())
	res, err := BuildConfig(e, mgr, s, false)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	route, ok := res.Config["route"].(map[string]interface{})
	if !ok {
		t.Fatalf("the config carries no route")
	}
	rules, ok := route["rules"].([]interface{})
	if !ok {
		t.Fatalf("the route carries no rules")
	}
	sniffAt, domainAt := -1, -1
	for i, r := range rules {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if m["action"] == "sniff" && sniffAt < 0 {
			sniffAt = i
		}
		if _, has := m["domain_suffix"]; has && domainAt < 0 {
			domainAt = i
		}
	}
	if sniffAt < 0 {
		t.Fatalf("the config carries no sniff rule")
	}
	if domainAt < 0 {
		t.Fatalf("the reader's own rule is not in the config at all")
	}
	if domainAt < sniffAt {
		t.Errorf("the reader's own domain rule sits at %d, in front of the sniff rule at %d: "+
			"sing-box does not know the domain until the sniff has run, and the rules in front "+
			"of the sniff rule are never tried again, so that rule could never match",
			domainAt, sniffAt)
	}
}

// --- the rule-order experiment ---------------------------------------------

// holdListener accepts connections and keeps them open, so a connection that
// took a given outbound stays visible.
func holdListener(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c
		}
	}()
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *strings.Builder
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// runCoreOrder starts sing-box with the given config, sends one plain HTTP
// request through its mixed inbound with a Host header, and returns the log.
func runCoreOrder(t *testing.T, cfg string, port int) string {
	t.Helper()
	exe := singBoxPath(t)
	f := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(f, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := exec.Command(exe, "run", "-c", f)
	var mu sync.Mutex
	var sb strings.Builder
	cmd.Stderr = &lockedWriter{mu: &mu, w: &sb}
	cmd.Stdout = &lockedWriter{mu: &mu, w: &sb}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	for i := 0; i < 60; i++ {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Absolute-form request to an IP: the domain is NOT known from the request
	// line, so only the sniffer can recover it from the Host header.
	fmt.Fprintf(conn, "GET http://1.2.3.4/ HTTP/1.1\r\nHost: example.com\r\n\r\n")
	time.Sleep(1500 * time.Millisecond)
	conn.Close()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return sb.String()
}

// Which rule wins when a domain-matching custom rule sits in front of the sniff
// rule? sing-box 1.13+ only knows the domain after the sniff action has run,
// and the rules before the sniff rule are not looked at again afterwards.
//
// Measured against the bundled core (1.14.2):
//
//	sniff rule FIRST:  match[0] => sniff, and then match[1]
//	                   domain_suffix=example.com => route(custom-hit): the
//	                   custom rule IS reached, because the domain is known by
//	                   the time the rules after the sniff one are tried;
//	custom rule FIRST: match[1] => sniff, and the connection goes to the
//	                   fallback: the custom rule sat at index 0, was passed
//	                   over — the domain was not known yet — and is never
//	                   tried again.
//
// So the sniff rule has to come first, and the builder puts it at the top of
// the rule list (buildRoute). This test holds that order down: a builder that
// moved the custom rules above the sniff rule would break every reader's own
// rules, and the symptom would be "my rules do nothing".
func TestRuleOrderSniffVsCustom(t *testing.T) {
	holdListener(t, 19000)
	mk := func(port int, sniffFirst bool) string {
		sniff := `{"inbound":["mixed-in"],"action":"sniff"}`
		custom := `{"domain_suffix":["example.com"],"outbound":"custom-hit"}`
		order := []string{custom, sniff}
		if sniffFirst {
			order = []string{sniff, custom}
		}
		return fmt.Sprintf(`{
  "log": {"level": "debug"},
  "inbounds": [{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":%d}],
  "outbounds": [
    {"type":"socks","tag":"custom-hit","server":"127.0.0.1","server_port":19000},
    {"type":"direct","tag":"fallback"}
  ],
  "route": {"rules": [%s], "final": "fallback"}
}`, port, strings.Join(order, ","))
	}

	logA := runCoreOrder(t, mk(2081, false), 2081)
	logB := runCoreOrder(t, mk(2082, true), 2082)

	pick := func(log string) string {
		for _, line := range strings.Split(log, "\n") {
			if strings.Contains(strings.ToLower(line), "match[") {
				return strings.TrimSpace(line)
			}
		}
		return "<no match line>"
	}
	t.Logf("custom rule BEFORE sniff: %s", pick(logA))
	t.Logf("sniff rule BEFORE custom: %s", pick(logB))

	const reached = "outbound/socks[custom-hit]"
	if !strings.Contains(logB, reached) {
		t.Errorf("with the sniff rule in front the connection did not reach custom-hit:\n%s", logB)
	}
	if strings.Contains(logA, reached) {
		t.Errorf("with the custom rule in front of the sniff rule the connection reached custom-hit "+
			"as well; the domain is not known before the sniff, so it cannot — and the rules before "+
			"the sniff rule are not tried again:\n%s", logA)
	}
}
