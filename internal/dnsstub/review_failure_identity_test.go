package dnsstub

import (
	"testing"
)

// TestReviewFailuresOnAReplacedClientDoNotCount pins the accounting rule the
// breaker runs on: a failure belongs to the client it came back on.
//
// Both replacement paths (setSide with a new resolver, clientFor with a new
// proxy) close the old client with queries still in flight on it, and those
// exchanges return "the path is closed" — a local teardown, not an upstream
// that failed. Counted against whatever holds the seat, two such strays are
// exactly failThreshold: the side that replaced the failing one was placed
// into a ten-second SERVFAIL breaker without ever being asked, over a resolver
// that was fine. The window that produces it — changing the resolver, or the
// core restarting mid-page-load — is dense with queries, so the strays arrive
// together.
func TestReviewFailuresOnAReplacedClientDoNotCount(t *testing.T) {
	s := startOn(0, 0, nil)
	defer s.Close()

	s.setSide(Direct, "tcp://127.0.0.1:1", Wires{})
	old := s.clientFor(Direct)
	if old == nil {
		t.Fatal("the side built no client for a configured resolver")
	}

	// The resolver moves — the old client's seat is gone, its in-flight
	// queries fail a moment later on the closed path.
	s.setSide(Direct, "tcp://127.0.0.1:2", Wires{})
	s.noteFailure(Direct, old)
	s.noteFailure(Direct, old)

	s.mu.Lock()
	fails := s.sides[Direct].fails
	failing := s.sides[Direct].failing
	s.mu.Unlock()
	if fails != 0 || failing {
		t.Errorf("the replacement side inherited %d failure(s) (failing=%v) from a client it never had: its first real question starts near the breaker", fails, failing)
	}

	// A failure on the client that DOES hold the seat still counts — the rule
	// is identity, not forgiveness.
	current := s.clientFor(Direct)
	if current == nil || current == old {
		t.Fatal("the new side did not build its own client")
	}
	s.noteFailure(Direct, current)
	s.mu.Lock()
	fails = s.sides[Direct].fails
	s.mu.Unlock()
	if fails != 1 {
		t.Errorf("a failure on the current client did not count: fails=%d, want 1", fails)
	}
}
