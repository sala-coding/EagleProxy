// Package dnsstub is the app's own DNS forwarder: two listeners, one per side of a routing
// profile, each pointed at a resolver that can be changed while the core runs.
//
// Why it exists. The resolvers a profile names are written into the core's config, and
// sing-box has no way to change them while it runs — so the DNS card's smart pick could
// choose a faster resolver, write it to the file, and change nothing about the resolution
// happening at that moment. What the profile can name instead is a port on this machine,
// and which resolver stands behind that port is then this package's state rather than the
// core's configuration: switching it costs one assignment (see Configure) and the core
// never learns that anything moved. That is the whole point — 智能优选 and 智能切换 take
// effect without restarting the core, on both sides of the split.
//
// Two listeners, because the profile has two resolvers and the core chooses between them
// with its rules: a proxied name is resolved through the foreign resolver, a direct one
// through the domestic one (see configbuilder.buildDNS). This package deliberately does not
// make that choice — it would have to evaluate the geosite rule sets to make the same one —
// so each side has its own port and the core stays the thing that decides.
//
// The two sides reach their resolvers differently, and that difference is the whole of the
// care in this file:
//
//   - the foreign side's queries are handed to the running core's mixed inbound (the only
//     path they may take — a foreign resolver asked unproxied is the leak and the poisoned
//     answer this side exists to avoid), and an upstream that cannot ride that path is
//     refused rather than silently asked somewhere else;
//   - the domestic side dials straight out, pinned to the machine's real interface (see
//     Wires.Egress) so that the query does not fall into the tunnel it is serving — where
//     the core's own hijack would answer it and ask this package again.
//
// The core is told to ask over TCP (see configbuilder.stubServer): an answer that does not
// fit in a datagram would otherwise arrive truncated, and a forwarder that hands its client
// half an answer is worse than one that takes a stream handshake it does not need.
package dnsstub

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"eagle/internal/dnstest"
	"eagle/internal/dnsx"
)

// Side is which of the profile's two resolvers a listener stands for.
type Side string

const (
	// Remote is the foreign resolver: its queries travel through the running node.
	Remote Side = "remote"
	// Direct is the domestic resolver: asked straight out, past the tunnel.
	Direct Side = "direct"
	// System is the machine's own resolver — the address an interface is pointed at while a
	// node is running, so that the names Windows resolves stop being answered by whichever
	// resolver replies first (see dnsstub's package comment and tunfast.PrependResolvers).
	//
	// It has no upstream of its own: its queries are handed to the main core exactly as the
	// TUN front-end's are, addressed to port 53, and the core's hijack-dns answers them from
	// the profile — which is what puts the split, the smart pick and this package's other
	// two sides in the path of the machine's ordinary resolutions.
	System Side = "system"
)

// The loopback ports the core is told to ask, one per side.
//
// Fixed rather than chosen at bind time: the core's config is built from them, and a port
// that moved between two builds would be a core asking a socket nobody is listening on.
// They sit outside the ranges the app already uses (the mixed inbound, the Clash API).
const (
	RemotePort = 5390
	DirectPort = 5391
)

// SystemPort is where the machine's own resolver is answered, and it is 53 because that is
// the only port the Windows DNS client asks. Bound only while the takeover is on: the port
// is one something else may legitimately own, and a forwarder that took it for itself on
// startup would be a forwarder that broke a machine running its own resolver.
const SystemPort = 53

// systemHopUpstream is the address the system side's queries are addressed to. It is never
// dialled: the hop is the core's mixed inbound and the destination is a port-53 address, so
// the core's own hijack-dns answers the message before any resolver sees it (the same
// arrangement the TUN front-end uses, see configbuilder.BuildTunFront). A literal rather
// than the reader's resolver so that a core which is down cannot turn this into a query sent
// somewhere real — with no core there is no hop, and the query fails, which leaves the
// machine's own resolvers standing behind this one to answer it (see tunfast's
// PrependResolvers).
const systemHopUpstream = "tcp://8.8.8.8:53"

// queryTimeout is one query's allowance, end to end. A resolver that has not answered in
// this long is not going to, and the client is waiting.
const queryTimeout = 5 * time.Second

// warmTimeout is one warm-up query's allowance — shorter than a real query's, because
// a path that cannot be opened in three seconds is not a path worth having warm.
const warmTimeout = 3 * time.Second

// serveTimeout is how long one question on a forwarded connection may take, on each
// side of it. It is a per-question allowance, refreshed before every read and every
// write: a deadline set once is absolute, and it cut the connection after two minutes
// however busy it was (see serve). A variable so a test can shorten it.
var serveTimeout = 2 * time.Minute

// maxInFlight bounds the queries being forwarded at once, per side. Each one holds a
// goroutine and a slot on that side's connection; a core resolving a burst of names queues
// here rather than opening a thread per name.
const maxInFlight = 64

