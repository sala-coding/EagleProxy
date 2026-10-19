package update

import "testing"

// The version comparison is what decides whether a release is an update, so
// the cases that have bitten before are the ones written down: the same
// version (the reader is already running it), the numbers that do not sort as
// text (1.10.0 is newer than 1.9.0), a pre-release against the release it
// leads to, and the strings that are not versions at all.
func TestCompare(t *testing.T) {
	cases := []struct {
		a, b       string
		want       int
		comparable bool
	}{
		{"v1.0.1", "v1.0.1", 0, true},
		{"v1.0.1", "v1.0.2", -1, true},
		{"v1.0.2", "v1.0.1", 1, true},
		{"1.0.1", "v1.0.1", 0, true},
		{"v1.0", "v1.0.0", 0, true},
		// Numbers, not text: "1.10.0" sorts before "1.9.0" as a string.
		{"v1.10.0", "v1.9.0", 1, true},
		{"v1.9.0", "v1.10.0", -1, true},
		// A pre-release is older than the release it leads to.
		{"v1.0.1", "v1.0.1-beta.1", 1, true},
		{"v1.0.1-beta.1", "v1.0.1", -1, true},
		{"v1.0.1-beta.2", "v1.0.1-beta.1", 1, true},
		// Build metadata is not part of the version.
		{"v1.2.3+build7", "v1.2.3", 0, true},
		// A working copy carries a number below every release, so it compares:
		// it is older than any release, and older than the 0.0.0 release too.
		{"0.0.0-dev+9988017", "v1.0.2", -1, true},
		{"0.0.0-dev+9988017", "v0.0.0", -1, true},
		{"0.0.0-dev+9988017", "v0.0.0-dev+9988017", 0, true},
		{"0.0.0-dev+9988017", "v0.0.0-dev+9988018", 0, true},
		// Not versions: the caller has to fall back on something else.
		{"dev-9988017", "v1.0.1", 0, false},
		{"", "v1.0.1", 0, false},
		{"v1.0.1", "", 0, false},
		{"not-a-version", "v1.0.1", 0, false},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if ok != c.comparable {
			t.Errorf("Compare(%q, %q) comparable = %v, want %v", c.a, c.b, ok, c.comparable)
			continue
		}
		if ok && got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// The Windows asset is picked by name: the release may carry several
// platforms' builds, and the first .exe in the list is not necessarily this
// machine's. The names published before the platform was spelled out have to
// keep working.
func TestWindowsAsset(t *testing.T) {
	rel := &Release{
		TagName: "v1.0.2",
		Assets: []Asset{
			{Name: "Eagle-v1.0.2-macos.zip"},
			{Name: "Eagle-v1.0.2-linux-amd64"},
			{Name: "Eagle-v1.0.2-windows.exe"},
		},
	}
	asset, err := WindowsAsset(rel)
	if err != nil {
		t.Fatalf("WindowsAsset: %v", err)
	}
	if asset.Name != "Eagle-v1.0.2-windows.exe" {
		t.Errorf("WindowsAsset picked %q", asset.Name)
	}
	// The name without the platform is still a Windows build.
	old := &Release{TagName: "v1.0.1", Assets: []Asset{{Name: "Eagle-v1.0.1.exe"}}}
	asset, err = WindowsAsset(old)
	if err != nil {
		t.Fatalf("WindowsAsset (old name): %v", err)
	}
	if asset.Name != "Eagle-v1.0.1.exe" {
		t.Errorf("WindowsAsset picked %q", asset.Name)
	}
	// Nothing Windows in it: that is an error, not a silent pick.
	none := &Release{TagName: "v1.0.3", Assets: []Asset{{Name: "Eagle-v1.0.3-linux-amd64"}}}
	if _, err := WindowsAsset(none); err == nil {
		t.Error("WindowsAsset accepted a release with no Windows build")
	}
}
