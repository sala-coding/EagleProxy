package configbuilder

import "testing"

// The IPv6-literal check decides whether a reader's own resolver stands in the config
// or is replaced by the app's default, so getting an address wrong either way is a
// resolver the network cannot use (see buildDNS).
func TestDNSHostIsIPv6(t *testing.T) {
	cases := map[string]bool{
		"":                              false,
		"2400:3200:baba::1":             true,
		"2400:3200::1":                  true,
		"tls://[2400:3200:baba::1]":     true,
		"tls://[2400:3200:baba::1]:853": true,
		"223.5.5.5":                     false,
		"tls://1.1.1.1:853":             false,
		"https://doh.pub/dns-query":     false,
		"dns.google":                    false,
		"local":                         false,
	}
	for raw, want := range cases {
		if got := dnsHostIsIPv6(raw); got != want {
			t.Errorf("dnsHostIsIPv6(%q) = %v, want %v", raw, got, want)
		}
	}
}

// An address with a port must not read the trailing group of an IPv6 literal as the
// port — the reason splitDNSAddress only splits on a colon outside brackets.
func TestSplitDNSAddressIPv6(t *testing.T) {
	typ, host, port, _ := splitDNSAddress("tls://[2400:3200:baba::1]:853")
	if typ != "tls" || host != "2400:3200:baba::1" || port != 853 {
		t.Errorf("got typ=%q host=%q port=%d", typ, host, port)
	}
	_, host, port, _ = splitDNSAddress("2400:3200:baba::1")
	if host != "2400:3200:baba::1" || port != 0 {
		t.Errorf("bare literal read as host=%q port=%d", host, port)
	}
}
