package main

// ncsi.go — the wake-up the system's own connectivity probe needs.
//
// Windows asks every interface on its own whether it has the internet: it resolves
// www.msftconnecttest.com with that interface's own resolver, and then fetches a fixed
// URL from it. The question is short on time, and an interface that loses it is not
// asked again for minutes — the passive backoff. That backoff is what put a tunnel
// that was carrying the machine's traffic on the tray's "无法连接到 Internet" for all
// of it.
//
// The TUN adapter is born with no routes and no resolver, so its first question is
// lost before it could be won. The app wakes the system once the takeover is in
// place (see refreshSystemNetworkState), but a wake that arrives before the takeover
// can answer is a question the system loses again: the very probe that would have
// said "this adapter carries the machine's traffic" is the one that says it does not,
// and the minutes that follow are the price.
//
// So the wake asks the question first. It is the system's own question — the same
// name, put to the same resolver, the way the system will put it — and the stack
// hears about the change only once the answer has come back.
//
// Every wait here is on the wake's own path, which is to say on the reader's path:
// the tray badge is what they are watching. So each one is the shortest that still
// separates "answered" from "not", and the slow path — a takeover that is not ready —
// gives up in about two seconds instead of six and lets the system look anyway.

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/crashlog"
	"eagle/internal/dnstest"
	"eagle/internal/dnsx"
	"eagle/internal/tunfast"
)

// The names the system's connectivity probe asks about. The first is the host whose
// address the probe fetches from; the second is the one whose shape in a resolver's
// answer is the system's own check that the resolver works.
const (
	nCSIProbeHost = "www.msftconnecttest.com"
	nCSIDNSHost   = "dns.msftncsi.com"
)

// nCSIReadyWait is how long the wake may wait for the takeover to answer the system's
// question.
//
// It is bounded, because the wait is a courtesy to the system and not a condition on
// it: a takeover that never answers must not keep the stack from hearing that the
// picture moved. Past this bound the wake goes out as it always did.
//
// The bound used to be three seconds, on the reasoning that a first answer needs time
// to arrive. The routes are taken only after the core's inbound answers (see
// takeTunRoutes), so the takeover is already able to resolve by the time this runs;
// the wait is for the adapter's own resolver, which SelfDNS has just written. Two
// seconds covers that on a slow machine, and a takeover that is not ready answers
// nothing however long it is given.
const nCSIReadyWait = 2 * time.Second

// nCSIQuestionWait is one question's own allowance. It is short, like the system's
// own: a question that took this long to be answered would not have been answered in
// time for the probe either, and the wait would have cost the reader more than it
// bought.
//
// The addresses asked are on this machine's own loopback path into the tunnel (see
// tunfast.ResolverAddrs), where an answer is milliseconds when it comes at all. The
// allowance therefore only has to separate "answered" from "will not answer", and
// the second is exactly the case the wake must not spend two seconds discovering.
const nCSIQuestionWait = 800 * time.Millisecond

// nCSIWarmWait is how long the cache warming may take. Warming is a courtesy too: a
// name the machine already knows costs nothing, and a name it does not is warmed only
// if the resolver answers in time.
//
// Both names are warmed at once, so this is the whole of the warming — not a budget
// per name — and the first of them is the only one the probe's first step needs.
const nCSIWarmWait = 500 * time.Millisecond

// nCSIRewakeDelay is when the second wake goes out.
//
// The first probe can still lose even over a warm cache: the stack may have begun it
// on the old picture, and one that loses is not asked again for minutes. A second
// InternetSetOption a moment later costs two wininet calls and gives the probe a
// fresh start against a machine that has fully settled — the adapter routed, its
// resolver written, the cache filled.
const nCSIRewakeDelay = 1500 * time.Millisecond

// wakeNCSI tells the system to look at its connectivity picture again, once the
// takeover is able to answer the question its own probe is going to ask.
//
// The order is the whole point: the question first, the cache, and only then the
// wake. A wake that arrives while the takeover is still answering nothing is a probe
// the system loses, and the backoff that follows is minutes long.
func (a *App) wakeNCSI() {
	// One budget for both waits: the answer, and the takeover behind it. Spending
	// the first wait's leftovers on the second is what keeps the whole wake inside
	// two seconds on a machine where everything is already in place.
	ctx, cancel := context.WithTimeout(context.Background(), nCSIReadyWait)
	ready := waitNCSIAnswer(ctx)
	canWarm := false
	if ready {
		canWarm = waitSystemTakeover(ctx)
	}
	cancel()
	switch {
	case ready && canWarm:
		// The cache is warm before the system asks: the probe's first step is a
		// resolution, and one that is already answered costs the probe nothing.
		warmNCSIProbe()
	case ready:
		// The takeover has not reached every interface yet. Warming now would put
		// whatever a physical resolver answers — possibly a forgery — into the very
		// cache the probe is about to hit, which is how its first step loses before
		// the probe began. Skipped deliberately; the second wake warms it instead.
		a.logf("[vpn] 系统 DNS 接管尚未生效，跳过缓存预热")
	default:
		a.logf("[vpn] 接管尚未答出探测域名，仍按旧例唤醒系统的连通性探测")
	}
	refreshSystemNetworkState()
	// And once more behind it: see nCSIRewakeDelay. By then the takeover has either
	// landed or will not, so the second wake warms the cache first — a good answer
	// in the cache, overwriting whatever the first probe's own resolution left
	// there, is what lets the probe's second start win.
	go crashlog.Guard("rewakeNCSI", func() {
		time.Sleep(nCSIRewakeDelay)
		if systemTakeoverSettled() {
			warmNCSIProbe()
		}
		refreshSystemNetworkState()
	})
}

