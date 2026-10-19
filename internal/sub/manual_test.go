package sub

// A manual test, skipped unless SUBPROBE=1, that runs the walk against a real
// panel from the working copy's own config (gid 7). Read-only: it prints the
// walk's own log lines and nothing else, so the picker can be checked against
// whatever the panel answers today without touching the store or putting the
// address token into a fixture.
//
//	go test ./internal/sub/ -run TestManualRealPanel -v   # skips
//	SUBPROBE=1 go test ./internal/sub/ -run TestManualRealPanel -v -count=1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"eagle/internal/setting"
)

func TestManualRealPanel(t *testing.T) {
	if os.Getenv("SUBPROBE") == "" {
		t.Skip("manual: set SUBPROBE=1 to run against the real panel")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "groups", "7.json"))
	if err != nil {
		t.Skipf("no fixture: %v", err)
	}
	var g struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Skipf("bad fixture: %v", err)
	}
	s := setting.Load(filepath.Join("..", "..", "config"))
	resp, ok := requestSubscription(g.URL, "test-sub", s, 0, func(line string) { t.Log(line) })
	if !ok {
		t.Fatalf("walk failed: %s", resp.Error)
	}
	count, kind := probeWith(resp.Data, s)
	t.Logf("WINNER kind=%q count=%d hasUserInfo=%v", kind, count, resp.Header.Get("Subscription-UserInfo") != "")
}
