package main

import (
	"testing"

	"eagle/internal/setting"
)

func TestSettingsChangeRequiresCoreRestartWhenConsoleVisibilityChanges(t *testing.T) {
	before := &setting.Setting{}
	after := before.Clone()
	after.VPNHideConsole = !before.VPNHideConsole
	if !settingsNeedCoreRestart(before, after) {
		t.Fatal("changing core console visibility should require a restart")
	}
}

func TestSettingsChangeDoesNotRequireCoreRestartForTheme(t *testing.T) {
	before := &setting.Setting{}
	after := before.Clone()
	after.Theme = "light"
	if settingsNeedCoreRestart(before, after) {
		t.Fatal("changing theme should not require a core restart")
	}
}
