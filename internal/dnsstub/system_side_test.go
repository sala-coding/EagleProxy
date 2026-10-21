package dnsstub

import (
	"net/url"
	"testing"

	"eagle/internal/dnsx"
)

// The system side asks through the core's mixed inbound; it has no path of its own. When
// Configure left that proxy unset, clientFor answered nil for it and every name the
// machine asked was a SERVFAIL — quietly, because that failure path does not log. This
// pins the one line that makes the takeover real.
func TestSystemSideGetsTheCoreProxy(t *testing.T) {
	s := startFor(t)
	defer Stop()

	u := &url.URL{Scheme: "http", Host: "127.0.0.1:2080"}
	s.Configure(Wires{Proxy: func() *url.URL { return u }})

	s.mu.Lock()
	got := s.proxies[System]
	s.mu.Unlock()
	if got == nil {
		t.Fatal("the system side has no proxy: its queries can only be answered by SERVFAIL")
	}
	if got() != u {
		t.Errorf("system proxy = %v, want the mixed inbound", got())
	}
}

// The app reconciles this every couple of seconds. A side whose resolver and egress have
// not moved must keep its paths — and with them the connections, the warm-up and the
// error throttle — or every tick looks like a new resolver to a side that has not
// changed. The side holds a pool now (see side.clients), so "kept" is the pool being
// the same pool rather than clientFor answering one particular pointer.
func TestConfigureKeepsTheClientOfAnUnchangedSide(t *testing.T) {
	s := startFor(t)
	defer Stop()

	u := &url.URL{Scheme: "http", Host: "127.0.0.1:2080"}
	proxy := func() *url.URL { return u }
	s.Configure(Wires{Remote: "tls://1.1.1.1", Direct: "https://223.5.5.5/dns-query", Proxy: proxy})

	if first := s.clientFor(Remote); first == nil {
		t.Fatal("no client for a configured remote side")
	}
	pool := poolState(s, Remote)
	if pool.n == 0 {
		t.Fatal("the side built no paths")
	}
	s.Configure(Wires{Remote: "tls://1.1.1.1", Direct: "https://223.5.5.5/dns-query", Proxy: proxy})
	if again := poolState(s, Remote); again != pool {
		t.Error("an unchanged reconcile replaced the pool; the connections behind it go too")
	}

	// A resolver that actually moved is a different path, and gets a different pool.
	s.Configure(Wires{Remote: "tls://8.8.8.8", Direct: "https://223.5.5.5/dns-query", Proxy: proxy})
	if moved := poolState(s, Remote); moved == pool {
		t.Error("a new resolver kept the old paths")
	}
}

// poolState is one comparison-sized look at a side's pool: how many paths it
// has, and which client sits at the head. Rebuilding a pool never reuses a
// client pointer, so the pair identifying it is enough to tell kept from
// replaced without pinning every element.
type poolStateResult struct {
	n     int
	first *dnsx.Client
}

func poolState(s *Server, which Side) poolStateResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sides[which]
	if sl == nil {
		return poolStateResult{}
	}
	out := poolStateResult{n: len(sl.clients)}
	if out.n > 0 {
		out.first = sl.clients[0]
	}
	return out
}

// The system side is not served until its port is: SystemReady is the gate the app uses
// before telling the elevated side to point the interfaces here, and a machine pointed at
// a port nothing answers is every name waiting out a timeout.
func TestSystemReadyFollowsTheListenerAndTheSide(t *testing.T) {
	s := startFor(t)
	defer Stop()
	if s.SystemReady() {
		t.Fatal("system side answered ready before anything configured it")
	}
	empty := &Server{}
	if empty.SystemReady() {
		t.Error("an empty server answered ready")
	}
}
