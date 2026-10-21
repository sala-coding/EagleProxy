//go:build windows

package tunserv_test

import (
	. "eagle/internal/tunserv"

	"testing"
)

// TestStateCarriesTheNodeFact pins the third fact the app publishes.
//
// The adapter's resolver may only point at the adapter while a node is serving: the
// address it names is answered by the main core's hijack-dns, and with no core behind it
// every query the machine sends it waits out a timeout — a machine that cannot resolve a
// name at all, which is worse than the cleartext leak the resolver was there to prevent.
//
// So the app has to say which of the two it is, and the elevated side can only act on
// what it is told.
func TestReviewStateCarriesTheNodeFact(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir, State{Adapter: true, Resolvers: true, Node: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	got := ReadState(dir)
	if !got.Adapter || !got.Resolvers || !got.Node {
		t.Fatalf("ReadState = %+v, want all three facts set", got)
	}

	// And it can be taken back: the node stopping is the ordinary case.
	if err := WriteState(dir, State{Adapter: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	got = ReadState(dir)
	if got.Node {
		t.Errorf("ReadState = %+v, want the node fact cleared", got)
	}
	if !got.Adapter {
		t.Errorf("ReadState = %+v, want the adapter fact kept", got)
	}
}
