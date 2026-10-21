package main

import (
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/dnsstub"
	"eagle/internal/tray"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RunningProfile returns the running profile id (or -1919).
func (a *App) RunningProfile() int {
	// The controller answers for every TUN shape alike: in the two-process setup the
	// front-end holds an adapter and a pid, not a profile — asking it which node is up
	// would return an answer about a process that holds no node at all.
	if a.curController() == nil {
		return core.StoppedID
	}
	return a.curController().RunningID()
}

// RunningProfileName returns the display name of the running profile, so the
// status bar can show it even when the node lives in another group.
func (a *App) RunningProfileName() string {
	if id := a.RunningProfile(); id != core.StoppedID {
		if ent := a.curMgr().SnapshotProfile(id); ent != nil {
			return ent.Name()
		}
	}
	return ""
}

// emitRunningState pushes the running profile id to the frontend, so the UI
// stays in sync with starts and stops that bypass its IPC layer (tray menu,
// autostart, hotkeys).
func (a *App) emitRunningState() {
	a.refreshTrayStatus()
	if a.curCtx() != nil {
		runtime.EventsEmit(a.curCtx(), "running:changed", a.RunningProfile())
	}
	// A node that has just come up is a foreign side that has just become reachable:
	// one query over its own path now, so the first real lookup skips the CONNECT
	// through the mixed inbound and the handshake with the resolver behind the node.
	// The core answers on its inbound before this runs, which is the precondition the
	// warm query needs (see inboundProxyURL).
	if a.RunningProfile() != core.StoppedID {
		go crashlog.Guard("dnsWarmRemote", func() { a.warmDNSStub(dnsstub.Remote) })
	}
	// Every start and every stop ends here, whichever side made it — so this is the one
	// place the machine's own resolvers can follow the running node from, without a call
	// at each of them. On a goroutine of its own because the work is a file write and an
	// interface-table read, and this runs inside some of those paths' locks; the reconcile
	// reads live state and is idempotent, so a stray one costs nothing (see
	// app_dns_takeover.go).
	go crashlog.Guard("reconcileSystemDNS", func() { a.reconcileSystemDNS(false) })
}

// refreshTrayStatus keeps the tray menu's two switches in step with the modes they stand
// for. It used to drive a breathing icon as well — the mark pulsing while a node ran under
// system proxy or TUN — and that is gone: a mark that moves in the notification area is a
// mark the eye keeps being pulled back to, in the one corner of the screen the reader looks
// at for something else.
func (a *App) refreshTrayStatus() {
	if a.curSetting() == nil || a.curMgr() == nil {
		return
	}
	// The menu's switches — the two network shapes and the outbound mode between
	// them — say the same thing the rail's buttons do: every mode change ends in
	// this function, whichever side made it.
	mode := a.curSetting().EgressMode
	if mode == "block" {
		mode = "rule"
	}
	tray.SetModes(a.curSetting().SPModeSystemProxy(), a.curSetting().VPNMode(), mode)
	// The status line at the menu's head answers the other half of "what is going
	// on": the node that is running now, or the fact that nothing is. Every start
	// and stop flows through here as well (see emitRunningState), so the line and
	// the window never disagree about who is up.
	tray.SetCurrent(a.RunningProfileName())
}

// GetSPMode returns the runtime split modes. A failed reload leaves the live
// settings unreachable; the binding still gets asked (the webview has booted),
// so it answers the "everything off" reading rather than panicking.
func (a *App) GetSPMode() map[string]bool {
	if a.curSetting() == nil {
		return map[string]bool{"vpn": false, "systemProxy": false}
	}
	return map[string]bool{
		"vpn":         a.curSetting().VPNMode(),
		"systemProxy": a.curSetting().SPModeSystemProxy(),
	}
}
