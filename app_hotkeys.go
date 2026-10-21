package main

import (
	"eagle/internal/core"
	"eagle/internal/hotkey"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) applyHotkeys() {
	if a.curSetting() == nil {
		return
	}
	b := map[string]func(){}
	// The map is keyed by the combination, so two actions asking for the same one means
	// the later silently takes the earlier's place: the reader set a key, pressed it, and
	// a different action answered — or nothing did, when the one that lost was the one
	// they meant. The binding table cannot hold both, but it can say so: the loser is
	// named in the log and in a toast, and the reader decides which action keeps the key.
	//
	// Windows-refused combinations are reported separately and by the hotkey package
	// (see Apply's refused list): that is the shell or another program holding the
	// combination, which is a different sentence.
	owner := map[string]string{}
	var clashes []string
	bind := func(combo, what string, fn func()) {
		if combo == "" {
			return
		}
		if prev, taken := owner[combo]; taken {
			clashes = append(clashes, fmt.Sprintf("%s 与 %s（%s）", prev, what, combo))
			a.logf("[hotkey] " + prev + " 与 " + what + " 使用了同一组合 " + combo + "，只有后者生效")
		}
		owner[combo] = what
		b[combo] = fn
	}
	s := a.curSetting()
	bind(s.HotkeyMainwindow, "打开主窗口", a.ShowWindow)
	bind(s.HotkeyGroup, "切换分组", a.nextGroup)
	bind(s.HotkeyRoute, "切换路由", a.nextRouting)
	bind(s.HotkeySystemProxyMenu, "切换系统代理", a.toggleSystemProxy)
	bind(s.HotkeyToggleNode, "启停节点", a.toggleNode)
	bind(s.HotkeyEgress, "切换出站模式", a.cycleEgress)
	bind(s.HotkeyToggleTun, "切换 TUN", a.toggleVPN)
	bind(s.HotkeyNextNode, "下一个节点", a.nextProfile)
	// RestartProfile reports why it could not — nothing running, no mode in place —
	// and shows the window with the message, so the key itself has nothing to add.
	bind(s.HotkeyRestartNode, "重启节点", func() { _ = a.RestartProfile() })
	bind(s.HotkeyConnections, "连接列表", func() { a.emitHotkeyToWindow("connections") })
	bind(s.HotkeyLogs, "日志面板", func() { a.emitHotkeyToWindow("logs") })
	bind(s.HotkeyDataDir, "打开数据目录", func() { _ = a.OpenDataDir() })
	if len(clashes) > 0 {
		a.notifyMessage("这些热键被两个动作共用，只有后者生效："+strings.Join(clashes, "、"), true)
	}
	if refused := hotkey.Apply(b); len(refused) > 0 {
		// Said rather than swallowed. A combination Windows would not take is
		// indistinguishable from one that was never set — the reader's own words for it
		// are "the key is not there" — and it is not a mistake on their side: the shell
		// owns some of these (Alt+Tab), and another program may hold the rest.
		a.logf("[hotkey] 未注册: " + strings.Join(refused, ", "))
		a.notifyMessage("这些热键没能注册："+strings.Join(refused, "、")+"。组合可能已被系统或其它程序占用。", true)
	}
}

// SuspendHotkeys unregisters every global hotkey, and ResumeHotkeys puts them back —
// they are what the settings sheet calls while a combination is being captured.
//
// Without the pause the one combination that cannot be pressed is the one being set:
// Windows delivers a registered combination to this process as a hotkey and passes
// nothing on, so the capture field never sees the press. Re-entering the key a row
// already shows — the first thing anyone tries — would look like a field that does not
// work, and so would a combination another row already holds.
func (a *App) SuspendHotkeys() { hotkey.Stop() }

// TestHotkey reports whether Windows would accept a combination. The settings sheet asks
// before it records one: a combination another program already holds is otherwise a key
// that quietly does nothing, which is what the reader would find out days later.
func (a *App) TestHotkey(combo string) bool { return hotkey.Test(combo) }

