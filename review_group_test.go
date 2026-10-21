package main

import (
	"fmt"
	"sync"
	"testing"

	"eagle/internal/db"
	"eagle/internal/setting"
)

// The group the reader is on: the group hotkey, the group the profile card
// switches to, and the file behind both.
//
// The settings object is replaced by other paths (the mode switches, the settings
// save), so a write into the live object is an update nobody sees: the group the
// reader picked was lost, and the next launch came back on the old tab.
//
//	cd e:\project\eagle-desk; go test -run TestReview ./

// reviewGroupFixture builds an App with a data directory, a settings file and
// three groups, so the group hotkey has something to cycle through.
func reviewGroupFixture(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	s := setting.Load(dir)
	if s == nil {
		t.Fatal("setting.Load returned nil")
	}
	mgr := db.NewManager(dir)
	if err := mgr.Load(); err != nil {
		t.Fatalf("manager.Load: %v", err)
	}
	for i := 0; i < 3; i++ {
		// -1 is "no id yet": the manager assigns one.
		if err := mgr.AddGroup(&db.Group{ID: -1, Name: fmt.Sprintf("group-%d", i)}); err != nil {
			t.Fatalf("AddGroup: %v", err)
		}
	}
	a := &App{dataDir: dir}
	a.setSetting(s)
	a.setMgr(mgr)
	return a
}

// TestReviewSetCurrentGroupLandsInTheLiveSetting covers app_profile.go's
// SetCurrentGroup.
//
// The group the reader picked is written to the settings, and the settings object
// is replaced by other paths (the mode switches, the settings save). A write into
// the live object is therefore an update nobody sees: the group the reader picked
// was lost, and the next launch came back on the old tab.
//
// The switch has to land in the object the app runs on — and in the file, so that
// the next launch comes back on the tab the reader left.
func TestReviewSetCurrentGroupLandsInTheLiveSetting(t *testing.T) {
	dir := t.TempDir()
	mgr := db.NewManager(dir)
	if err := mgr.Load(); err != nil {
		t.Fatalf("manager load: %v", err)
	}
	s := setting.Load(dir)
	// Two groups: the first takes the id the settings already point at, and the
	// switch below has to be to a different one (SetCurrentGroup leaves the
	// current group alone).
	first := &db.Group{ID: -1, Name: "first"}
	if err := mgr.AddGroup(first); err != nil {
		t.Fatalf("add group: %v", err)
	}
	g := &db.Group{ID: -1, Name: "second"}
	if err := mgr.AddGroup(g); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if g.ID == s.CurrentGroup {
		t.Fatalf("the group to switch to has the id the settings already carry (%d)", g.ID)
	}

	a := &App{dataDir: dir}
	a.setSetting(s)
	a.setMgr(mgr)

	before := a.curSetting()
	if err := a.SetCurrentGroup(g.ID); err != nil {
		t.Fatalf("App.SetCurrentGroup: %v", err)
	}

	after := a.curSetting()
	if after.CurrentGroup != g.ID {
		t.Errorf("the live setting still points at group %d, want %d: the switch was written "+
			"into an object another path had already replaced", after.CurrentGroup, g.ID)
	}
	if before == after {
		t.Errorf("the live setting was written into in place instead of being replaced")
	}
	if before.CurrentGroup == g.ID {
		t.Errorf("the previous setting object was written into: it must stay frozen for the " +
			"goroutines still reading it")
	}

	// And it must be there the next time the app reads the file.
	reloaded := setting.Load(dir)
	if reloaded.CurrentGroup != g.ID {
		t.Errorf("the file does not carry the group: current_group=%d, want %d",
			reloaded.CurrentGroup, g.ID)
	}
}

// TestReviewNextGroupDoesNotWriteIntoTheLiveSetting is a race test.
//
// The group hotkey wrote the new group into the live setting object. Every other
// path replaces that object instead, and a write into an object another goroutine
// has already replaced is an update nobody sees: the group the reader asked for
// was lost, and the next launch came back on the old tab.
//
// It was a data race as well: the settings save copies the same object, and the
// copy read the field the hotkey was writing.
func TestReviewNextGroupDoesNotWriteIntoTheLiveSetting(t *testing.T) {
	a := reviewGroupFixture(t)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			a.nextGroup()
		}()
		go func() {
			defer wg.Done()
			live := a.curSetting()
			if live == nil {
				return
			}
			// What the settings save does: take a copy of the live object.
			cp := live.Clone()
			if _, err := live.Replace(cp); err != nil {
				t.Errorf("Replace: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestReviewNextGroupLandsOnTheNextGroup is the plain claim the hotkey makes:
// the group the app runs on is the one after the current one, and it is the live
// setting that carries it — not only the file.
func TestReviewNextGroupLandsOnTheNextGroup(t *testing.T) {
	a := reviewGroupFixture(t)
	order := a.curMgr().TabOrder()
	if len(order) < 2 {
		t.Fatalf("the fixture has %d groups, want at least 2", len(order))
	}
	start := a.curSetting().CurrentGroup
	if !a.curMgr().HasGroup(start) {
		// Put the app on a group it has, so the rotation is well defined.
		if err := a.SetCurrentGroup(order[0]); err != nil {
			t.Fatalf("SetCurrentGroup: %v", err)
		}
		start = order[0]
	}
	idx := -1
	for i, g := range order {
		if g == start {
			idx = i
		}
	}
	want := order[(idx+1+len(order))%len(order)]

	a.nextGroup()

	if got := a.curSetting().CurrentGroup; got != want {
		t.Errorf("CurrentGroup = %d, want %d", got, want)
	}
	// And it was persisted: the next launch comes back on the same tab.
	reloaded := setting.Load(a.curDataDir())
	if reloaded == nil {
		t.Fatal("setting.Load returned nil")
	}
	if reloaded.CurrentGroup != want {
		t.Errorf("the group was not persisted: on disk CurrentGroup=%d, want %d", reloaded.CurrentGroup, want)
	}
}
