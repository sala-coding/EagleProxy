//go:build windows

// This file is the half of tunfast that runs after the adapter is already up.
// The device itself belongs to the process that opened it (the front-end core),
// and what the app can still move is its routes: taking them installs the
// capture, releasing them hands the machine back to its physical adapter.
//
// That split is the whole reason switching TUN mode off and on again is cheap.
// Destroying and rebuilding an adapter is a PnP create/remove — seconds, on this
// machine 3-4 of them, and the reason TUN used to feel slow at both ends. The
// device is never touched here, so none of that cost enters the cycle; only the
// two /1 routes and the interface metric do, and those are microseconds.

package tunfast

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"

	"eagle/internal/tunfast/internal/winipcfg"

	"golang.org/x/sys/windows"
)

// Link is a handle on an adapter that is already up, found by its network
// connection name — the same name sing-box was told to use, which is what
// Windows shows in 网络连接.
//
// It owns nothing. The device stays with the process that opened it, so there
// is no Close here: closing the adapter is that process's business, and doing
// it from here would tear the session out from under its reader.
type Link struct {
	name string
	luid winipcfg.LUID
}

// Find locates an adapter by name and reports ErrNotFound when the machine has
// none. Every other error is the interface table refusing to be read.
func Find(name string) (*Link, error) {
	if name == "" {
		return nil, errors.New("tunfast: adapter name is required")
	}
	rows, err := winipcfg.GetIfTable2Ex(winipcfg.MibIfEntryNormal)
	if err != nil {
		return nil, fmt.Errorf("tunfast: list interfaces: %w", err)
	}
	for i := range rows {
		if rows[i].Alias() == name {
			return &Link{name: name, luid: rows[i].InterfaceLUID}, nil
		}
	}
	return nil, fmt.Errorf("tunfast: adapter %q: %w", name, ErrNotFound)
}

// ErrNotFound reports an adapter the machine does not have — the ordinary
// answer when TUN mode is off and nothing has been built yet, which callers
// treat as "nothing to release" rather than as a failure.
var ErrNotFound = errors.New("adapter not found")

// Take installs the capture: the default route of every address family the
// adapter carries is written as two /1 halves at metric 0, and the interface
// metric is pinned to 0 so those halves beat the physical adapter's default
// route.
//
// Halves rather than 0.0.0.0/0, and a metric rather than a rewrite: Windows
// matches the most specific prefix first, so the two /1 routes win while the
// physical default route is left exactly where it was. Releasing is then a
// delete of what was added — nothing has to be restored from memory, which is
// what keeps the cycle symmetric and cheap.
//
// Taking is idempotent, and that matters more than it looks: a capture left
// behind by a release that failed is exactly the state a reader retries from,
// and a route that is already there answers Create with ERROR_OBJECT_ALREADY_
// EXISTS — a second switch-on that reports failure over a capture that is in
// fact in place. So whatever is there goes first.
func (l *Link) Take() error {
	v4, v6 := l.families()
	if !v4 && !v6 {
		return fmt.Errorf("tunfast: adapter %q carries no address to route", l.name)
	}
	if err := l.clearCapture(); err != nil {
		return err
	}
	if err := l.ensureOnLink(); err != nil {
		return err
	}
	// From here the machine is being captured, and every failure below puts it
	// back: the old code returned the error straight through, leaving whichever
	// /1 halves had already landed in the table — and by longest-prefix match a
	// half-capture is still a capture, so the caller was told "not taken" while
	// the machine was in fact half in the tunnel (or, on the metric failure,
	// fully in it with a metric nothing had pinned). Release is the symmetric
	// undo: routes gone, metric back to idle, and a no-op against a state it
	// did not create.
	for _, prefix := range capturePrefixes(v4, v6) {
		if err := addRouteList(l.luid, []netip.Prefix{prefix}); err != nil {
			_ = l.Release()
			return fmt.Errorf("tunfast: capture route %s on %q: %w", prefix, l.name, err)
		}
	}
	if err := l.setMetric(v4, v6, linkMetricCaptured); err != nil {
		_ = l.Release()
		return err
	}
	return nil
}

// linkMetricCaptured and linkMetricIdle are the two interface metrics this
// package writes: 0 wins over every physical adapter, 9999 loses to all of them.
const (
	linkMetricCaptured = 0
	linkMetricIdle     = 9999
)

