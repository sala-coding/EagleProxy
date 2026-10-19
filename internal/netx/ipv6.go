package netx

import (
	"net"
	"strings"
	"sync"
	"time"
)

// HasIPv6 reports whether this machine can put IPv6 packets on the wire right now.
//
// The answer decides what a generated config is allowed to do with the second address
// family. sing-box answers a DNS query with A *and* AAAA unless a strategy says
// otherwise, and a machine with no IPv6 route then dials an address the routing table
// has no path to: "dial tcp [240e:...]:443 ... A socket operation was attempted to an
// unreachable network" in the core log, for a connection that would have worked over
// the A record sitting next to the AAAA one.
//
// It is measured rather than configured because the same binary runs on networks that
// have IPv6 and networks that do not — a reader who wants both needs the config to
// follow the network, not a switch to remember on every network they join. Three
// properties make the measurement worth trusting:
//
//   - It binds the probe to a global IPv6 address of a physical interface, so the
//     source address picks the interface. The tun adapter therefore cannot answer for
//     the network behind it, and a proxy node cannot answer on this machine's behalf:
//     the question is whether *this* machine can send v6, and only its own addresses
//     can answer it.
//   - It asks for a TCP handshake rather than a route. A route can exist with nothing
//     behind it (a default route on a network whose upstream is broken, a tunnel that
//     is up but not carrying), and a route alone would read as support in exactly the
//     case that produces the errors above.
//   - It fails towards IPv4. A network whose v6 works but blocks these probes reads as
//     "no IPv6" and the config stays IPv4 — a slower answer, never an unreachable one.
//     The opposite mistake would break a working network, so the probes are several,
//     concurrent, and capped: at most about a second, and usually far less, since a
//     machine with no global IPv6 address has nothing to try and a machine with an
//     address but no route is refused by the kernel immediately.
//
// The answer is cached for a few minutes: a config is built at node start, mode switch
// and TUN bring-up, and none of those should wait on a network round trip they have
// already paid for. InvalidateIPv6 drops it when the routing under it has changed.
const (
	ipv6ProbeTimeout = 1200 * time.Millisecond
	ipv6CacheTTL     = 5 * time.Minute
)

var (
	ipv6Mu  sync.Mutex
	ipv6Val bool
	ipv6At  time.Time
)

// HasIPv6 answers the question above, from cache when the cache is young.
func HasIPv6() bool {
	ipv6Mu.Lock()
	defer ipv6Mu.Unlock()
	if !ipv6At.IsZero() && time.Since(ipv6At) < ipv6CacheTTL {
		return ipv6Val
	}
	ipv6Val = probeIPv6()
	ipv6At = time.Now()
	return ipv6Val
}

// InvalidateIPv6 drops the cached answer so the next one is measured afresh. A mode
// switch is the moment for it: bringing the tun adapter up or down changes the routing
// table the probe would otherwise be reading, and the TUN config about to be written is
// the very thing the answer feeds.
func InvalidateIPv6() {
	ipv6Mu.Lock()
	ipv6At = time.Time{}
	ipv6Mu.Unlock()
}

// probeIPv6 performs the measurement described on HasIPv6: bind a probe to each global
// IPv6 address this machine owns and try to complete a handshake over it.
//
// Route first, handshake second. The lookup is what makes the common case cheap: a
// machine with an IPv6 address but no route to the IPv6 internet — a DHCPv6 lease on a
// network that never advertised one, the leftovers of a tun adapter — is refused by the
// kernel the moment it is asked, and without this step that same machine would sit
// through a timeout on every handshake probe instead, which is the difference between a
// config build that pauses for milliseconds and one that pauses for a second.
func probeIPv6() bool {
	var routable []net.IP
	for _, src := range ipv6Sources() {
		if hasIPv6Route(src) {
			routable = append(routable, src)
		}
	}
	if len(routable) == 0 {
		// Nothing to send from: no global v6 anywhere, so there is no v6 to support
		// and no packet to send in order to find that out.
		return false
	}
	// Four targets, all asked at once. The AliDNS anycast pair is what a domestic
	// profile sends direct; Cloudflare and Google stand in for a network that cannot
	// reach the first pair, so that "IPv6 works here" does not depend on which continent
	// the machine is on. One handshake from any of them is enough.
	targets := []string{
		"[2400:3200::1]:443",
		"[2400:3200:baba::1]:443",
		"[2606:4700:4700::1111]:443",
		"[2001:4860:4860::8888]:443",
	}
	done := make(chan struct{})
	var once sync.Once
	for _, src := range routable {
		for _, dst := range targets {
			go func(src net.IP, dst string) {
				d := net.Dialer{
					Timeout:   ipv6ProbeTimeout,
					LocalAddr: &net.TCPAddr{IP: src},
				}
				c, err := d.Dial("tcp6", dst)
				if err != nil {
					return
				}
				c.Close()
				once.Do(func() { close(done) })
			}(src, dst)
		}
	}
	select {
	case <-done:
		return true
	case <-time.After(ipv6ProbeTimeout):
		return false
	}
}

// hasIPv6Route asks the kernel whether a packet to the IPv6 internet can leave from
// this address at all. A UDP connect() performs the route lookup and sends nothing, so
// the answer costs a syscall rather than a round trip — and no route is an error there
// and then, which is the whole point of asking before dialling anything.
func hasIPv6Route(src net.IP) bool {
	d := net.Dialer{
		Timeout:   ipv6ProbeTimeout,
		LocalAddr: &net.UDPAddr{IP: src},
	}
	c, err := d.Dial("udp6", "[2400:3200::1]:53")
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// ipv6Sources lists the global IPv6 addresses a probe may bind to.
//
// Two kinds of address are deliberately left out, and each for the same reason: it
// would let the probe be answered by something other than this machine's own network.
// Unique-local addresses (fc00::/7) are the tell for a tunnel — the tun inbound
// declares fdfe:dcba:9876::1, and a probe bound to it would be asking whether the core
// can reach the internet, which is a different question and one a proxy profile could
// answer yes to on a machine whose IPv6 is broken. Loopback and down interfaces go
// without saying, as do tunnel devices by name (EagleTun, utun, …) — a global address
// on one of those belongs to somebody else's tunnel.
func ipv6Sources() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isTunnelInterface(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			network, ok := a.(*net.IPNet)
			if !ok || network.IP.To4() != nil {
				continue // IPv4, or not an address at all
			}
			ip := network.IP.To16()
			if ip == nil || !ip.IsGlobalUnicast() {
				continue // unspecified, loopback, multicast, link-local
			}
			if ip[0]&0xfe == 0xfc {
				continue // fc00::/7 — unique local, tunnels live here
			}
			out = append(out, ip)
		}
	}
	return out
}

// isTunnelInterface reports whether an interface name belongs to a tunnel rather than
// to the network this machine is on.
func isTunnelInterface(name string) bool {
	n := strings.ToLower(name)
	if n == "eagletun" {
		return true
	}
	for _, prefix := range []string{"utun", "tun", "tap", "wintun", "wg", "wireguard"} {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}