// waitSystemTakeover answers whether the takeover has reached every interface that
// resolves — the resident server applies it on its own one-second tick, so a state
// file written moments ago does not yet describe the machine — waiting briefly, on
// the budget the caller hands over.
func waitSystemTakeover(ctx context.Context) bool {
	for {
		if systemTakeoverSettled() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// systemTakeoverSettled reports whether every interface the machine resolves through
// now leads with the takeover's loopback.
//
// The list asked is the same one the server took over (see tunfast.SystemResolvers:
// interfaces with a default route, the tunnel itself skipped), so "all of them carry
// the loopback" is not a guess about which interfaces matter — it is the exact
// condition under which a query through the system resolver can only be answered by
// the forwarder.
func systemTakeoverSettled() bool {
	list, err := tunfast.SystemResolvers(configbuilder.TunAdapterName())
	if err != nil || len(list) == 0 {
		return false
	}
	for _, e := range list {
		if !carriesTakeover(e) {
			return false
		}
	}
	return true
}

// carriesTakeover answers whether one interface's resolver list now starts with the
// takeover. Per family, not per interface: an interface carrying only IPv4 resolvers
// is taken over on that family alone (see PrependResolvers), and its queries are
// answered by the forwarder all the same.
func carriesTakeover(e tunfast.AdapterResolvers) bool {
	for _, v := range e.V4 {
		if v == "127.0.0.1" {
			return true
		}
	}
	for _, v := range e.V6 {
		if v == "::1" {
			return true
		}
	}
	return false
}

// waitNCSIAnswer asks the takeover's own resolver the question the system's probe is
// going to ask, and waits for the answer.
//
// The resolver asked is the adapter's own — the peer address its names are answered
// at (see tunfast.ResolverAddrs) — because that is the one the system's probe will
// use, and no other path proves that one works: the machine's own resolvers may
// answer while the adapter's still cannot.
//
// The addresses are asked at once. They are one per family, and the probe will use
// whichever its own socket picks; asking them in turn made the family that cannot
// answer a full question's wait in front of the wake, which is the wait this function
// exists to keep short.
func waitNCSIAnswer(ctx context.Context) bool {
	addrs, err := tunfast.ResolverAddrs(configbuilder.TunAdapterName())
	if err != nil || len(addrs) == 0 {
		return false
	}
	answers := make(chan bool, len(addrs))
	for _, addr := range addrs {
		go func(addr netip.Addr) {
			answers <- askNCIName(ctx, addr, nCSIProbeHost)
		}(addr)
	}
	for range addrs {
		if <-answers {
			return true
		}
	}
	return false
}

// askNCIName puts one question to one resolver and says whether the answer is one
// the system's probe would accept: no error code, and a real answer behind it.
func askNCIName(ctx context.Context, addr netip.Addr, name string) bool {
	target, err := dnsx.Parse(net.JoinHostPort(addr.String(), "53"))
	if err != nil {
		return false
	}
	qctx, cancel := context.WithTimeout(ctx, nCSIQuestionWait)
	defer cancel()
	reply, err := dnsx.Exchange(qctx, target, dnstest.QueryFor(name, nCSIQueryID()), dnsx.Options{
		Timeout: nCSIQuestionWait,
	})
	if err != nil {
		return false
	}
	return nCSIAnswered(reply)
}

// nCSIAnswered reads a DNS answer and says whether the system's probe would take it.
//
// A SERVFAIL is what a takeover whose core is not up gives, and it is not an answer at
// all as far as the probe is concerned: its allowance is spent on the exchange, and
// the name is still unresolved when the allowance runs out.
func nCSIAnswered(reply []byte) bool {
	if len(reply) < 12 {
		return false
	}
	if reply[3]&0x0F != 0 { // RCODE
		return false
	}
	return binary.BigEndian.Uint16(reply[6:8]) > 0 // ANCOUNT
}

// warmNCSIProbe puts the names the system's probe uses into the machine's DNS cache,
// so that the probe's first step is a cache hit rather than a round trip through the
// tunnel.
//
// It goes through the machine's own resolver on purpose: a question put straight to a
// resolver would be answered all the same, but it would populate no cache — and the
// cache is the thing being warmed here.
//
// Both names go out together: they are two questions to the same resolver, and doing
// them in turn put the second one's wait in the cache's own path for no reason.
func warmNCSIProbe() {
	ctx, cancel := context.WithTimeout(context.Background(), nCSIWarmWait)
	defer cancel()
	var wg sync.WaitGroup
	for _, name := range []string{nCSIProbeHost, nCSIDNSHost} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			// A name that does not resolve is not a failure here: the cache
			// remembers that too, and the probe asks for what it asks for.
			_, _ = net.DefaultResolver.LookupHost(ctx, name)
		}(name)
	}
	wg.Wait()
}

// nCSIQueryID is the id of one question. It only has to differ from the questions
// that are in flight; the clock is the cheapest thing that does.
func nCSIQueryID() uint16 {
	return uint16(time.Now().UnixNano())
}
