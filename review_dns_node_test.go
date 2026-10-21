package main

import (
	"testing"

	"eagle/internal/core"
	"eagle/internal/setting"
)

// The fact the adapter's resolver depends on: there has to be a core behind it
// that answers.
//
//	cd e:\project\eagle-desk; go test -run TestReview ./

// TestReviewNodeServingNeedsACoreThatAnswers covers the fact the adapter's
// resolver depends on.
//
// The address the adapter's resolver names is answered by the main core's
// hijack-dns. With no core behind it — or with a core that is still booting and
// has not opened its inbound yet — every query the machine sends waits out a
// timeout, and the machine has an internet connection it cannot use. So the
// resolver may only point at the adapter while a node is up and answering.
func TestReviewNodeServingNeedsACoreThatAnswers(t *testing.T) {
	dir := t.TempDir()
	a := &App{dataDir: dir}
	a.setSetting(setting.Load(dir))

	if a.nodeServing() {
		t.Errorf("nodeServing with no controller at all: there is no core to answer anything")
	}

	// A controller with nothing running is the same answer.
	ctrl := core.New(nil, setting.Load(dir), dir)
	a.setController(ctrl)
	if a.nodeServing() {
		t.Errorf("nodeServing with no core running: the core is not there to answer")
	}
}
