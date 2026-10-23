package main

import (
	"eagle/internal/logstore"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// notifySecondInstance reports that another launch was intercepted, so the user
// knows why the window came back instead of a second copy starting.
//
// Through the notice queue rather than straight at the webview, and that is the whole
// of the fix here: this runs from the single-instance processor, which Wails starts
// before OnStartup, so a second launch inside the first second or two of this one —
// exactly the interval a double-click on the desktop icon produces — arrived while
// the context was still nil, and the log line then was dropped on the floor: the
// window came up, or did not, and nothing said why. notifyMessage now carries the
// words into the console's stream, where the "core" log file is written at the
// moment the line is raised, listener or no listener.
func (a *App) notifySecondInstance() {
	a.emitLog("core", logstore.Stamp(">>> 检测到重复启动，已切换到正在运行的实例"))
	a.notifyMessage("检测到重复启动，已切换到正在运行的实例")
}

// ShowWindow shows and focuses the main window.
func (a *App) ShowWindow() {
	if a.curCtx() == nil {
		return
	}
	runtime.WindowShow(a.curCtx())
	runtime.WindowUnminimise(a.curCtx())
}

// HideWindow hides the main window.
func (a *App) HideWindow() {
	if a.curCtx() != nil {
		runtime.WindowHide(a.curCtx())
	}
}

// QuitApp exits the application.
func (a *App) QuitApp() {
	a.shutdown()
	if a.curCtx() != nil {
		runtime.Quit(a.curCtx())
	}
}

// toggleSystemProxy is what the fourth hotkey does. nekoray's own is "show the system
// proxy menu"; this app has no such menu — the switch sits on the rail and the mode
// sits in the status bar — so the hotkey takes the action that menu was mostly used
// for, turning the system proxy on or off.
func (a *App) toggleSystemProxy() {
	if a.curSetting() == nil {
		return
	}
	// Held across the read and the switch, for the same reason toggleNode holds
	// it: the hotkey layer dispatches every press on its own goroutine, and two
	// presses a moment apart both read the flag as it stood before either ran —
	// both decided "turn on", queued two SetSystemProxyMode(true) behind each
	// other, and the double press meant on-then-on instead of on-then-off.
	// Same order as the other holders (hotkeyMu → startStopMu, see setEgress).
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	if err := a.SetSystemProxyMode(!a.curSetting().SPModeSystemProxy()); err != nil {
		a.notifyMessage("系统代理切换失败: "+err.Error(), true)
		// The press may well have come from the tray with the window hidden; a
		// failure nobody can see is a failure that looks like nothing happened.
		a.ShowWindow()
	}
}

// toggleVPN is the tray's other switch: the same action the rail's shield button
// performs. What that means — clearing the system proxy, restarting the node under the
// new inbound, asking for the administrator token — belongs to SetVPNMode, and is not
// repeated here.
func (a *App) toggleVPN() {
	if a.curSetting() == nil {
		return
	}
	// Same double-press guard as toggleSystemProxy above (see hotkeyMu).
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	if err := a.SetVPNMode(!a.curSetting().VPNMode()); err != nil {
		a.notifyMessage("TUN 切换失败: "+err.Error(), true)
		a.ShowWindow()
	}
}

// nextProfile starts the next node of the current group, stopping whatever is running
// first — the tray's "not this one". The hotkeys already cycle groups and routing
// profiles this way; this is the third of that family, and the one readers reach for
// most often without wanting the window.
func (a *App) nextProfile() {
	if a.curSetting() == nil || a.curMgr() == nil {
		return
	}
	// Serialised with the other node keys: "next" is decided from which node is
	// running now, and two presses at once decided from the same stale answer (see
	// hotkeyMu).
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	ps := a.curMgr().ProfilesWithOrder(a.curSetting().CurrentGroup)
	if len(ps) == 0 {
		a.notifyMessage("当前分组没有节点", true)
		a.ShowWindow()
		return
	}
	cur := a.RunningProfile()
	idx := -1
	for i, p := range ps {
		if p.ID == cur {
			idx = i
			break
		}
	}
	next := ps[(idx+1)%len(ps)]
	// No stop first, for the same reason switchTo no longer does one: StartProfile moves
	// in place when the node is already in the running config, and stops the old core only
	// when it genuinely has to (see the comment there). A stop here is what made every
	// press of this menu item rebuild the tun adapter.
	if err := a.StartProfile(next.ID); err != nil {
		a.notifyMessage("切换节点失败: "+err.Error(), true)
		a.ShowWindow()
		return
	}
	a.notifyMessage("已切换到 " + next.DisplayName())
}
