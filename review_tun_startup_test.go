package main

import (
	"path/filepath"
	"testing"

	"eagle/internal/bean"
	"eagle/internal/core"
	"eagle/internal/db"
	"eagle/internal/setting"
)

// TUN at start-up: the mode the last session was left in.
//
//	cd e:\project\eagle-desk; go test -run TestReview ./

// TestReviewRememberedTUNModeIsInForceWhenTheNodeStarts covers
// applyRememberedSpmode, the start-up restore of the mode the last session was
// left in.
//
// The code restores the TUN mode like this:
//
//	a.StartProfile(id)     // the node starts here
//	...
//	if a.curSetting().VPNMode() {   // never true: the flag is runtime-only
//	    a.notifyMessage("TUN 模式已恢复")
//	    return
//	}
//	a.SetVPNMode(true)    // so this runs: it stops the node and starts it again
//
// The comment on the branch says the opposite — "The node above came up straight
// into its tun config (the flag was remembered on), so there is nothing left to
// switch" — and app.go's applyPendingTun does exactly that, setting the flag
// BEFORE the node starts ("The flag goes down before the node comes up, so the
// profile starts straight into its tun config — one core start, no live switch").
//
// The symptom: with remember_spmode on and the remembered mode "vpn", every launch
// starts the node twice — once in the plain mixed-inbound config, then again
// under TUN — and the first (useless) core start is on the reader's path.
func TestReviewRememberedTUNModeIsInForceWhenTheNodeStarts(t *testing.T) {
	dir := t.TempDir()

	mgr := db.NewManager(dir)
	if err := mgr.Load(); err != nil {
		t.Fatalf("manager load: %v", err)
	}
	ent := &db.ProxyEntity{Type: "vmess", GID: 0, ID: -1}
	ent.Bean = bean.New()
	ent.Bean.Set("addr", "example.com")
	ent.Bean.Set("port", 443)
	ent.Bean.Set("id", "00000000-0000-0000-0000-000000000000")
	if err := mgr.AddProfile(ent, 0); err != nil {
		t.Fatalf("add profile: %v", err)
	}
	id := ent.ID

	s := setting.Load(dir)
	s.RememberSpmodeOn = true
	s.RememberSpmode = []string{"vpn"}
	s.RememberID = id

	a := &App{dataDir: dir}
	// The TUN front-end is an ELEVATED process, and starting it asks the reader
	// for administrator rights — which a test run must never do. A core path
	// that does not exist makes the front-end give up before it reaches that
	// point (ensureTunFront resolves the core first), so the node is still
	// started and the flag is still read at the moment the core is asked for.
	// What this test is about is the FLAG, not the front-end.
	a.setCorePath(filepath.Join(dir, "no-core-here.exe"), filepath.Join(dir, "no-core-here.exe"))
	a.setSetting(s)
	a.setMgr(mgr)
	ctrl := core.New(mgr, s, dir)
	a.setController(ctrl)

	// What the TUN flag read, at the moment the core is asked to start.
	started := false
	flagAtStart := false
	ctrl.OnLog = func(line string) {
		if started {
			return
		}
		started = true
		flagAtStart = a.curSetting().VPNMode()
	}

	a.applyRememberedSpmode()

	if !started {
		t.Fatalf("the node was never started — the reproduction did not reach the core start")
	}
	if !flagAtStart {
		t.Fatalf("the node was started with the TUN flag OFF: it came up in the plain " +
			"mixed-inbound config and applyRememberedSpmode then has to stop it and start it " +
			"again through SetVPNMode(true). The remembered TUN mode must already be in force " +
			"when the node starts, exactly as app.go's applyPendingTun does it " +
			"(\"the flag goes down before the node comes up ... one core start, no live switch\")")
	}
}