// ResumeHotkeys re-registers whatever the settings now hold. Nothing to save first: the
// sheet writes the settings when it is saved, and this only re-reads them.
func (a *App) ResumeHotkeys() {
	if a.curSetting() == nil {
		return
	}
	a.applyHotkeys()
}

// emitHotkeyToWindow hands a press that belongs to the window rather than to the backend
// to the frontend, which is the side that knows whether a panel is open. The window is
// brought up first: these panels are pieces of it, and a press that opened one behind a
// hidden window would look like a press that did nothing at all.
func (a *App) emitHotkeyToWindow(what string) {
	a.ShowWindow()
	if a.curCtx() != nil {
		runtime.EventsEmit(a.curCtx(), "hotkey:"+what)
	}
}

// toggleNode is the hotkey that starts a node or stops the running one: the tray's two
// items behind a single key, because a reader who wants a node up does not also want to
// remember which of the two presses that is.
func (a *App) toggleNode() {
	if a.curSetting() == nil {
		return
	}
	// Held for the whole press: the state this decides on is read below and written
	// by the work that follows, and a second press arriving in between reads the
	// state of a press that has not finished (see hotkeyMu).
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	stop := func() {
		if err := a.StopProfile(); err != nil {
			a.notifyMessage("停止节点失败: "+err.Error(), true)
		}
	}
	if a.RunningProfile() != core.StoppedID {
		stop()
		return
	}
	// The front-end's pid is deliberately not consulted here: it holds an adapter, not a
	// node, and a TUN setup with no node running is "nothing running" as far as this
	// switch is concerned — the press below starts one.
	if err := a.StartLastProfile(); err != nil {
		a.notifyMessage("启用节点失败: "+err.Error(), true)
		a.ShowWindow()
	}
}

// cycleEgress steps the outbound mode on to the next one, in the order the status bar's
// list shows them, and puts it in force at once.
//
// The two things the mode decides — route.final, and whether the profile's rules are used
// at all — are written when the core is started, and sing-box will not read its config a
// second time. So a running node is restarted here, which is the same bargain the status
// bar's own switch makes: a mode that takes effect only after the reader restarts the
// node by hand is a mode that looks broken.
func (a *App) cycleEgress() {
	if a.curSetting() == nil {
		return
	}
	// The three modes the status bar's list shows, in the order it shows them. It
	// used to carry a fourth, "block" — but that mode was retired: the config
	// builder reads it as "rule", and so do the tray and the status bar. A press
	// that landed on it therefore wrote "block" into the settings file, said
	// 阻断 in a toast, and changed nothing at all: the bar still read 规则 and the
	// core was still running the rule mode. The cycle walks the modes the window
	// actually has.
	modes := []string{"rule", "direct", "global"}
	cur := a.curSetting().EgressMode
	if cur == "" {
		cur = "rule"
	}
	next := modes[0]
	for i, m := range modes {
		if m == cur {
			next = modes[(i+1)%len(modes)]
			break
		}
	}
	a.setEgress(next)
}