// Release hands the machine back: the capture's routes go, and the interface is
// put at the back of Windows' queue.
//
// Only the capture goes. The adapter's own routes — the on-link entry for its
// address, the multicast and broadcast entries Windows keeps for it — are not
// the capture and are not this function's to delete: removing them leaves an
// interface that still has an address and no record that the address is local,
// which is an adapter that can receive nothing. An earlier cut flushed every
// route on the interface and did exactly that, which is why the delete below is
// by prefix rather than by interface.
//
// Releasing twice is a no-op, which is what a stop path wants: it runs on states
// it did not create and must not fail over a capture that is already gone.
func (l *Link) Release() error {
	// The metric goes first. Release is a "hand the machine back" that runs on
	// states it did not create, and each half can fail on its own — the order
	// decides which failure leaves the machine behind. Metric first: the
	// interface stops winning Windows' queue at once, so whatever happens
	// below, the reader's traffic is already on the physical adapter. The old
	// order (routes first, metric after) had the two shapes that do not: a
	// route cut that failed part-way left a broken capture under a metric
	// that still won it (a black hole the reader meets as "no internet"),
	// and a metric write that failed after the cut left the interface
	// claiming the queue with nothing captured.
	v4, v6 := l.families()
	if err := l.setMetric(v4, v6, linkMetricIdle); err != nil {
		// The metric is stuck high, so the capture's routes are the machine's
		// only honest answer for now: cut them anyway — a failed release is
		// reported either way, and leaving both up is the black hole.
		_ = l.clearCapture()
		return err
	}
	return l.clearCapture()
}

// clearCapture removes the routes this package writes: the /1 halves of a
// default route, and a /0 if one was ever written by hand. Anything longer than
// a /1 is somebody else's — the adapter's own on-link routes above all — and is
// left alone.
//
// Releasing twice is a no-op rather than an error, which is what a stop path
// wants: it runs on states it did not create and must not fail over a capture
// that is already gone.
func (l *Link) clearCapture() error {
	return clearCaptureRoutes(l.luid, l.name)
}

// clearCaptureRoutes is the capture removal on a bare LUID, so the Open failure
// path can take back what startRoutes put down without a Link to go through
// (see tun_windows.go): the /1 halves of a default route and any /0, on that
// interface only — longer prefixes are somebody else's, above all the adapter's
// own on-link routes.
func clearCaptureRoutes(luid winipcfg.LUID, name string) error {
	var firstErr error
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		rows, err := winipcfg.GetIPForwardTable2(family)
		if err != nil {
			// A table that cannot be read is not an empty table. Answering nil
			// here made Release report "handed back" while the capture routes
			// might well still be in the table it could not look at — the switch
			// reads off over a machine still routed into the tunnel. The error
			// travels up: the release path retries it and logs it.
			if firstErr == nil {
				firstErr = fmt.Errorf("tunfast: read forward table (%d): %w", family, err)
			}
			continue
		}
		for i := range rows {
			if rows[i].InterfaceLUID != luid {
				continue
			}
			prefix := rows[i].DestinationPrefix.Prefix()
			if !prefix.IsValid() || prefix.Bits() > 1 {
				continue
			}
			if err := rows[i].Delete(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("tunfast: remove capture route %s on %q: %w", prefix, name, err)
			}
		}
	}
	return firstErr
}

