//go:build windows

package tunserv

import (
	"os"
	"testing"
)

// The state file is the whole contract between the app and the elevated side, and its
// failure modes are the ones that matter: a file that is missing or unreadable has to read
// as "nothing wanted" — which leaves the machine's resolvers alone — rather than as "take
// them over", which is the answer that would break a machine whose app had just died.
func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := ReadState(dir); got.Adapter || got.Resolvers {
		t.Errorf("a directory with no state file answered %+v, want nothing wanted", got)
	}
	want := State{Adapter: true, Resolvers: true}
	if err := WriteState(dir, want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if got := ReadState(dir); got != want {
		t.Errorf("ReadState = %+v, want %+v", got, want)
	}
	// Both switches off is a real state as well — the app writes it as a node stops.
	if err := WriteState(dir, State{}); err != nil {
		t.Fatalf("WriteState(off): %v", err)
	}
	if got := ReadState(dir); got.Adapter || got.Resolvers {
		t.Errorf("ReadState after switching off = %+v, want nothing wanted", got)
	}
	// A file that will not parse is treated as nothing wanted, for the same reason.
	if err := WriteState(dir, State{Resolvers: true}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if err := os.WriteFile(statePath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupting the file: %v", err)
	}
	if got := ReadState(dir); got.Adapter || got.Resolvers {
		t.Errorf("a corrupt state file answered %+v, want nothing wanted", got)
	}
}

// With nothing taken over, both directions are no-ops — and the restore path in particular
// must not fail: it runs on the server's way out, including for a session that never touched
// the machine's resolvers (system-proxy mode with no node ever started).
func TestReconcileAndRestoreAreQuietWhenNothingWasTaken(t *testing.T) {
	dir := t.TempDir()
	ReconcileResolvers(dir, false)
	RestoreResolvers(dir)
	if undoExists(dir) {
		t.Error("an undo record was written for a machine nothing was done to")
	}
}