// Wires is what the app tells the forwarder about the world it is forwarding into. It
// arrives whole rather than field by field, because the pieces decide each other: whether a
// domestic upstream is usable depends on whether there is a tunnel to step over, and
// whether the foreign one is usable depends on whether the core's inbound is there to hand
// it to.
type Wires struct {
	// Remote is the foreign resolver's address, in the settings' own syntax.
	Remote string
	// Direct is the domestic resolver's address, same syntax.
	Direct string
	// Proxy is the running core's mixed inbound, read at dial time so a port or a
	// credential that changed under a running forwarder is picked up by the next query.
	// Nil means no path for the foreign side, which refuses its queries rather than
	// asking a foreign resolver unproxied.
	Proxy func() *url.URL
	// Egress is the physical interface's index (see tunfast.EgressIndex), used to pin the
	// domestic side's dials so they do not go through the tunnel. Zero when it could not
	// be determined, which takes the plain-resolver domestic side out of service rather
	// than letting it loop.
	Egress uint32
	// Tunnel says whether TUN mode is the mode in force. With no tunnel up there is
	// nothing for a query to fall into, and a plain domestic resolver is reached the
	// ordinary way.
	Tunnel bool
	// Bootstrap is the machine's own resolvers from before the takeover, used only to
	// resolve an upstream written as a host name. Without it, a DoH resolver named by
	// host name cannot be dialled while the system resolver points here: its name comes
	// back through this forwarder, whose direct side is exactly the path waiting for that
	// lookup (see app_dns_takeover.go and dnsx.Options.Bootstrap).
	Bootstrap []string
	// System asks for the machine's own resolver to be answered here, on port 53. The app
	// asks for it exactly while a node is running and the machine's interfaces have been
	// pointed here (see app_dns_takeover.go): the two are one decision, and a listener
	// without the interfaces pointed at it would be a port taken for nothing.
	System bool
}

// side is one listener's state.
type side struct {
	upstream string
	// bootstrap is what the client resolves its own upstream name with; empty means the
	// system resolver, which is right whenever nothing has taken it over.
	bootstrap []string
	// proxyKey is the proxy the current client was built with, as a string. The mixed
	// inbound's port and credential can change under a running forwarder, and the client is
	// rebuilt when they do — at the query that follows, rather than at the setting that
	// changed, because a rebuild that has to be remembered is a rebuild that gets forgotten.
	proxyKey string
	egress   uint32
	// clients is the side's pool, not one path: a dnsx.Client answers one
	// exchange at a time (a stream cannot carry two questions without ids to
	// tell them apart), and with a single client per side that "one at a time"
	// became the whole side's concurrency. A page load asks dozens of names at
	// once, each through this pool in turn; at one path each they queued on it
	// in arrival order and the tail of every burst met its own five-second
	// deadline — the SERVFAILs that read as "DNS works sometimes". The pool is
	// built and thrown away as a unit (see clientFor); poolNext is the
	// round-robin cursor over it.
	clients  []*dnsx.Client
	poolNext int
	// lastSpoke throttles the error log: a resolver that is down would otherwise write a
	// line per query, and a DNS log nobody can read is a log nobody reads.
	lastSpoke time.Time
	// fails counts the questions this side's upstream has not answered, one after
	// another, and lastTry is when the last one was asked.
	//
	// They are what lets a side that is down stop costing five seconds per name
	// (see answer). A question asked of an upstream that is not answering gets the
	// same answer as the one before it — a SERVFAIL, five seconds later — and a
	// machine asking a dozen names at once pays that twelve times.
	fails   int
	lastTry time.Time
	// failing says whether the side has been told that it is failing, so that the
	// reader hears about it once and not once per question.
	failing bool
}

// Server is the running forwarder.
type Server struct {
	host string
	logf func(string)

	mu      sync.Mutex
	sides   map[Side]*side
	proxies map[Side]func() *url.URL
	listens map[Side]net.Listener
	// sysV6 is the second listener the system side needs: it answers on both loopbacks, and
	// listens is keyed by side rather than by address (see bindSystem).
	sysV6 net.Listener
	// sysU4 and sysU6 are the datagram halves of the same takeover. The Windows resolver
	// asks over UDP first and only falls back to TCP after its own timeout, so a takeover
	// that answered on TCP alone made every cold name wait out that timeout — and a client
	// that does not fall back at all resolved nothing.
	sysU4, sysU6 net.PacketConn
	// sysLastBind is when bindSystem last tried to take port 53; while that
	// attempt fails (the port held by another resolver), reconciles arrive
	// every two seconds and one attempt per thirty is all the retry needs.
	sysLastBind time.Time
	tunnel      bool
	closed      bool
	sem         map[Side]chan struct{}
}

var (
	regMu   sync.RWMutex
	current *Server
)

// Endpoints reports the loopback addresses the core should ask, per side.
//
// An empty string means "this side cannot be served here": the forwarder is not running,
// that listener did not bind, or no usable upstream has been set for it. The config builder
// writes the reader's own resolver address in that case — the forwarder is an improvement,
// never a prerequisite.
func Endpoints() (remote, direct string) {
	regMu.RLock()
	s := current
	regMu.RUnlock()
	if s == nil {
		return "", ""
	}
	return s.Endpoints()
}

// Endpoints is the same answer for one server.
//
// The address is the listener's own, not the constant it was asked for: in production the
// two are the same thing, and a test that binds whatever port is free gets the right answer
// either way (see startOn).
func (s *Server) Endpoints() (remote, direct string) {
	if s == nil {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", ""
	}
	if s.sides[Remote].upstream != "" {
		remote = s.addrLocked(Remote)
	}
	if s.sides[Direct].upstream != "" {
		direct = s.addrLocked(Direct)
	}
	return remote, direct
}

