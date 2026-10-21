package main

import "testing"

// TestReviewNCSIAnsweredTakesOnlyRealAnswers pins down what counts as an answer the
// system's connectivity probe would accept.
//
// The probe's allowance is short and a probe that loses is not asked again for
// minutes, so the difference between "the takeover answered" and "the takeover said
// SERVFAIL" is the difference between a badge that comes back now and a badge that
// comes back in minutes.
func TestReviewNCSIAnsweredTakesOnlyRealAnswers(t *testing.T) {
	// A header with no answer behind it: NOERROR, and nothing in the answer count.
	empty := make([]byte, 12)
	if nCSIAnswered(empty) {
		t.Error("an empty NOERROR was taken for an answer")
	}

	// The same header with an error code: SERVFAIL is what a takeover whose core is
	// not up gives, and the answer count says nothing — the probe would still be
	// left with an unresolved name.
	servfail := make([]byte, 12)
	servfail[3] = 0x82
	servfail[6] = 0x00
	servfail[7] = 0x01
	if nCSIAnswered(servfail) {
		t.Error("a SERVFAIL carrying an answer count was taken for an answer")
	}

	// A real answer: NOERROR and one answer behind it.
	answered := make([]byte, 12)
	answered[6] = 0x00
	answered[7] = 0x01
	if !nCSIAnswered(answered) {
		t.Error("a NOERROR with one answer was not taken for an answer")
	}

	// Too short to be a message at all.
	if nCSIAnswered([]byte{0, 1, 0x81, 0x80}) {
		t.Error("a truncated message was taken for an answer")
	}
}
