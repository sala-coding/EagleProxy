package main

import (
	"testing"
	"time"
)

// A node that cannot carry traffic must not be asked again straight away.
//
// The corner refreshes itself every two seconds, and each ask is a request to
// several echo services, each of which hangs until its own timeout, through a
// tunnel that is not moving. The answer cannot have changed in between, so the
// next one waits — and the log says the same thing once rather than fifty times.
func TestReviewExitProbeWaitsAfterAFailure(t *testing.T) {
	a := &App{}

	// A node that has not been asked yet is asked at once.
	if !a.exitProbeDue() {
		t.Fatal("the probe was refused before anything had failed")
	}

	// One empty answer: the next ask waits.
	a.noteExitFailure()
	if a.exitProbeDue() {
		t.Error("the probe was asked again straight after an empty answer")
	}

	// And it stays refused until the window is up.
	a.exitProbeMu.Lock()
	a.exitFailAt = time.Now().Add(-exitFailBackoff - time.Second)
	a.exitProbeMu.Unlock()
	if !a.exitProbeDue() {
		t.Error("the probe was refused after the backoff window was up: a node that comes back is never noticed")
	}

	// A node that answers is asked at once, whatever came before.
	a.noteExitSuccess()
	if !a.exitProbeDue() {
		t.Error("the probe was refused after the node answered")
	}
}

// The failure line is for the reader, not for the volume: a node that is down
// answers nothing every time, and a line per attempt buries the lines that are
// about something else.
func TestReviewExitFailureIsLoggedOnceInAWhile(t *testing.T) {
	a := &App{}

	// The first failure is written: nobody has said it yet.
	if !a.noteExitFailLog() {
		t.Fatal("the first failure was not written to the log")
	}

	// The next one, right behind it, is not: the line just before stands for it.
	if a.noteExitFailLog() {
		t.Error("the same failure was written to the log twice in a row")
	}

	// And once the window is up, the next one is written again.
	a.exitProbeMu.Lock()
	a.exitFailLoggedAt = time.Now().Add(-exitFailLogGap - time.Second)
	a.exitProbeMu.Unlock()
	if !a.noteExitFailLog() {
		t.Error("the failure was not written after the quiet window was up: a node that stays down is never mentioned again")
	}
}