// addr is the listener's own address for one side, empty when that side has none. It does
// not care whether an upstream is set: the tests need to reach a side that has none (to see
// it answer SERVFAIL), and production never asks for an address it is not told to use.
func (s *Server) addr(which Side) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addrLocked(which)
}

func (s *Server) addrLocked(which Side) string {
	if l := s.listens[which]; l != nil {
		return l.Addr().String()
	}
	return ""
}

// Start brings the forwarder up on the loopback and registers it as the one the config
// builder should name. Calling it twice is a no-op on the second call.
//
// A listener that cannot bind is not an error the caller has to handle: that side is left
// out of Endpoints and the core goes on asking the reader's resolver directly.
func Start(logf func(string)) *Server {
	regMu.Lock()
	defer regMu.Unlock()
	if current != nil {
		if logf != nil {
			current.setLogger(logf)
		}
		return current
	}
	current = startOn(RemotePort, DirectPort, logf)
	return current
}

// startOn is Start with the two ports handed in, for a caller that is not the app: the
// tests bind whatever the system gives them (0), because the app itself may well be running
// and holding the production ports — which is a fact about the machine, not a fault to
// report.
func startOn(remotePort, directPort int, logf func(string)) *Server {
	s := &Server{
		host:    "127.0.0.1",
		logf:    logf,
		sides:   map[Side]*side{Remote: {}, Direct: {}, System: {}},
		proxies: map[Side]func() *url.URL{},
		listens: map[Side]net.Listener{},
		sem: map[Side]chan struct{}{
			Remote: make(chan struct{}, maxInFlight),
			Direct: make(chan struct{}, maxInFlight),
			System: make(chan struct{}, maxInFlight),
		},
	}
	for _, listen := range []struct {
		which Side
		port  int
	}{{Remote, remotePort}, {Direct, directPort}} {
		addr := net.JoinHostPort(s.host, fmt.Sprint(listen.port))
		lc := net.ListenConfig{Control: reuseAddr}
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			// Not fatal, and deliberately loud: from here on the core will be told to ask
			// the reader's own resolver for this side, which works but does not follow the
			// smart pick.
			s.log(fmt.Sprintf("[dns] %s 侧监听 %s 失败（%v），该侧回退为按路由档直问", listen.which, addr, err))
			continue
		}
		s.listens[listen.which] = l
		go s.accept(l, listen.which)
	}
	s.log(fmt.Sprintf("[dns] 内置解析器已就绪：remote %s / direct %s（换解析器无需重启核心）",
		s.addrLocked(Remote), s.addrLocked(Direct)))
	return s
}

// (startOn's callers pass 0 for "any port the system will give", which is what a test wants:
// the app itself may be running and holding the production ones, and that is a fact about
// the machine rather than a fault to report. Endpoints reports the listener's own address,
// so nothing else has to know which port was chosen.)

// Stop takes the forwarder down and unregisters it.
func Stop() {
	regMu.Lock()
	s := current
	current = nil
	regMu.Unlock()
	if s != nil {
		s.Close()
	}
}

// Current is the running forwarder, or nil.
func Current() *Server {
	regMu.RLock()
	defer regMu.RUnlock()
	return current
}

// Configure points the two sides at the resolvers the active routing profile names, and
// tells the forwarder how each one has to be reached.
//
// An empty, unreadable or unusable upstream takes that side down (its queries are answered
// SERVFAIL) and takes it out of Endpoints, so the next config built for the core names the
// reader's own address instead. That is the shape of the whole fallback: nothing here may
// be the difference between the machine resolving names and not.
func (s *Server) Configure(w Wires) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tunnel = w.Tunnel
	// The system side asks through the same door as the foreign one: its queries are
	// addressed to the core's hijack (see systemHopUpstream) and are only answerable over
	// the mixed inbound. Without this line the side had no path at all — clientFor answers
	// nil for a system side with no proxy — and every name the machine asked was a
	// SERVFAIL that fell through to the resolvers kept behind ours. The takeover logged
	// itself as working the whole time (see setSystem), which is what made it silent.
	s.proxies[Remote] = w.Proxy
	s.proxies[System] = w.Proxy
	s.mu.Unlock()
	s.setSide(Remote, w.Remote, w)
	s.setSide(Direct, w.Direct, w)
	s.setSystem(w)
}

