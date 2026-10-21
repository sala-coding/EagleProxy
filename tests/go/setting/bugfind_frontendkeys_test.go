package setting_test

// Every key the settings form writes has to be a key the settings file actually has.
//
// The form and the file are two declarations of the same list, in two languages, and
// nothing checks that they agree: the form builds a settings object, the backend
// unmarshals it into the struct, and a key the struct does not have is dropped on the
// way in — silently. The reader changes a setting, the window says it changed, the
// file does not carry it, and the next start is back where it was.
//
// This test reads the front end's own list (frontend/src/lib/settings-meta.ts) and
// checks every key in it against the struct, by reflection, so the two cannot drift
// apart without this failing.
//
// A key the form writes that the file does not have is a bug in one of two places:
// either the form offers a setting that cannot be saved, or the file has a field the
// form cannot reach. Both are worth a red test.

import (
	. "eagle/internal/setting"

	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// settingJSONNames is every JSON name the settings file carries, at the top level.
//
// The form's own list also names a key or two that belongs to a neighbouring file — // sniffing_mode is a field of a routing profile, not of the settings file, and the
// form hides it because the routing card owns it. Those are not settings, so they
// are not checked here: the list is about what the settings file can hold.
func settingJSONNames() map[string]bool {
	out := map[string]bool{}
	for _, v := range []any{Setting{}, Routing{}, InboundAuth{}, ExtraCore{}} {
		t := reflect.TypeOf(v)
		for i := 0; i < t.NumField(); i++ {
			tag := t.Field(i).Tag.Get("json")
			if tag == "" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			if name == "" || name == "-" {
				continue
			}
			out[name] = true
		}
	}
	return out
}

// The front end's own list of the keys the form offers, read out of its source: the
// two blocks that hold them, read as text because the file is TypeScript and this is
// Go.
var (
	groupsRe = regexp.MustCompile(`(?s)export const SETTING_GROUPS.*?export const FRAME_KEYS`)
	hiddenRe = regexp.MustCompile(`(?s)export const SETTING_HIDDEN.*?export const LANGUAGES`)
	quotedRe = regexp.MustCompile(`'([A-Za-z0-9_]+)'`)
)

func frontendSettingKeys(t *testing.T) []string {
	t.Helper()
	path := "../../frontend/src/lib/settings-meta.ts"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("the front end's settings list is not here (%v) — nothing to check against", err)
	}
	text := string(raw)
	seen := map[string]bool{}
	var out []string
	for _, re := range []*regexp.Regexp{groupsRe, hiddenRe} {
		block := re.FindString(text)
		if block == "" {
			t.Fatalf("could not find one of the two blocks in %s — the shape changed and this test must follow", path)
		}
		for _, m := range quotedRe.FindAllStringSubmatch(block, -1) {
			k := m[1]
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func TestFrontendSettingKeysExistInTheFile(t *testing.T) {
	have := settingJSONNames()
	var missing []string
	for _, k := range frontendSettingKeys(t) {
		// A nested address: the form reaches into a field the file holds
		// (inbound_auth.user, for one). The outer name is what has to exist.
		head := k
		if i := strings.Index(k, "."); i > 0 {
			head = k[:i]
		}
		if !have[head] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		report := "the settings form writes " + strconv.Itoa(len(missing)) +
			" key(s) the settings file does not have: " + strings.Join(missing, ", ") + "\n"
		// The report also goes to a file, so the list can be read without the test
		// runner's own output having to be captured.
		out := os.Getenv("EAGLE_KEY_REPORT")
		if out == "" {
			out = filepath.Join(os.TempDir(), "eagle-key-report.txt")
		}
		_ = os.WriteFile(out, []byte(report), 0o644)
		t.Fatalf("%s"+
			"each of them is dropped when the file is written, so the setting changes and "+
			"does not stick (internal/setting/setting.go, Setting)",
			report)
	}
}
