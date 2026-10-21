//go:build windows

package tunfast

import (
	"net/netip"
	"testing"
)

// TestClearSelfDNSTakesTheResolverAway covers the moment the machine has to stop
// asking the adapter for names.
//
// The adapter's resolver points at the adapter, where the main core's hijack-dns answers
// with the profile's DNS. With no core behind it, every query waits out a timeout — and
// the routes are already back on the physical adapter by then, so the machine has an
// internet connection and no way to use it.
//
// The adapter itself stays: it is the expensive half, and the next start reuses it.
func TestReviewClearSelfDNSTakesTheResolverAway(t *testing.T) {
	name := "EagleTun"
	// The adapter needs an address of its own for the self resolver to exist: the
	// address the machine is pointed at is a peer of it. The front-end gives the
	// adapter that address while it runs; this gives it the same one for the length
	// of the test, and the routes are left alone (AutoRoute is off) — the machine's
	// traffic must not move for a test about a resolver.
	iface, err := Open(Options{
		Name:         name,
		Inet4Address: []netip.Prefix{netip.MustParsePrefix("172.19.0.1/28")},
		MTU:          1500,
	})
	if err != nil {
		t.Skipf("the adapter could not be brought up here: %v", err)
	}
	defer func() { _ = iface.Close() }()

	l, err := Find(name)
	if err != nil {
		t.Skipf("no adapter named %s after bringing it up: %v", name, err)
	}
	// The self address is a peer of the adapter's own address, so there is one only
	// while the adapter carries an address.
	self := l.resolverAddrs()
	if len(self) == 0 {
		t.Skipf("the adapter carries no address, so it has no self resolver to take away")
	}
	if err := SelfDNS(name); err != nil {
		t.Skipf("this process may not hold the rights to write an interface's resolver: %v", err)
	}
	before, err := l.luid.DNS()
	if err != nil {
		t.Fatalf("read the adapter's resolvers: %v", err)
	}
	if !carriesAddr(before, self) {
		t.Skipf("the adapter's resolver list %v does not carry its own address %v", before, self)
	}

	if err := ClearSelfDNS(name); err != nil {
		t.Fatalf("ClearSelfDNS: %v", err)
	}
	after, err := l.luid.DNS()
	if err != nil {
		t.Fatalf("read the adapter's resolvers: %v", err)
	}
	if carriesAddr(after, self) {
		t.Errorf("the adapter still points at itself: %v (the self address is %v). The machine "+
			"would keep asking a peer that has no core behind it, and every name would wait "+
			"out a timeout", after, self)
	}
}

// carriesAddr reports whether the list holds any of the wanted addresses.
func carriesAddr(list []netip.Addr, want []netip.Addr) bool {
	for _, a := range list {
		for _, w := range want {
			if a == w {
				return true
			}
		}
	}
	return false
}