// setSystem takes the machine's own resolver in or out of service, and the port the OS asks
// comes and goes with it.
//
// Only while asked for, and that is not tidiness. Port 53 is a port something else may
// legitimately own — a resolver the reader runs, Windows' own DNS service in some
// configurations — and a forwarder that claimed it on startup would be a forwarder that
// broke them for as long as it lived. It is also only useful while a core is there to answer
// through: the queries go to the core, and with no core the machine would be waiting for
// this socket instead of the resolver it had (the interfaces keep their own resolvers behind
// ours, see tunfast.PrependResolvers, but a timeout per name is still a bad trade for
// nothing).
func (s *Server) setSystem(w Wires) {
	if !w.System {
		s.mu.Lock()
		l4, l6 := s.listens[System], s.sysV6
		u4, u6 := s.sysU4, s.sysU6
		s.listens[System], s.sysV6 = nil, nil
		s.sysU4, s.sysU6 = nil, nil
		// The throttle paced the attempt, not the listener: keeping it past a deliberate
		// stop meant a node restarted inside thirty seconds left the interfaces pointed at
		// a port nothing was listening on (see bindSystem).
		s.sysLastBind = time.Time{}
		old := s.sides[System]
		s.sides[System] = &side{}
		s.mu.Unlock()
		if l4 != nil {
			_ = l4.Close()
		}
		if l6 != nil {
			_ = l6.Close()
		}
		if u4 != nil {
			_ = u4.Close()
		}
		if u6 != nil {
			_ = u6.Close()
		}
		closeClients(old.clients)
		return
	}
	if w.Proxy == nil {
		// No mixed inbound means no path to the core, which is the whole of what this side
		// does: without it there is nothing to point the machine at.
		return
	}
	s.mu.Lock()
	bound := s.systemBoundLocked()
	s.mu.Unlock()
	if !bound {
		// Not every socket is up. The question used to be "is anything
		// serving?", and a half-taken port answered that — so the missing
		// halves stayed missing for the life of the process, while the app went
		// on telling the machine to point its interfaces at 127.0.0.1:53 and
		// reporting the takeover as ready. bindSystem fills in each address
		// family that is missing and leaves the ones that are up alone, so
		// asking it again is how the rest gets there.
		s.bindSystem()
	}
	s.mu.Lock()
	ok := s.systemServingLocked()
	old := s.sides[System]
	unchanged := ok && old.upstream == systemHopUpstream
	switch {
	case unchanged:
		// Nothing moved. This runs on the app's reconcile, every couple of seconds, and
		// rebuilding the side each time threw away its client with it: the CONNECT
		// through the core, the keep-alive under it and the error throttle were all
		// reset on a timer, so every cold query paid a handshake and a failing resolver
		// wrote a line per query instead of one per half-minute.
	case ok:
		s.sides[System] = &side{upstream: systemHopUpstream}
	default:
		// The port was taken or lost: nothing to serve until the next attempt.
		s.sides[System] = &side{}
	}
	s.mu.Unlock()
	if ok && !unchanged {
		s.log("[dns] 系统解析器已接管：本机 53 → 核心按路由档回答")
	}
	if !unchanged {
		closeClients(old.clients)
	}
}

// systemServingLocked reports whether anything is answering on port 53. Either family and
// either transport counts: Windows asks over UDP first and only falls back to TCP after its
// own timeout, so a TCP-only answer would turn every cold name into that wait. Lock-held.
func (s *Server) systemServingLocked() bool {
	return s.listens[System] != nil || s.sysV6 != nil || s.sysU4 != nil || s.sysU6 != nil
}

// systemBoundLocked reports whether every socket the takeover needs is up: both loopbacks,
// over both UDP and TCP. A port half taken is not a takeover — the interface pointed at
// it waits out a timeout on the half that answers, which is the whole reason the v6
// sockets are taken as well. Lock-held.
func (s *Server) systemBoundLocked() bool {
	return s.listens[System] != nil && s.sysV6 != nil && s.sysU4 != nil && s.sysU6 != nil
}

// SystemReady reports whether the machine's resolver port is actually being answered right
// now. The app asks before it tells the elevated side to point the interfaces here: a state
// file asking for a takeover while nothing listens is a machine whose every name waits out a
// timeout (see app_dns_takeover.go).
func (s *Server) SystemReady() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sides[System]
	return !s.closed && sl != nil && sl.upstream != "" && s.systemServingLocked()
}

// bindSystem takes port 53 on both loopbacks and starts answering on it.
//
// Both, because an interface pointed at ::1 for its IPv6 names would otherwise wait out a
// timeout on every one of them before falling back — the leak's own path is the IPv6 one
// (that is where the router's forged answers arrived from on this machine), so leaving v6
// unanswered would leave half the fix undone.
func (s *Server) bindSystem() {
	s.mu.Lock()
	// Port 53 is not always ours to take (another local resolver may hold it); failing
	// that every two seconds would only spam the log. One attempt per thirty seconds is
	// all the retry this needs — and the throttle guards the ATTEMPT, not the sockets.
	// Closing what worked and then finding the retry throttled left port 53 answering for
	// about one second in thirty whenever only half the bindings were up, which the reader
	// met as names that were slow and sometimes wrong. Nothing is closed here any more:
	// each address family is filled in only when it is missing, so a retry cannot take
	// away what the previous one installed.
	if !s.sysLastBind.IsZero() && time.Since(s.sysLastBind) < 30*time.Second {
		s.mu.Unlock()
		return
	}
	s.sysLastBind = time.Now()
	s.mu.Unlock()
	lc := net.ListenConfig{Control: reuseAddr}
	for _, target := range []struct {
		addr string
		v6   bool
	}{
		{net.JoinHostPort("127.0.0.1", fmt.Sprint(SystemPort)), false},
		{net.JoinHostPort("::1", fmt.Sprint(SystemPort)), true},
	} {
		s.mu.Lock()
		haveUDP := (target.v6 && s.sysU6 != nil) || (!target.v6 && s.sysU4 != nil)
		haveTCP := (target.v6 && s.sysV6 != nil) || (!target.v6 && s.listens[System] != nil)
		s.mu.Unlock()
		// Datagram first, and without SO_REUSEADDR: on Windows that flag lets two
		// sockets bind one UDP port, which would read as a takeover while the queries
		// went to whichever socket bound last. UDP is the half the Windows resolver
		// asks, so a failure here is the one that matters most.
		if !haveUDP {
			pc, err := net.ListenPacket("udp", target.addr)
			if err != nil {
				s.log(fmt.Sprintf("[dns] 系统解析器 UDP 监听 %s 失败（%v），该地址只能等客户端超时后走 TCP", target.addr, err))
			} else {
				s.mu.Lock()
				if target.v6 {
					s.sysU6 = pc
				} else {
					s.sysU4 = pc
				}
				s.mu.Unlock()
				go s.acceptUDP(pc, System)
			}
		}
		if haveTCP {
			continue
		}
		l, err := lc.Listen(context.Background(), "tcp", target.addr)
		if err != nil {
			s.log(fmt.Sprintf("[dns] 系统解析器 TCP 监听 %s 失败（%v）", target.addr, err))
			continue
		}
		s.mu.Lock()
		if target.v6 {
			s.sysV6 = l
		} else {
			s.listens[System] = l
		}
		s.mu.Unlock()
		go s.accept(l, System)
	}
}

