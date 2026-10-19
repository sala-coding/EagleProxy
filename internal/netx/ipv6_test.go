package netx

import (
	"testing"
	"time"
)

// The names that must not be asked about — a probe bound to one of them would be
// answered by a tunnel rather than by this machine's own network.
func TestIsTunnelInterface(t *testing.T) {
	cases := map[string]bool{
		"EagleTun":                     true,
		"eagletun":                     true,
		"utun9":                        true,
		"tun0":                         true,
		"TAP-Windows Adapter V9":       true,
		"wintun":                       true,
		"WireGuard Tunnel":             true,
		"WLAN":                         false,
		"以太网":                          false,
		"vEthernet (WSL)":              false,
		"Local Area Connection":        false,
		"Bluetooth Network Connection": false,
	}
	for name, want := range cases {
		if got := isTunnelInterface(name); got != want {
			t.Errorf("isTunnelInterface(%q) = %v, want %v", name, got, want)
		}
	}
}

// The measurement is on the path of every config build, so it has to be bounded: a
// probe that hangs would hold up starting a node, which is the one thing a reader is
// waiting on. The cap inside probeIPv6 is ~1.2s; the allowance here is for scheduling
// and for the machines where a dial has to time out rather than fail at once.
func TestHasIPv6IsBounded(t *testing.T) {
	InvalidateIPv6()
	start := time.Now()
	_ = HasIPv6()
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("HasIPv6 took %v, want at most 5s", took)
	}
	// A second call answers from cache and must not pay for the network again.
	InvalidateIPv6()
	_ = HasIPv6()
	start = time.Now()
	_ = HasIPv6()
	if took := time.Since(start); took > 50*time.Millisecond {
		t.Errorf("cached HasIPv6 took %v, want at most 50ms", took)
	}
}
