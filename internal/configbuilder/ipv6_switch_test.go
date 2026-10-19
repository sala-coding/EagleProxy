package configbuilder

import (
	"fmt"
	"strings"
	"testing"

	"eagle/internal/netx"
	"eagle/internal/setting"
)

// The address-family question has two answers depending on the settings card: measured
// when detection is on, taken from the settings when it is off. What must not happen is
// the two getting mixed — a measurement applied to a reader who switched it off, or a
// silent substitution of their own choices.
func TestIPv6AutoDetectSwitch(t *testing.T) {
	load := func() *setting.Setting {
		s := setting.Load(t.TempDir())
		s.VPNIPv6 = true
		return s
	}
	servers := func(b *builder) string {
		obj := b.buildDNS("proxy")
		return fmt.Sprint(obj["servers"])
	}
	resolverStrategy := func(b *builder) string {
		r := b.buildRoute("proxy")
		res, _ := r["default_domain_resolver"].(map[string]interface{})
		v, _ := res["strategy"].(string)
		return v
	}

	// Detection off: nothing measured, nothing substituted.
	s := load()
	s.IPv6AutoDetect = false
	s.Routing().DirectDNS = "2400:3200:baba::1"
	if !tunIPv6(s) {
		t.Error("tunIPv6 = false with detection off and the TUN switch on; the switch is the reader's and must stand")
	}
	b := &builder{s: s, globalProfiles: map[int]bool{}}
	if _, ok := b.buildDNS("proxy")["strategy"]; ok {
		t.Error("dns.strategy written with detection off; nothing may be decided for the reader")
	}
	if got := resolverStrategy(b); got != "" {
		t.Errorf("default_domain_resolver.strategy = %q with detection off, want empty", got)
	}
	if !containsIP(servers(b), "2400:3200:baba::1") {
		t.Error("IPv6-literal direct DNS replaced with detection off; the reader's resolver is theirs")
	}

	// Detection on: every answer follows the measurement, whatever it happens to be here.
	s = load()
	s.IPv6AutoDetect = true
	s.Routing().DirectDNS = "2400:3200:baba::1"
	usable := netx.HasIPv6()
	b = &builder{s: s, globalProfiles: map[int]bool{}}
	_, wroteStrategy := b.buildDNS("proxy")["strategy"]
	if wroteStrategy == usable {
		t.Errorf("dns.strategy written=%v while the network has IPv6=%v; want the two to disagree", wroteStrategy, usable)
	}
	if got, want := resolverStrategy(b), map[bool]string{true: "", false: "ipv4_only"}[usable]; got != want {
		t.Errorf("default_domain_resolver.strategy = %q, want %q (network has IPv6=%v)", got, want, usable)
	}
	if got := tunIPv6(s); got != usable {
		t.Errorf("tunIPv6 = %v with detection on, want the measurement %v", got, usable)
	}
	if usable && !containsIP(servers(b), "2400:3200:baba::1") {
		t.Error("IPv6-literal direct DNS replaced on a network that has IPv6")
	}
}

// containsIP reports whether a printed server list names the given host, which is all
// the shape this test needs from buildDNS's output.
func containsIP(list, host string) bool {
	return strings.Contains(list, host)
}
