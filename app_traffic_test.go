package main

import (
	"testing"

	"eagle/internal/clash"
)

// The LAN-device count is what the Wi-Fi icon's badge shows, so the filter it
// applies is worth pinning: loopback in both families, the machine's own LAN
// address, and empty entries are not devices, and two machines holding several
// connections each are still two machines.
func TestCountLanDevices(t *testing.T) {
	conns := []clash.Connection{
		{Metadata: clash.Metadata{SourceIP: "127.0.0.1"}},
		{Metadata: clash.Metadata{SourceIP: "127.0.0.1"}},
		{Metadata: clash.Metadata{SourceIP: "::1"}},
		{Metadata: clash.Metadata{SourceIP: ""}},
		{Metadata: clash.Metadata{SourceIP: "192.168.1.18"}}, // this machine itself
		{Metadata: clash.Metadata{SourceIP: "192.168.1.15"}},
		{Metadata: clash.Metadata{SourceIP: "192.168.1.15"}},
		{Metadata: clash.Metadata{SourceIP: "192.168.1.15"}},
		{Metadata: clash.Metadata{SourceIP: "10.0.0.7"}},
	}
	if got := countLanDevices(conns, map[string]bool{"192.168.1.18": true}); got != 2 {
		t.Fatalf("countLanDevices = %d, want 2", got)
	}
	// A machine whose own addresses could not be read degrades to the loopback
	// filter only: nothing is assumed about the rest.
	if got := countLanDevices(conns, nil); got != 3 {
		t.Fatalf("countLanDevices(self=nil) = %d, want 3", got)
	}
}