// setEgress puts one named outbound mode in force — the shared body of the hotkey
// cycle and the tray menu's three answers. The save is what the settings file holds,
// the event is what the status bar reads to light its word, and the restart is what
// makes either true: a mode takes effect when its core is rebuilt, not when the name
// is written.
func (a *App) setEgress(mode string) {
	if a.curSetting() == nil {
		return
	}
	// The same lock as the node keys, and the only place it is taken for this path:
	// the hotkey cycle computes the next mode and calls in here, the tray calls in
	// here directly, and two of them at once restarted the core twice — each
	// restart stopping what the other had just started.
	a.hotkeyMu.Lock()
	defer a.hotkeyMu.Unlock()
	// Re-read under the lock, and replace the struct rather than a field of it: a node
	// start in another goroutine swaps this pointer (rememberStarted), and a write into
	// the copy it left behind loses the change — the tray said 直连 and the bar showed
	// 规则 again a moment later, with nothing in the log. applySetting is what makes the
	// save and the swap one step.
	cur := a.curSetting()
	if cur == nil {
		return
	}
	if err := a.applySetting(cur.WithEgressMode(mode)); err != nil {
		a.notifyMessage("保存出站模式失败: "+err.Error(), true)
		return
	}
	// The bar reads the mode out of the settings file (see refreshEgress), so this event
	// is what puts the new word on it; the restart below is what makes the word true.
	if a.curCtx() != nil {
		runtime.EventsEmit(a.curCtx(), "egress:changed", mode)
	}
	if a.RunningProfile() == core.StoppedID {
		// The tray's three radio items read this same value; with no node to restart,
		// nothing else below would refresh them, and the hotkey left the menu on the
		// previous answer while the status bar showed the new one.
		a.refreshTrayStatus()
		a.notifyMessage("出站模式已设为" + egressName(mode))
		return
	}
	if err := a.RestartProfile(); err != nil {
		a.notifyMessage("切换出站模式失败: "+err.Error(), true)
		return
	}
	a.notifyMessage("出站模式已设为" + egressName(mode))
}

// egressName is a mode as the reader knows it. The strings in the settings file are a
// contract with the config builder; these are the four words the window uses for the same
// four, and a message should not confuse the two.
func egressName(mode string) string {
	switch mode {
	case "direct":
		return "直连"
	case "global":
		return "代理"
	case "block":
		return "阻断"
	default:
		return "规则"
	}
}

func (a *App) nextGroup() {
	// Hotkeys fire against the machine, not the webview: a failed reload leaves
	// the stores nil, and the key would panic the process mid-reload.
	if a.curMgr() == nil || a.curSetting() == nil {
		return
	}
	order := a.curMgr().TabOrder()
	if len(order) == 0 {
		return
	}
	// -1, not 0: the current group can name nothing — a group deleted while the
	// window was not the one doing it — and a 0 here made that state start the
	// rotation at order[1], one past a group the reader never saw. nextRouting
	// below runs the same arithmetic for the same reason.
	idx := -1
	for i, g := range order {
		if g == a.curSetting().CurrentGroup {
			idx = i
			break
		}
	}
	next := order[(idx+1+len(order))%len(order)]
	// Through SetCurrentGroup, not a write into the live object. The setting is
	// replaced by the other paths (the settings save, the mode switches), and a
	// write into the object another goroutine has already replaced is an update
	// nobody sees: the group the hotkey asked for was lost, and the next launch
	// came back on the old tab. The write was a data race as well — the same
	// field the table and the config builder read without a lock.
	if err := a.SetCurrentGroup(next); err != nil {
		a.logf("[hotkey] 切换分组失败: " + err.Error())
		return
	}
	if a.curCtx() != nil {
		runtime.EventsEmit(a.curCtx(), "groups:changed", next)
	}
}

func (a *App) nextRouting() {
	if a.curSetting() == nil {
		return
	}
	names := a.curSetting().ListRoutings()
	if len(names) == 0 {
		return
	}
	idx := -1
	for i, n := range names {
		if n == a.curSetting().ActiveRouting {
			idx = i
			break
		}
	}
	next := names[(idx+1+len(names))%len(names)]
	// Switching means the ACTIVE profile moves. This used to call ApplyRouting,
	// which saved the target profile back to itself and rebuilt the core while
	// ActiveRouting — and with it the profile every config build reads — stayed
	// on the old one: the press announced a switch that had not happened, and
	// charged a core restart for it.
	if err := a.SetActiveRouting(next); err != nil {
		a.notifyMessage("切换路由失败: " + err.Error())
		return
	}
	// The rules live in the core's config and sing-box reads it once, so a
	// running node is rebuilt on the profile that is now active. With nothing
	// running the file is the whole change (the next start reads it) — and the
	// routing:changed event has already gone out from SetActiveRouting.
	if running := a.RunningProfile(); running != core.StoppedID {
		if err := a.rebuildCoreForRouting(running); err != nil {
			a.notifyMessage("切换路由失败: " + err.Error())
		}
	}
}