// ensureOnLink puts back the on-link route for each address the adapter carries,
// when it is missing.
//
// This is repair, not routine: a healthy adapter has these from the moment its
// address is assigned, and nothing here should ever need to write one. What it
// covers is an adapter that has already been damaged — by an older release that
// flushed the whole interface, or by anything else that took them away — and an
// adapter with an address and no on-link route is one whose own address is
// unreachable, so the packets coming back out of the tunnel have nowhere to go.
// Cheap enough to check on every take, and the check is what makes a damaged
// adapter heal on the next switch-on instead of staying broken.
func (l *Link) ensureOnLink() error {
	addrs, err := winipcfg.GetUnicastIPAddressTable(windows.AF_UNSPEC)
	if err != nil {
		// "Cannot see" used to answer "nothing to repair", and the take went on
		// to claim a capture whose precondition could not be checked — an
		// adapter whose own on-link route is missing is one whose replies never
		// arrive, and that is the failure this repair exists for. Failing the
		// take is honest: the caller reports it instead of running a tunnel that
		// answers nothing.
		return fmt.Errorf("tunfast: read address table: %w", err)
	}
	have := make(map[netip.Prefix]bool)
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		rows, err := winipcfg.GetIPForwardTable2(family)
		if err != nil {
			// Same rule as clearCapture: a table that cannot be read makes the
			// presence check a guess, and a wrong guess means either a
			// redundant write or a repair skipped.
			return fmt.Errorf("tunfast: read forward table (%d): %w", family, err)
		}
		for i := range rows {
			if rows[i].InterfaceLUID != l.luid {
				continue
			}
			if prefix := rows[i].DestinationPrefix.Prefix(); prefix.IsValid() {
				have[prefix] = true
			}
		}
	}
	for i := range addrs {
		if addrs[i].InterfaceLUID != l.luid {
			continue
		}
		ip := addrs[i].Address.Addr()
		if !ip.IsValid() {
			continue
		}
		// Masked, and the masking is the whole repair: PrefixFrom keeps the
		// host bits, and an address like 172.19.0.1/28 reads as the prefix
		// 172.19.0.1/28 — a destination Windows answers with "The parameter
		// is incorrect", and a miss against the table's 172.19.0.0/28 that
		// turned every healthy adapter into a "damaged" one needing this
		// function's write, which then aborted the whole take.
		prefix := netip.PrefixFrom(ip, int(addrs[i].OnLinkPrefixLength)).Masked()
		if have[prefix] {
			continue
		}
		if err := addRouteList(l.luid, []netip.Prefix{prefix}); err != nil {
			return fmt.Errorf("tunfast: restore on-link route %s on %q: %w", prefix, l.name, err)
		}
	}
	return nil
}

// SelfDNS points the adapter's resolver at the adapter itself.
//
// The server behind the address is the main core's hijack-dns rule: every DNS
// packet that enters the tunnel is sniffed, intercepted, and resolved by the
// core's own dns configuration — the profile's RemoteDNS and DirectDNS, the
// smart-select pick among them. That is why this needs no real resolver IP and
// never needs rewriting: the app can re-measure and switch its resolvers as
// often as it likes, and the adapter keeps pointing at one unchanging address
// while the policy behind it moves.
//
// This is also what the tray's connectivity badge reads, not the tunnel's
// traffic: Windows probes every interface on its own — resolve
// www.msftconnecttest.com, then fetch a fixed URL from it — and an adapter
// with no resolver fails the first step, so the tray reads 无法连接到
// Internet over a tunnel that is in fact carrying the machine's traffic. With
// the resolver pointed here, the probe's query is hijacked and answered like
// any other.
//
// Nothing is written while the adapter already holds the same list — the call
// sits in a loop that runs forever, and a resolver flush every ten seconds is
// churn NLA notices.
func SelfDNS(name string) error {
	l, err := Find(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // no adapter, nothing to point anywhere
		}
		return err
	}
	servers := l.resolverAddrs()
	if len(servers) == 0 {
		// No address yet — the core has not configured the adapter, and a
		// resolver with no route to itself is a lie. The next pass catches it.
		return nil
	}
	current, err := l.luid.DNS()
	if err != nil {
		return err
	}
	if sameAddrs(current, servers) {
		return nil
	}
	// SetDNS keeps only the addresses of the family it is called for, so the
	// one combined list goes to both calls and each family ends up with
	// exactly its own servers.
	if err := l.luid.SetDNS(windows.AF_INET, servers, nil); err != nil {
		return fmt.Errorf("tunfast: set dns on %q: %w", l.name, err)
	}
	if err := l.luid.SetDNS(windows.AF_INET6, servers, nil); err != nil {
		return fmt.Errorf("tunfast: set dns on %q: %w", l.name, err)
	}
	return nil
}

// ResolverAddrs answers the addresses the adapter's resolver is pointed at.
//
// It is what a caller asks when it wants to put a question to the resolver the way
// the machine's own probe would: the address is where the adapter's names are
// answered, and only the adapter knows which address that is. What it answers is
// what the adapter carries now, not what it ought to carry — an adapter that was
// never pointed anywhere has no resolver to ask, and saying otherwise would be a
// lie a caller could act on.
func ResolverAddrs(name string) ([]netip.Addr, error) {
	l, err := Find(name)
	if err != nil {
		return nil, err
	}
	addrs, err := l.luid.DNS()
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, errors.New("tunfast: the adapter carries no resolver yet")
	}
	return addrs, nil
}

