package update_test

import (
	. "eagle/internal/update"

	"testing"
)

// TestReviewPreReleaseNumbersCompareAsText pins down how two pre-release builds of the
// same version are ordered.
//
// parseVersion keeps the pre-release suffix as a string and Compare orders the two
// suffixes with a plain string comparison. Semver says the numeric part of a
// pre-release identifier is compared as a number, so beta.10 is NEWER than beta.9.
// A string comparison says the opposite: "beta.10" < "beta.9" because '1' < '9'.
//
// The consequence is not academic: the app asks Compare whether the release on offer is
// newer than the build it is running, and a build tagged v1.2.0-beta.10 is told that
// v1.2.0-beta.9 is newer — so the About card offers a downgrade, and a build tagged
// v1.2.0-beta.9 is told that v1.2.0-beta.10 is not newer, so the update it is missing
// is never offered.
func TestReviewPreReleaseNumbersCompareAsText(t *testing.T) {
	c, ok := Compare("1.2.0-beta.10", "1.2.0-beta.9")
	if !ok {
		t.Fatalf("the versions could not be compared at all")
	}
	if c <= 0 {
		t.Errorf("1.2.0-beta.10 compared as NOT newer than 1.2.0-beta.9 (got %d): the "+
			"pre-release numbers are compared as text, so beta.9 is offered as an update "+
			"to a beta.10 build", c)
	}

	c2, ok2 := Compare("1.2.0-beta.9", "1.2.0-beta.10")
	if !ok2 {
		t.Fatalf("the versions could not be compared at all")
	}
	if c2 >= 0 {
		t.Errorf("1.2.0-beta.9 compared as NOT older than 1.2.0-beta.10 (got %d): the "+
			"newer pre-release is never offered", c2)
	}
}
