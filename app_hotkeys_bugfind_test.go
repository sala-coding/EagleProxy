package main

// The egress hotkey cycles through a fourth mode — "block" — that the rest of the
// application does not have.
//
// The status bar's own list is three modes: 规则 / 直连 / 代理 (EGRESS_MODES in the
// frontend, lib/layout.ts). The hotkey's cycle is four: rule, direct, global, block.
//
// "block" is a retired mode. The config builder reads it as "rule"
// (internal/configbuilder/builder.go), the tray reads it as "rule"
// (app_state.go, refreshTrayStatus), and the status bar reads it as "rule"
// (App.vue, refreshEgress). So pressing the hotkey once too many writes
// egress_mode="block" into the settings file, says "出站模式已设为阻断" in a toast,
// and changes nothing at all: the bar still reads 规则, the tray still reads 规则, and
// the core is still running the rule mode.
//
// The comment on cycleEgress says it steps "in the order the status bar's list shows
// them" — three modes. The code cycles four.
//
// The fix is to cycle the three the window actually has, in the order it shows them.

import (
	"testing"

	"eagle/internal/setting"
)

// newTestApp builds an App with just enough around it for the egress switch to run:
// the settings (the mode lives there) and a data directory to save them into. No
// manager and no controller, so nothing below the mode write is reached.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a := &App{dataDir: dir}
	a.setSetting(setting.Load(dir))
	return a
}

// TestCycleEgressStaysInTheModesTheWindowShows walks the whole cycle and checks that
// every mode it lands on is one the status bar can show.
func TestCycleEgressStaysInTheModesTheWindowShows(t *testing.T) {
	// The modes the window has, in the order it shows them.
	want := []string{"rule", "direct", "global"}

	a := newTestApp(t)
	// Start from each mode in turn and walk the cycle all the way round.
	for _, start := range want {
		a.setSetting(a.curSetting().WithEgressMode(start))
		seen := []string{start}
		for i := 0; i < len(want); i++ {
			a.cycleEgress()
			got := a.curSetting().EgressMode
			seen = append(seen, got)
			if got == "block" {
				t.Fatalf("starting from %q, the hotkey landed on \"block\" after %d press(es): "+
					"that mode was retired (the config builder, the tray and the status bar all "+
					"read it as \"rule\"), so the press says 阻断 while nothing changes "+
					"(app_hotkeys.go, cycleEgress)", start, i+1)
			}
		}
		// The cycle must come back to where it started.
		if last := seen[len(seen)-1]; last != start {
			t.Errorf("starting from %q, the cycle came back to %q, not to where it started", start, last)
		}
	}
}

// TestCycleEgressSkipsARetiredModeInTheFile is the narrower claim: whatever the
// hotkey writes into the settings file must be a mode the window can show.
func TestCycleEgressSkipsARetiredModeInTheFile(t *testing.T) {
	shown := map[string]bool{"rule": true, "direct": true, "global": true}
	a := newTestApp(t)
	for i := 0; i < 8; i++ {
		a.cycleEgress()
		mode := a.curSetting().EgressMode
		if !shown[mode] {
			t.Fatalf("the hotkey wrote the mode %q into the settings file, which is not one of %v",
				mode, []string{"rule", "direct", "global"})
		}
	}
}
