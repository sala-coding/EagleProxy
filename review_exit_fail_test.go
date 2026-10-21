package main

import "testing"

// The reader has to be told when the node cannot carry traffic.
//
// This is the one failure they cannot see for themselves: the corner keeps the last
// address it had, the proxy answers on its port, and the only sign is that nothing
// loads — which reads as "the app is broken" rather than "this node is dead". The
// app already knew, from the probe, and said it only in the log.
func TestReviewExitFailureIsSaidOnceAndOnlyAfterTwo(t *testing.T) {
	a := &App{}
	// One failure is not enough: the echo services are other people's servers.
	a.noteExitFailure()
	if len(a.pendingNotice) != 0 {
		t.Fatalf("a single failed probe told the reader the node was dead (%d notices)", len(a.pendingNotice))
	}
	// Two in a row, each asked through the node and each against several services
	// at once, is a fact about the node.
	a.noteExitFailure()
	if len(a.pendingNotice) != 1 {
		t.Fatalf("two failed probes said nothing (%d notices)", len(a.pendingNotice))
	}
	// And once, not on every poll.
	a.noteExitFailure()
	if len(a.pendingNotice) != 1 {
		t.Errorf("the failure was said more than once (%d notices)", len(a.pendingNotice))
	}
	// A probe that answers ends the streak: the next node is a different story.
	a.noteExitSuccess()
	a.noteExitFailure()
	a.noteExitFailure()
	if len(a.pendingNotice) != 2 {
		t.Errorf("a new streak did not say itself (%d notices)", len(a.pendingNotice))
	}
}
