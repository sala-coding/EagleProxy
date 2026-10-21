package main

import (
	"testing"

	"eagle/internal/setting"
)

// What is in this file is the moment a setting is replaced.
//
// The app replaces the live setting object rather than writing into it: the
// goroutines that already hold the old value (the traffic loop, the speed tests,
// the hotkeys) are meant to keep reading a consistent snapshot. Two things broke
// that arrangement, and both are here.
//
//	cd e:\project\eagle-desk; go test -run TestReview ./

// reviewSettingSwapFixture builds an App whose settings live in a throwaway data
// directory.
func reviewSettingSwapFixture(t *testing.T) (*App, *setting.Setting) {
	t.Helper()
	dir := t.TempDir()
	s := setting.Load(dir)
	if s == nil {
		t.Fatal("setting.Load returned nil")
	}
	a := &App{}
	a.setDataDir(dir, dir)
	a.setSetting(s)
	return a, s
}

// TestReviewUpdateSettingsDoesNotMutateThePreviousSetting pins the other half
// of the same contract: App.applySetting's own comment says the pointer is
// swapped rather than mutated in place, so goroutines holding the old value keep
// reading a consistent snapshot.
//
// Setting.Replace does `next := *s` and then json.Unmarshal(data, &next).
// ExtraCore and InboundAuth are POINTER fields, so next shares the objects with
// the previous setting — and Unmarshal decodes into the existing pointed-to
// structs. The previous, supposedly frozen snapshot is therefore rewritten under
// the feet of every goroutine still reading it (the controller's own setting,
// app_dns.go's inboundProxyURL, App.TestDNS).
func TestReviewUpdateSettingsDoesNotMutateThePreviousSetting(t *testing.T) {
	a, _ := reviewSettingSwapFixture(t)

	prev := a.curSetting()
	prevAuth := prev.InboundAuth
	if prevAuth == nil {
		t.Fatal("the setting has no InboundAuth")
	}
	before := prevAuth.Username

	in := setting.Setting{
		InboundAuth: &setting.InboundAuth{Username: "review-user", Password: "review-pass"},
	}
	if err := a.UpdateSettings(in); err != nil {
		t.Fatalf("App.UpdateSettings: %v", err)
	}

	// The live setting has the new credentials — that part is right.
	if got := a.curSetting().InboundAuth.Username; got != "review-user" {
		t.Fatalf("the live setting was not updated: InboundAuth.Username=%q", got)
	}
	// The PREVIOUS snapshot must not have been rewritten.
	if prevAuth.Username != before {
		t.Errorf("the previous Setting was mutated in place: its InboundAuth.Username went from %q to %q",
			before, prevAuth.Username)
	}
}

// TestReviewApplySettingKeepsTheRuntimeModeFlags is the TUN flag that the
// machine and the app disagreed about.
//
// The two runtime mode flags live in the setting object, and applySetting
// replaces that object. The copy it is handed was taken a moment earlier, so a
// tray or hotkey toggle that landed in between wrote its flag on the object that
// was live at that moment — and the object that went live afterwards was the
// copy, which had the flag off.
//
// The result was the worst kind of disagreement: the machine was in TUN mode,
// the app believed it was not, and the exit path, the DNS takeover and the tray
// all read that belief.
func TestReviewApplySettingKeepsTheRuntimeModeFlags(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	a.setSetting(setting.Load(a.curDataDir()))

	// The copy the settings save hands over, taken while the flags are off.
	stale := a.curSetting().Clone()

	// The tray (or the hotkey) turns TUN on, on the object that is live now.
	a.curSetting().SetVPNMode(true)

	if err := a.applySetting(stale); err != nil {
		t.Fatalf("applySetting: %v", err)
	}

	if !a.curSetting().VPNMode() {
		t.Error("the TUN flag was dropped by the settings swap: the machine is in TUN mode and the app says it is not")
	}
	if a.curSetting().SPModeSystemProxy() {
		t.Error("the system-proxy flag was turned on by the swap")
	}
}

// TestReviewRemoveIntLeavesTheCallersListAlone is the small one.
//
// The root package's removeInt wrote the answer over the caller's own slice, so
// the caller's list came back changed — a different answer from the one that was
// asked for. The copy in internal/db does not do that, and two functions with one
// name in one project should not answer differently.
func TestReviewRemoveIntLeavesTheCallersListAlone(t *testing.T) {
	original := []int{3, 1, 4, 1, 5}
	kept := original[:len(original):len(original)]

	got := removeInt(kept, 1)

	if len(original) != 5 || original[0] != 3 || original[1] != 1 || original[2] != 4 || original[3] != 1 || original[4] != 5 {
		t.Errorf("the caller's list was changed: %v", original)
	}
	if len(got) != 3 || got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Errorf("removeInt = %v, want [3 4 5]", got)
	}
}