// ClearSelfDNS takes the adapter's resolver away.
//
// The server behind the address is the main core's hijack-dns rule: every query
// that enters the tunnel is answered by the core's own DNS. With no core there
// is nothing behind it, and an adapter pointed at a peer that will not answer is
// a machine that cannot resolve a name at all — worse than the leakage the
// resolver was there to prevent.
//
// So the resolver goes back to where it was before the tunnel the moment the
// node that answered those queries is gone. The adapter itself stays: it is the
// expensive half, and the next start reuses it.
func ClearSelfDNS(name string) error {
	l, err := Find(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // no adapter, nothing to point anywhere
		}
		return err
	}
	current, err := l.luid.DNS()
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return nil // nothing is written, nothing to take away
	}
	// Flushed per family: SetDNS keeps only the addresses of the family it is
	// called for, and the adapter carried both.
	if err := l.luid.FlushDNS(windows.AF_INET); err != nil {
		return fmt.Errorf("tunfast: clear dns on %q: %w", l.name, err)
	}
	if err := l.luid.FlushDNS(windows.AF_INET6); err != nil {
		return fmt.Errorf("tunfast: clear dns on %q: %w", l.name, err)
	}
	return nil
}

// EgressIndex is the interface the machine's own traffic leaves by, with the tunnel passed
// over when one is up.
//
// It exists for the app's own DNS forwarder (internal/dnsstub). A query that process makes
// is an ordinary socket's query, so with TUN on it follows the machine's routes into the
// tunnel it serves: the front-end hands it to the main core, whose hijack answers it — and
// the answer would be asked of the forwarder again, a circle. Pinning the forwarder's dials
// to the real interface is what keeps a domestic resolver reachable without that circle:
// the query leaves the way the machine's traffic would if no tunnel were up.
//
// The default route with the lowest metric on any interface but the tunnel's. With no
// tunnel up at all this is simply the machine's default route, which is what the same
// caller wants in the other mode.
func EgressIndex(tunnelName string) (uint32, error) {
	skip := winipcfg.LUID(0)
	if tunnelName != "" {
		if l, err := Find(tunnelName); err == nil {
			skip = l.luid
		}
	}
	best := uint32(0)
	bestMetric := ^uint32(0)
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		rows, err := winipcfg.GetIPForwardTable2(family)
		if err != nil {
			continue
		}
		for i := range rows {
			if skip != 0 && rows[i].InterfaceLUID == skip {
				continue
			}
			prefix := rows[i].DestinationPrefix.Prefix()
			if !prefix.IsValid() || !prefix.Addr().IsUnspecified() || prefix.Bits() != 0 {
				continue // not a default route
			}
			if rows[i].Metric < bestMetric {
				bestMetric = rows[i].Metric
				best = rows[i].InterfaceIndex
			}
		}
	}
	if best == 0 {
		return 0, errors.New("tunfast: no default route outside the tunnel")
	}
	return best, nil
}

// resolverAddrs answers where the adapter's resolver should be pointed: one
// peer address per family, inside the adapter's own subnet but never the
// adapter's own address.
//
// The own address is exactly the wrong answer, and the first cut here used it:
// Windows delivers a packet addressed to a local IP over the loopback path, so
// a query for the adapter's own address never enters the tunnel at all — it
// times out against nothing, and the tray reads 无法连接到 Internet over a
// tunnel that carries the machine's traffic. A peer address in the same prefix
// is on-link — injected into the device — and the hijack-dns rule behind it
// answers without caring that no one owns the address.
func (l *Link) resolverAddrs() []netip.Addr {
	addrs, err := winipcfg.GetUnicastIPAddressTable(windows.AF_UNSPEC)
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for i := range addrs {
		if addrs[i].InterfaceLUID != l.luid {
			continue
		}
		ip := addrs[i].Address.Addr()
		if !ip.IsValid() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
			continue
		}
		prefix := netip.PrefixFrom(ip, int(addrs[i].OnLinkPrefixLength)).Masked()
		// The subnet's first host address, stepping past the adapter's own if
		// it happens to be that one (172.19.0.1/28 lands on 172.19.0.2). Any
		// peer address works, because the query is answered by the hijack and
		// not by a host at the address.
		candidate := prefix.Addr().Next()
		if candidate == ip {
			candidate = candidate.Next()
		}
		if !prefix.Contains(candidate) {
			continue // a /31 or /32 has no spare peer address to offer
		}
		out = append(out, candidate)
	}
	return out
}