// Upstreams reports what each side is pointed at, for the log and for the tests.
func (s *Server) Upstreams() (remote, direct string) {
	if s == nil {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sides[Remote].upstream, s.sides[Direct].upstream
}

func (s *Server) setSide(which Side, upstream string, w Wires) {
	upstream = strings.TrimSpace(upstream)
	err := upstreamProblem(which, upstream, w)
	if err != nil {
		if upstream != "" {
			s.log(fmt.Sprintf("[dns] %s 侧上游不可用（%s），回退为按路由档直问", which, err))
		}
		s.mu.Lock()
		old := s.sides[which]
		s.sides[which] = &side{}
		s.mu.Unlock()
		closeClients(old.clients)
		return
	}
	// The client is not built here: it is built at the first query, so that the proxy it
	// needs (the mixed inbound, which is only up while a node is) is read then rather than
	// guessed now.
	egress := uint32(0)
	if which == Direct {
		egress = w.Egress
	}
	s.mu.Lock()
	old := s.sides[which]
	was := old.upstream
	// The app reconciles this every couple of seconds. A side whose resolver, egress and
	// bootstrap have not moved keeps its client — and with it the connection the first
	// query paid for, the proxy key the next query would otherwise re-derive, and the
	// error throttle. Rebuilding it here made every tick look like a new resolver.
	if was == upstream && old.egress == egress && slices.Equal(old.bootstrap, w.Bootstrap) {
		s.mu.Unlock()
		return
	}
	s.sides[which] = &side{upstream: upstream, egress: egress, bootstrap: w.Bootstrap}
	s.mu.Unlock()
	if was != upstream && was != "" {
		s.log(fmt.Sprintf("[dns] %s 侧解析器切换为 %s（无需重启核心）", which, upstream))
	} else if was == "" {
		s.log(fmt.Sprintf("[dns] %s 侧解析器为 %s", which, upstream))
	}
	closeClients(old.clients)
}

// upstreamProblem is the whole of "may this side be pointed at this address", and it is
// per side because the two have different ways out.
func upstreamProblem(which Side, upstream string, w Wires) error {
	if upstream == "" {
		return errors.New("未设置上游")
	}
	if strings.EqualFold(upstream, "local") {
		// A forwarder pointed at the system resolver would be asking the machine, which is
		// the answer the whole arrangement is trying to get away from.
		return errors.New("local 是系统解析器，无法作为转发目标")
	}
	if strings.EqualFold(upstream, "fakeip") {
		return errors.New("fakeip 由核心生成，无法作为转发目标")
	}
	t, err := dnsx.Parse(upstream)
	if err != nil {
		return err
	}
	if t.Transport == dnsx.TLS || t.Transport == dnsx.HTTPS {
		return nil
	}
	// Plain DNS, which is the case the two sides answer differently.
	//
	// The foreign side cannot use it at all: its queries are handed to the core's mixed
	// inbound as a stream, and a plain resolver asked over that stream is not the resolver
	// being asked — while a plain resolver asked any other way is a query that left the
	// tunnel.
	if which == Remote {
		return errors.New("国外侧只能用加密解析器（明文无法经核心那一跳，直问则是泄漏）")
	}
	// The domestic side may use it, but only when the query can be kept out of the tunnel:
	// pinned to the machine's real interface (see Wires.Egress), or with no tunnel up at
	// all. Without that, the query falls into the tunnel, the core's own hijack answers it,
	// and the answer is asked of this forwarder again — a circle, and one that answers
	// nothing while it runs.
	if w.Tunnel && w.Egress == 0 {
		return errors.New("无法确定隧道外的出口网卡，明文解析器会与核心劫持成环")
	}
	return nil
}

// clientFor is the side's client as of this query: built when there is not one, and built
// again when the proxy under it has moved.
func (s *Server) clientFor(which Side) *dnsx.Client {
	s.mu.Lock()
	sl := s.sides[which]
	if s.closed || sl == nil || sl.upstream == "" {
		s.mu.Unlock()
		return nil
	}
	upstream, egress := sl.upstream, sl.egress
	fn := s.proxies[which]
	s.mu.Unlock()

	proxy := (*url.URL)(nil)
	if fn != nil {
		proxy = fn()
	}
	if proxy == nil && (which == Remote || which == System) {
		// Neither of these two has a path of its own: their queries go through the running
		// core — the foreign side to reach the node, the system side so that the core's own
		// rules answer them — and with no mixed inbound to hand them to there is no path.
		// The foreign side asked straight out would be the leak and the forged answer this
		// exists to prevent; the system side asked straight out would be a second resolver
		// answering, which is the whole thing being replaced.
		return nil
	}
	key := ""
	if proxy != nil {
		key = proxy.String()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sl = s.sides[which]
	if s.closed || sl == nil || sl.upstream != upstream {
		// Replaced while this query was being set up; the newer one owns the side now.
		return nil
	}
	if len(sl.clients) > 0 && sl.proxyKey == key {
		// Round-robin over the pool: consecutive questions land on different
		// paths, which is the whole of the concurrency this side has (see the
		// note on side.clients).
		c := sl.clients[sl.poolNext%len(sl.clients)]
		sl.poolNext++
		return c
	}
	target, err := dnsx.Parse(upstream)
	if err != nil {
		return nil
	}
	// The count under the old paths was theirs: failures over the proxy or
	// resolver that has just gone must not open the new paths' account already
	// near the breaker (see noteFailure), and a side that was failing on the
	// old path gets its one clean question on the new one — hence the reset
	// below, inside the same lock that swaps the pool.
	olds := sl.clients
	sl.clients = make([]*dnsx.Client, dnsPoolSize)
	for i := range sl.clients {
		sl.clients[i] = dnsx.NewClient(target, dnsx.Options{
			Proxy: proxy,
			// A relay owes its client a whole answer: a truncated one is re-asked over TCP
			// rather than passed on cut in half.
			RetryTruncated: true,
			Timeout:        queryTimeout,
			InterfaceIndex: egress,
			Bootstrap:      sl.bootstrap,
		})
	}
	sl.proxyKey = key
	sl.poolNext = 1 // index 0 is the one answered below
	if len(olds) > 0 {
		sl.fails = 0
		sl.failing = false
		closeClients(olds)
	}
	return sl.clients[0]
}

// WarmUp opens the side's path ahead of the first real query: one exchange over the
// very client the side's queries use, whose connection — dialled, handshaken, and kept
// open by the client — is then sitting there waiting for the first query that matters.
// What it saves is that first query's setup: a dial and TLS handshake to a domestic
// DoH, or a CONNECT through the node plus the far handshake, several hundred
// milliseconds once per start.
//
// Failure answers an error and nothing else: the path warms itself with the first
// real query exactly as it did before, so nobody owes this a retry.
func (s *Server) WarmUp(which Side) error {
	c := s.clientFor(which)
	if c == nil {
		return errors.New("该侧当前没有可用路径")
	}
	ctx, cancel := context.WithTimeout(context.Background(), warmTimeout)
	defer cancel()
	_, err := c.Exchange(ctx, dnstest.Query(0x5741))
	return err
}

func (s *Server) setLogger(logf func(string)) {
	s.mu.Lock()
	s.logf = logf
	s.mu.Unlock()
}

func (s *Server) log(line string) {
	s.mu.Lock()
	logf := s.logf
	s.mu.Unlock()
	if logf != nil {
		logf(line)
	}
}

func (s *Server) accept(l net.Listener, which Side) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go s.serve(conn, which)
	}
}

