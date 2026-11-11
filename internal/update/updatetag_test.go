package update

import (
	"strings"
	"testing"
)

// The tag comes out of a redirect the release page answers, so the only thing that can
// go wrong is where the tag sits inside Location — and that shape is GitHub's, not
// this package's, which makes it worth asking for real once.
func TestLatestTagReadsTheRedirect(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	tag, err := LatestTag("sala-coding/EagleProxy")
	if err != nil {
		t.Fatalf("LatestTag: %v", err)
	}
	if !strings.HasPrefix(tag, "v") || strings.Contains(tag, "/") {
		t.Fatalf("tag = %q", tag)
	}
}