// sameAddrs answers whether two resolver lists name the same servers, order
// and family interleaving aside — Windows hands the list back in enumeration
// order, which is not a promise this code needs to depend on.
func sameAddrs(a, b []netip.Addr) bool {
	if len(a) != len(b) {
		return false
	}
	as := make([]string, 0, len(a))
	for _, addr := range a {
		as = append(as, addr.String())
	}
	bs := make([]string, 0, len(b))
	for _, addr := range b {
		bs = append(bs, addr.String())
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// Addressed reports whether the adapter carries a global unicast address —
// the condition the core reaches a moment after opening the device, and the
// precondition of SelfDNS doing anything at all. Callers use it to wait for
// "the adapter is ready for its resolver" instead of sleeping blind.
func Addressed(name string) bool {
	l, err := Find(name)
	if err != nil {
		return false
	}
	addrs, err := winipcfg.GetUnicastIPAddressTable(windows.AF_UNSPEC)
	if err != nil {
		return false
	}
	for i := range addrs {
		if addrs[i].InterfaceLUID != l.luid {
			continue
		}
		ip := addrs[i].Address.Addr()
		if ip.IsValid() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast() {
			return true
		}
	}
	return false
}

// capturePrefixes is what a capture writes for the families the adapter carries:
// each /0 split into its two /1 halves, which is the shape that beats a physical
// default route without touching it.
func capturePrefixes(v4, v6 bool) []netip.Prefix {
	var out []netip.Prefix
	if v4 {
		out = append(out, halve(netip.MustParsePrefix("0.0.0.0/0"))...)
	}
	if v6 {
		out = append(out, halve(netip.MustParsePrefix("::/0"))...)
	}
	return out
}

// families reports which address families the adapter actually carries, so a
// v4-only tunnel never gets a v6 default route (and the reverse). Link-local
// addresses do not count: Windows puts one on every interface, including one
// that carries no usable IPv6 at all.
func (l *Link) families() (v4, v6 bool) {
	addrs, err := winipcfg.GetUnicastIPAddressTable(windows.AF_UNSPEC)
	if err != nil {
		return false, false
	}
	for i := range addrs {
		if addrs[i].InterfaceLUID != l.luid {
			continue
		}
		ip := addrs[i].Address.Addr()
		if !ip.IsValid() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
			continue
		}
		if ip.Is4() {
			v4 = true
		} else {
			v6 = true
		}
	}
	return v4, v6
}

// setMetric pins the interface metric: 0 while the capture is on, 9999 while the
// adapter is present but idle.
//
// A route's effective metric is its own plus the interface's, so 0 is what makes
// the /1 halves beat the physical default route even on a machine whose
// interface metric happens to be high. 9999 is the other half, and it is not a
// plain undo — a wintun adapter reports a 100 Gbps link, Windows computes an
// automatic metric of about 5 for it, and 5 is lower than any real adapter's.
// Windows then prefers it for everything it chooses per interface, the NCSI
// probe above all, which is what drew "EagleTun: no internet" over the tray icon
// while the machine's own connection was fine. An adapter that captures nothing
// has to sit behind the physical one, and 9999 is how it does.
func (l *Link) setMetric(v4, v6 bool, metric uint32) error {
	return setInterfaceMetric(l.luid, l.name, v4, v6, metric)
}

// setInterfaceMetric is the metric write on a bare LUID, shared with the Open
// failure path (see tun_windows.go).
func setInterfaceMetric(luid winipcfg.LUID, name string, v4, v6 bool, metric uint32) error {
	var firstErr error
	for _, f := range []struct {
		on bool
		af winipcfg.AddressFamily
	}{
		{v4, windows.AF_INET},
		{v6, windows.AF_INET6},
	} {
		if !f.on {
			continue
		}
		// A family the adapter has no interface entry for answers with an
		// error here; that is not a failure of the call that asked.
		row, err := luid.IPInterface(f.af)
		if err != nil {
			continue
		}
		row.UseAutomaticMetric = false
		row.Metric = metric
		if err := row.Set(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("tunfast: set interface metric on %q: %w", name, err)
		}
	}
	return firstErr
}