// acceptUDP answers datagrams off one socket. There is no framing and no connection to
// keep, so each message is answered on its own goroutine and the reply goes back to the
// sender's address; the buffer is the largest a datagram can be, because a truncated read
// would be a malformed query rather than a short one.
func (s *Server) acceptUDP(pc net.PacketConn, which Side) {
	buf := make([]byte, 65535)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		msg := make([]byte, n)
		copy(msg, buf[:n])
		go func(msg []byte, addr net.Addr) {
			reply := s.answer(which, msg)
			if len(reply) == 0 {
				return
			}
			_, _ = pc.WriteTo(reply, addr)
		}(msg, addr)
	}
}

// serve reads queries off one connection and writes the answers back, in order.
func (s *Server) serve(conn net.Conn, which Side) {
	defer conn.Close()
	for {
		// The deadline is set per question, not once at accept. A deadline set
		// once is absolute: it does not move along with the traffic, so it cut
		// the connection after two minutes however busy it was — and this
		// connection is a long one. The core is told to ask over TCP (see
		// configbuilder.stubServer) and keeps the connection open and reuses
		// it, so the queries on it failed every two minutes for the life of the
		// tunnel, and the answer could not even be sent back: the write deadline
		// had passed as well.
		_ = conn.SetReadDeadline(time.Now().Add(serveTimeout))
		msg, err := readFramed(conn)
		if err != nil {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(serveTimeout))
		if err := writeFramed(conn, s.answer(which, msg)); err != nil {
			return
		}
	}
}

