package setting_test

import (
	. "eagle/internal/setting"

	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The settings file is written by marshalling the struct, and a string field
// that carries `omitempty` disappears from the JSON when it is empty. That is
// fine for a field nobody empties — and it is exactly what happens to every
// field the reader CAN empty: a hotkey binding, the PAC address, the bypass
// list, the user-agent list, the ignored-log list.
//
// The way a changed setting reaches the file is Replace(): the frontend's
// object is marshalled and unmarshalled back over a copy of the current
// settings. A key that is missing from that JSON keeps the value it had —
// so an emptied field never overwrites the old one, and the reader's change
// is thrown away without a word.
//
// The symptom for a hotkey is the sharpest one: the reader clears the binding,
// saves, and the key is still bound. The setting row shows the old binding
// again on the next read of the file.
func TestReviewClearedSettingIsNotRestored(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir)

	// what the file holds before the reader touches it
	s.HotkeyMainwindow = "Ctrl+Alt+E"
	s.SystemProxyPAC = "http://127.0.0.1:9/proxy.pac"
	s.SystemProxyBypass = "127.0.0.1;localhost"
	s.UserAgent = "mihomo/1.19.0"
	s.LogIgnore = []string{"noisy line"}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// the reader empties every one of them in the form
	in := *s
	in.HotkeyMainwindow = ""
	in.SystemProxyPAC = ""
	in.SystemProxyBypass = ""
	in.UserAgent = ""
	in.LogIgnore = []string{}

	next, err := s.Replace(&in)
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	cases := []struct {
		name string
		got  string
	}{
		{"hk_mw", next.HotkeyMainwindow},
		{"sp_pac", next.SystemProxyPAC},
		{"sp_bypass", next.SystemProxyBypass},
		{"user_agent2", next.UserAgent},
	}
	for _, c := range cases {
		if c.got != "" {
			t.Errorf("%s = %q, want %q — an emptied setting was thrown away and the old value kept", c.name, c.got, "")
		}
	}
	if len(next.LogIgnore) != 0 {
		t.Errorf("log_ignore = %v, want an empty list — the reader removed every entry and the old list was kept", next.LogIgnore)
	}
}

// The same, one step further: the value has to survive the file as well, not
// only the copy Replace() returns.
func TestReviewClearedSettingIsNotRestoredOnDisk(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir)
	s.HotkeyMainwindow = "Ctrl+Alt+E"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	in := *s
	in.HotkeyMainwindow = ""
	next, err := s.Replace(&in)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Save(); err != nil {
		t.Fatal(err)
	}

	// what the file says now
	data, err := os.ReadFile(filepath.Join(dir, "groups", "eagle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["hk_mw"]; ok {
		t.Errorf("the file still carries hk_mw = %v — the reader unbound the hotkey and it was written back", v)
	}

	// and what the next start reads
	back := Load(dir)
	if back.HotkeyMainwindow != "" {
		t.Errorf("the next launch reads hk_mw = %q, want %q — the binding the reader cleared came back", back.HotkeyMainwindow, "")
	}
}

// The keys the file holds that the reader can empty, listed so the shape of
// the problem is visible: every one of them carries `omitempty`.
func TestReviewEmptiableSettingsCarryOmitempty(t *testing.T) {
	emptiable := []string{
		"hk_mw", "hk_group", "hk_route", "hk_spmenu", "hk_toggle_node",
		"hk_next_node", "hk_restart_node", "hk_connections", "hk_logs",
		"hk_egress", "hk_toggle_tun", "hk_datadir",
		"sp_pac", "sp_bypass", "user_agent2", "log_ignore",
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "settings-meta.ts"))
	if err != nil {
		t.Skipf("the front end's settings list is not here: %v", err)
	}
	text := string(raw)
	for _, k := range emptiable {
		if !containsKey(text, k) {
			continue // not offered by the form
		}
		if !fieldHasOmitempty(k) {
			t.Errorf("%q is offered by the form and can be emptied, but the field does not carry omitempty — "+
				"check that this is still the case", k)
		}
	}
}

func containsKey(text, k string) bool {
	return len(text) > 0 && (indexOf(text, "'"+k+"'") >= 0 || indexOf(text, "\""+k+"\"") >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// fieldHasOmitempty reports whether the settings field with that JSON name
// carries `omitempty` — the tag that drops it from the file when it is empty.
func fieldHasOmitempty(name string) bool {
	t := reflect.TypeOf(Setting{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" {
			continue
		}
		parts := strings.Split(tag, ",")
		if parts[0] != name {
			continue
		}
		for _, p := range parts[1:] {
			if p == "omitempty" {
				return true
			}
		}
	}
	return false
}
