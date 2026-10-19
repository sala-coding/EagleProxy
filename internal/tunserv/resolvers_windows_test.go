//go:build windows

package tunserv

import (
	"testing"

	"eagle/internal/tunfast"
)

// The bootstrap list the forwarder gets is the machine's pre-takeover resolvers. What must
// never be in it: loopback (that is the takeover itself, or another local resolver — asking
// either is the circle this exists to break) and a link-local address (it needs a scope the
// record does not carry). Duplicates across interfaces are collapsed.
func TestOriginalResolversSkipsWhatCannotBootstrap(t *testing.T) {
	dir := t.TempDir()
	snapshot := []tunfast.AdapterResolvers{
		{Name: "WLAN", LUID: 1, V4: []string{"192.168.1.1", "127.0.0.1"}, V6: []string{"::1", "fe80::1"}},
		{Name: "Ethernet", LUID: 2, V4: []string{"192.168.1.1", "223.5.5.5"}},
	}
	if err := writeUndo(dir, snapshot); err != nil {
		t.Fatalf("writeUndo: %v", err)
	}
	got := OriginalResolvers(dir)
	want := []string{"192.168.1.1", "223.5.5.5"}
	if len(got) != len(want) {
		t.Fatalf("OriginalResolvers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OriginalResolvers = %v, want %v", got, want)
		}
	}
}

// Nothing taken over means no record, and no record means the system resolver is still the
// right thing to ask — an empty list, not a guess.
func TestOriginalResolversWithoutARecord(t *testing.T) {
	if got := OriginalResolvers(t.TempDir()); len(got) != 0 {
		t.Errorf("OriginalResolvers = %v with no undo record, want none", got)
	}
}
