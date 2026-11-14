package main

import (
	"os"
	"path/filepath"
	"testing"

	"eagle/internal/configbuilder"
)

func TestRuleSetFolder(t *testing.T) {
	cases := map[string]string{
		"geoip-cn":                  "CN",
		"geosite-cn":                "CN",
		"geosite-category-ru":       "RU",
		"geoip-us":                  "US",
		"geoip-ir":                  "IR",
		"geosite-category-ads-all":  "misc",
		"geosite-private":           "misc",
	}
	for tag, want := range cases {
		if got := ruleSetFolder(tag, ""); got != want {
			t.Errorf("ruleSetFolder(%q) = %q, want %q", tag, got, want)
		}
	}
	// A reader's note wins over the suffix, and a 2-letter note uppercases.
	if got := ruleSetFolder("myrules", "us"); got != "US" {
		t.Errorf("note folder = %q, want US", got)
	}
	for _, note := range []string{"..", "c1", "/x", "中日"} {
		if got := ruleSetFolder("myrules", note); got != "misc" {
			t.Errorf("invalid country note %q produced folder %q, want misc", note, got)
		}
	}
}

func TestMigrateAndFind(t *testing.T) {
	dir := t.TempDir()
	// A loose flat copy an older build left behind.
	if err := os.WriteFile(filepath.Join(dir, "geoip-cn.srs"), []byte("SRSdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	migrateRuleSetFolders(dir, map[string]string{})

	if _, err := os.Stat(filepath.Join(dir, "geoip-cn.srs")); !os.IsNotExist(err) {
		t.Errorf("flat file should be gone after migration")
	}
	moved := filepath.Join(dir, "CN", "geoip-cn.srs")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("expected moved file at %s: %v", moved, err)
	}
	// The core's locator must find it in the folder.
	path, ok := configbuilder.FindRuleSetFile(dir, "geoip-cn")
	if !ok || filepath.Clean(path) != filepath.Clean(moved) {
		t.Errorf("FindRuleSetFile = %q,%v; want %s", path, ok, moved)
	}
	// Migration is idempotent: a second pass moves nothing and errors nothing.
	migrateRuleSetFolders(dir, map[string]string{})
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("second migration clobbered the folder file: %v", err)
	}
}