// answer forwards one query and returns the message to write back. Every failure is a
// SERVFAIL rather than a dropped connection: the core is a client, and a client that is
// told "no" moves on, while a silent one waits out its own timeout on every name.
func (s *Server) answer(which Side, msg []byte) []byte {
	if !isQuery(msg) {
		return servfail(msg)
	}
	// A side whose upstream is not answering is not asked again straight away.
	//
	// The answer would be the same SERVFAIL, five seconds later, for every name the
	// machine asks — and the machine asks a lot of names when a page is loading.
	// One attempt every so often is enough to notice a side that has come back (and
	// the app's own smart pick moves the upstream anyway); the rest are told "no" at
	// once, which is the difference between a browser that says it failed and one
	// that sits there.
	if !s.sideUsable(which) {
		return servfail(msg)
	}
	client := s.clientFor(which)
	if client == nil {
		return servfail(msg)
	}
	select {
	case s.sem[which] <- struct{}{}:
		defer func() { <-s.sem[which] }()
	default:
		// Saturated: answered rather than queued behind everything already in flight.
		s.spoke(which, "查询过多，已直接回 SERVFAIL")
		return servfail(msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	reply, err := client.Exchange(ctx, msg)
	if err != nil {
		s.noteFailure(which, client)
		s.spoke(which, err.Error())
		return servfail(msg)
	}
	if len(reply) < 12 || binary.BigEndian.Uint16(reply[:2]) != binary.BigEndian.Uint16(msg[:2]) {
		// An answer to another question is not an answer: the client's id travels through
		// untouched, so a mismatch means something between here and the resolver is
		// confused, and guessing which of the two is right is not this code's business.
		s.noteFailure(which, client)
		s.spoke(which, "上游返回的编号与查询不符")
		return servfail(msg)
	}
	s.noteSuccess(which, client)
	return reply
}

// failThreshold is how many questions in a row the upstream has to leave unanswered
// before this side stops asking it.
//
// One is not enough: a resolver can drop a single question, and a side that gave up
// on the first sign would be a side that answers "no" to a resolver that was having
// a bad moment. Two in a row is a pattern.
const failThreshold = 2

// failRetryAfter is how long a failing side waits before trying its upstream again.
//
// It is the price of the fast answer: the side is not really better off, it is
// better off being honest sooner. A short window means a side that comes back is
// noticed quickly; the attempt itself is what costs, and one attempt in this many
// seconds is one question in that time waiting out the full timeout.
var failRetryAfter = 10 * time.Second

// sideUsable reports whether this side may be asked right now, and books the
// attempt when it may.
//
// A side that is not failing is always asked. A side that is failing is asked once
// per failRetryAfter, and the rest of the questions are answered at once.
func (s *Server) sideUsable(which Side) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sides[which]
	if sl == nil {
		return false
	}
	if sl.fails < failThreshold {
		sl.lastTry = time.Now()
		return true
	}
	if time.Since(sl.lastTry) >= failRetryAfter {
		sl.lastTry = time.Now()
		return true
	}
	return false
}

// noteFailure records that this side's upstream did not answer — but only when
// the client it was asked through is still the side's.
//
// The identity matters because both replacement paths close the old client
// with queries still in flight on it (setSide on a new resolver, clientFor on
// a new proxy), and those exchanges come back as "the path is closed": a local
// teardown, not an upstream that failed. Counted against whatever holds the
// seat now, two such in-flight strays are exactly failThreshold — the breaker
// tripped on a side that had never been asked, its first real question met
// with ten seconds of SERVFAIL over a resolver that was fine (the window this
// bites in — a resolver change or a core restart mid-page-load — is dense with
// queries, so the two strays arrive together).
//
// from is the client the failure came back on; a nil from (no client was ever
// established) counts as before.
func (s *Server) noteFailure(which Side, from *dnsx.Client) {
	s.mu.Lock()
	sl := s.sides[which]
	if sl == nil || !poolHas(sl.clients, from) {
		// The seat moved while this query was in flight (or it went through a
		// pool that has since been replaced): the failure belongs to the path
		// that went away, and the newcomer starts with a clean count.
		s.mu.Unlock()
		return
	}
	sl.fails++
	nowFailing := sl.fails >= failThreshold && !sl.failing
	if nowFailing {
		sl.failing = true
	}
	up := sl.upstream
	s.mu.Unlock()
	if nowFailing {
		// Said once, because the reader is looking at a machine that is suddenly slow
		// and this is the reason: the side's resolver is not answering, and every name
		// the machine asks is being told "no" at once instead of after a wait.
		s.log(fmt.Sprintf("[dns] %s 侧上游无响应（%s），该侧改为直接回 SERVFAIL", which, up))
	}
}

// noteSuccess records that this side's upstream answered, and that the side is
// working again. The same identity rule as noteFailure: an answer arriving on
// a client the side has already replaced clears nothing — the count it would
// have cleared belongs to the seat now, which has its own.
func (s *Server) noteSuccess(which Side, from *dnsx.Client) {
	s.mu.Lock()
	sl := s.sides[which]
	if sl == nil || !poolHas(sl.clients, from) {
		s.mu.Unlock()
		return
	}
	wasFailing := sl.failing
	sl.fails = 0
	sl.failing = false
	s.mu.Unlock()
	if wasFailing {
		s.log(fmt.Sprintf("[dns] %s 侧已恢复响应", which))
	}
}

// spoke logs an error at most once every thirty seconds per side. A resolver that is down
// fails every query, and the log is for the reader rather than for the volumes.
func (s *Server) spoke(which Side, reason string) {
	s.mu.Lock()
	sl := s.sides[which]
	if sl == nil || time.Since(sl.lastSpoke) < 30*time.Second {
		s.mu.Unlock()
		return
	}
	sl.lastSpoke = time.Now()
	up := sl.upstream
	s.mu.Unlock()
	s.log(fmt.Sprintf("[dns] %s 侧解析失败（%s）：%s", which, up, reason))
}

// Close puts the listeners and every side's connection away.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	listens := []net.Listener{s.listens[Remote], s.listens[Direct], s.listens[System], s.sysV6}
	packets := []net.PacketConn{s.sysU4, s.sysU6}
	old := []*side{s.sides[Remote], s.sides[Direct], s.sides[System]}
	s.listens = map[Side]net.Listener{}
	s.sysV6 = nil
	s.sysU4, s.sysU6 = nil, nil
	s.sides = map[Side]*side{Remote: {}, Direct: {}, System: {}}
	s.mu.Unlock()

	for _, l := range listens {
		if l != nil {
			_ = l.Close()
		}
	}
	for _, pc := range packets {
		if pc != nil {
			_ = pc.Close()
		}
	}
	for _, sl := range old {
		closeClients(sl.clients)
	}
}

