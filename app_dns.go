package main

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"eagle/internal/configbuilder"
	"eagle/internal/crashlog"
	"eagle/internal/dnsstub"
	"eagle/internal/tunfast"
	"eagle/internal/tunserv"
)

// The app's own DNS forwarder (see internal/dnsstub), wired to the rest of the app here.
//
// What it buys is the one thing a routing profile cannot have: resolvers that can be
// changed while the core is running. The profile names two resolvers, the core writes them
// into its config at start-up, and sing-box has no way to hand it another one afterwards —
// so 智能优选 could pick a faster resolver, write it to the file, and change nothing about
// the resolution happening at that moment. With the profile naming the forwarder's two
// ports instead, the resolver behind each port is this app's state, and switching it is one
// assignment (see syncDNSStub's callers: every door a profile can come in by).

// startDNSStub brings the forwarder up, once.
func (a *App) startDNSStub() {
	dnsstub.Start(a.logf)
	a.syncDNSStub()
	// The domestic side can be reached the moment the app is: one query over the
	// client the side's queries use, and the dial plus handshake the first real
	// lookup would have paid is already paid.
	go crashlog.Guard("dnsWarmDirect", func() { a.warmDNSStub(dnsstub.Direct) })
}

// warmDNSStub opens one side's path ahead of the first query that needs it. Failure
// stays quiet — the side warms itself with the first real query exactly as before,
// and a line about a path nobody was waiting on is noise.
func (a *App) warmDNSStub(side dnsstub.Side) {
	s := dnsstub.Current()
	if s == nil {
		return
	}
	if err := s.WarmUp(side); err != nil {
		return
	}
	a.logf("[dns] " + string(side) + " 侧解析路径已预热")
}

// syncDNSStub points the forwarder at the resolvers the active routing profile names, and
// tells it how each side has to be reached.
//
// Called from every door the profile can come in by — a settings save, a routing save, a
// profile switch, a reload, startup — because the alternative is remembering to call it,
// and a resolver that is only sometimes current is worse than one that never moves.
func (a *App) syncDNSStub() {
	s := dnsstub.Current()
	if s == nil || a.curSetting() == nil {
		return
	}
	r := a.curSetting().Routing()
	if r == nil {
		return
	}
	s.Configure(dnsstub.Wires{
		Remote: r.RemoteDNS,
		Direct: r.DirectDNS,
		Proxy:  a.inboundProxyURL,
		Egress: a.egressIndex(),
		Tunnel: a.curSetting().VPNMode(),
		// Whether the forwarder should HOLD port 53 — not whether it already answers on it:
		// binding is what this very call is about to make possible, and asking the second
		// question here answered no for ever (see systemDNSEnabled). Telling the elevated
		// side to point the interfaces at it is the narrower decision, and that one does
		// wait for the port (see systemDNSWanted).
		System: a.systemDNSEnabled(),
		// What resolves an upstream written as a host name while this process is the
		// machine's resolver (see Wires.Bootstrap). Empty before any takeover, which is
		// when the system resolver is still the right one to ask.
		Bootstrap: a.bootstrapResolvers(),
	})
}

// bootstrapResolvers reads the machine's pre-takeover resolvers out of the elevated side's
// undo record. It is the only copy of "what the interfaces carried before we pointed them
// here" that survives a crash, and it is what a DoH/DoT upstream named by host name needs
// to be dialled at all.
func (a *App) bootstrapResolvers() []string {
	if a == nil || a.dataDir == "" {
		return nil
	}
	return tunserv.OriginalResolvers(a.dataDir)
}

// egressIndex is the interface the machine's own traffic leaves by, with the tunnel passed
// over — what the forwarder's domestic dials are pinned to, so that a query it makes does
// not fall into the tunnel it is serving and come back to it as the core's own question.
//
// Zero when it cannot be told, which takes plain domestic resolvers out of service rather
// than letting them circle (see dnsstub.upstreamProblem). Read fresh on every sync: this is
// a route-table read, and the answer changes with the network — a laptop leaving one
// wireless network for another has a different interface index, and a pin from the last one
// would send the app's queries out of an interface that is no longer the way out.
func (a *App) egressIndex() uint32 {
	idx, err := tunfast.EgressIndex(configbuilder.TunAdapterName())
	if err != nil {
		return 0
	}
	return idx
}

// inboundProxyURL is the running core's mixed inbound: the one path the foreign side's
// queries are handed to, and the reason they carry no leak — a query the node is not
// carrying is a query that side refuses (see dnsstub.clientFor) rather than one asked
// straight out.
func (a *App) inboundProxyURL() *url.URL {
	set := a.curSetting()
	if set == nil {
		return nil
	}
	host := strings.TrimSpace(set.InboundAddress)
	// A wildcard listen address is not a dial target: 0.0.0.0 and :: mean "every interface
	// here", and handing that to net.Dial reaches nothing on some systems and localhost on
	// others.
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := set.InboundSocksPort
	if port <= 0 || port > 65535 {
		return nil
	}
	u := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if set.InboundAuth != nil && set.InboundAuth.NeedAuth() {
		u.User = url.UserPassword(set.InboundAuth.Username, set.InboundAuth.Password)
	}
	return u
}

// stopDNSStub takes the forwarder down.
func (a *App) stopDNSStub() {
	dnsstub.Stop()
}
