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

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"time"

	"eagle/internal/configbuilder"
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
const nCSIReadyWait = 3 * time.Second

// nCSIQuestionWait is one question's own allowance. It is short, like the system's
// own: a question that took this long to be answered would not have been answered in
// time for the probe either, and the wait would have cost the reader more than it
// bought.
const nCSIQuestionWait = 1200 * time.Millisecond

// nCSIWarmWait is how long the cache warming may take. Warming is a courtesy too: a
// name the machine already knows costs nothing, and a name it does not is warmed only
// if the resolver answers in time.
const nCSIWarmWait = 1500 * time.Millisecond

// wakeNCSI tells the system to look at its connectivity picture again, once the
// takeover is able to answer the question its own probe is going to ask.
//
// The order is the whole point: the question first, the cache, and only then the
// wake. A wake that arrives while the takeover is still answering nothing is a probe
// the system loses, and the backoff that follows is minutes long.
func (a *App) wakeNCSI() {
	ctx, cancel := context.WithTimeout(context.Background(), nCSIReadyWait)
	ready := waitNCSIAnswer(ctx)
	cancel()
	if ready {
		// The cache is warm before the system asks: the probe's first step is a
		// resolution, and one that is already answered costs the probe nothing.
		warmNCSIProbe()
	} else {
		a.logf("[vpn] 接管尚未答出探测域名，仍按旧例唤醒系统的连通性探测")
	}
	refreshSystemNetworkState()
}

// waitNCSIAnswer asks the takeover's own resolver the question the system's probe is
// going to ask, and waits for the answer.
//
// The resolver asked is the adapter's own — the peer address its names are answered
// at (see tunfast.ResolverAddrs) — because that is the one the system's probe will
// use, and no other path proves that one works: the machine's own resolvers may
// answer while the adapter's still cannot.
func waitNCSIAnswer(ctx context.Context) bool {
	addrs, err := tunfast.ResolverAddrs(configbuilder.TunAdapterName())
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		if askNCIName(ctx, addr, nCSIProbeHost) {
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
func warmNCSIProbe() {
	ctx, cancel := context.WithTimeout(context.Background(), nCSIWarmWait)
	defer cancel()
	for _, name := range []string{nCSIProbeHost, nCSIDNSHost} {
		// A name that does not resolve is not a failure here: the cache remembers
		// that too, and the probe asks for what it asks for.
		_, _ = net.DefaultResolver.LookupHost(ctx, name)
	}
}

// nCSIQueryID is the id of one question. It only has to differ from the questions
// that are in flight; the clock is the cheapest thing that does.
func nCSIQueryID() uint16 {
	return uint16(time.Now().UnixNano())
}