// closeClient puts a client away without holding the caller up. Close waits for any
// exchange already in flight on that connection, and the caller is a settings save: a save
// must not stand still for five seconds because a resolver is slow to answer.
func closeClient(c *dnsx.Client) {
	if c != nil {
		go func(c *dnsx.Client) { _ = c.Close() }(c)
	}
}

// closeClients puts a whole pool away, one goroutine per path for the reason
// above: Close waits for the exchange in flight on that path, and the caller is
// a settings save that must not.
func closeClients(cs []*dnsx.Client) {
	for _, c := range cs {
		closeClient(c)
	}
}

// dnsPoolSize is how many paths one side keeps open. Four is what the burst
// from a page load needs to stay inside the five-second budget on a tunnel
// that costs ~300ms a question: twenty waiters drain in about 1.5s, where one
// path took six.
const dnsPoolSize = 4

// poolHas reports whether c is one of the side's current paths. A nil c (a
// question that never got a client) counts as the side's, as it always did.
func poolHas(pool []*dnsx.Client, c *dnsx.Client) bool {
	if c == nil {
		return true
	}
	for _, x := range pool {
		if x == c {
			return true
		}
	}
	return false
}

// isQuery reports whether this is a single-question query, which is the only thing worth
// forwarding: a response arriving at a forwarder is a client that has lost its way, and a
// message with no question has nothing to answer.
func isQuery(msg []byte) bool {
	if len(msg) < 12 {
		return false
	}
	if msg[2]&0x80 != 0 { // QR: this is already a response
		return false
	}
	return binary.BigEndian.Uint16(msg[4:6]) == 1
}

// servfail answers a query with SERVFAIL, echoing its id, opcode and question and nothing
// else.
func servfail(query []byte) []byte {
	if len(query) < 12 {
		return []byte{0, 0, 0x81, 0x82, 0, 0, 0, 0, 0, 0, 0, 0}
	}
	out := make([]byte, len(query))
	copy(out, query)
	// QR and the opcode and RD the client asked for; RA set, RCODE 2. Writing them
	// separately is what keeps a query's own flags from being echoed as if they were the
	// answer's.
	out[2] = 0x80 | (query[2] & 0x0F)
	out[3] = 0x80 | 0x02
	binary.BigEndian.PutUint16(out[6:8], 0)  // ANCOUNT
	binary.BigEndian.PutUint16(out[8:10], 0) // NSCOUNT
	binary.BigEndian.PutUint16(out[10:12], 0)
	if end := questionEnd(query); end > 0 && end <= len(out) {
		out = out[:end]
	}
	return out
}

// questionEnd is where the question section ends in a query: after the name's root label
// and the type and class that follow it. Compression pointers cannot appear in a question,
// and one that does is answered by leaving the message whole.
func questionEnd(query []byte) int {
	i := 12
	for i < len(query) {
		l := int(query[i])
		if l == 0 {
			end := i + 1 + 4
			if end <= len(query) {
				return end
			}
			return 0
		}
		if l&0xC0 != 0 {
			return 0
		}
		i += 1 + l
	}
	return 0
}

// readFramed reads one length-prefixed DNS message, which is how a stream carries them.
func readFramed(r io.Reader) ([]byte, error) {
	var length [2]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(length[:]))
	if size < 12 || size > 65535 {
		return nil, errors.New("消息长度异常")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// writeFramed writes one, with its length in front.
func writeFramed(w io.Writer, msg []byte) error {
	framed := make([]byte, 2, 2+len(msg))
	binary.BigEndian.PutUint16(framed, uint16(len(msg)))
	framed = append(framed, msg...)
	_, err := w.Write(framed)
	return err
}
