package main

import (
	"eagle/internal/core"
	"eagle/internal/netx"
	"eagle/internal/sysproxy"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SetVPNMode toggles TUN mode. Turning it on clears the system proxy, because
// both hijack the machine's network path: with both on, traffic is captured
// twice and which one actually takes effect is undefined.
//
// The inbound type (mixed port vs tun) is baked into the config the running
// core was started with, so a live switch is applied by restarting the same
// profile under the new mode — no manual stop required.
func (a *App) SetVPNMode(on bool) error {
	// One switch at a time, and taken before anything else this function defers:
	// the lock releases last of all (it was registered first), so the flags, the
	// tray and the remembered mode all settle while it is still held — the next
	// press in the queue reads a settled machine, never one mid-switch.
	//
	// The start/stop seat comes first (startStopMu → modeMu, the only order any
	// caller runs): a switch's stop+start pair is one unit, and a binding or a
	// tray press reaching StartProfile directly must wait behind the whole
	// switch, not slip between its halves.
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	a.modeMu.Lock()
	defer a.modeMu.Unlock()
	// The animation tracks the flags, and this function has several exits (the
	// failed-elevation undo, the handover, the plain path), so one deferred
	// refresh after the flags have settled covers them all — and the same is true
	// of the mode this session will be remembered as.
	defer a.refreshTrayStatus()
	defer a.rememberSpmode()
	// 节点先行：a capture with nothing behind it is a machine whose traffic
	// enters a tunnel that leads nowhere. The reader picks and starts a node
	// first; this switch, pressed without one, is refused with the missing
	// step named.
	if on && a.RunningProfile() == core.StoppedID {
		msg := "请先选择并启动一个节点，再开启 TUN 模式"
		a.notifyMessage(msg, true)
		return fmt.Errorf("%s", msg)
	}
	if on {
		// And the node it is about to restart has to still be in the store. It can be
		// gone — a subscription update replaced it, the reader deleted it, its group
		// was removed — and this switch works by restarting the running node: without
		// this check the Windows proxy was cleared and the old core stopped before the
		// missing node was discovered, so the reader was left with a stopped node, no
		// proxy, and the words "profile N not found". Refused here, with the machine
		// exactly as it was found.
		if running := a.RunningProfile(); running != core.StoppedID &&
			(a.curMgr() == nil || a.curMgr().SnapshotProfile(running) == nil) {
			msg := "当前节点已不在列表中（可能已被订阅更新替换），请刷新后重新选择节点，再开启 TUN"
			a.notifyMessage(msg, true)
			return fmt.Errorf("%s", msg)
		}
	}
	// The routing table is about to move under anything that measures it: the tun
	// adapter's routes come and go with this switch, and both configs written from here
	// — the front-end's address list and the main core's address family — ask whether
	// IPv6 actually works on this network (see netx.HasIPv6). Without this, both would
	// be answered from the cache the previous mode left behind.
	netx.InvalidateIPv6()
	if on {
		// Switching TUN on cancels the countdown that would have taken the adapter
		// apart: whatever is left of the last one is about to be used again, and
		// that is the whole reason it was kept.
		a.cancelTunIdleReclaim()
	}
	// Hand the real Windows proxy back BEFORE the flag flips. StopProfile only
	// restores it while the flag still says "on", so clearing the flag first
	// would leave the machine pointing at the local inbound port that TUN is
	// about to take over — no network until the user fixed it by hand.
	// Remembered because the rollback further down has to put it back: if the UAC
	// prompt is declined the whole switch amounts to nothing, and the system
	// proxy it just tore down should not be collateral damage.
	hadSystemProxy := on && a.curSetting().SPModeSystemProxy()
	if hadSystemProxy {
		// Cleared whether or not a node is running. The flag goes off with it, and
		// every later path that reads the registry — this function's own rollback,
		// StopProfile, runShutdown — reads the flag first: a Windows proxy left
		// pointing at the local port while the flag says off is a leftover nothing
		// comes back for, which is an offline machine the moment the core behind it
		// is gone (crashed, restarted, never started). The write is idempotent:
		// clearing a registry that was already clean is nothing to do.
		if err := a.applySystemProxy(false); err != nil {
			a.logf("[system proxy] 切向 TUN 前的代理清理失败: " + err.Error())
		}
		a.curSetting().SetSPModeSystemProxy(false)
	}
	running := a.RunningProfile()
	// Flip the flag before touching the core. Switching TUN off used to stop
	// first and set the flag after, so any failure to kill the elevated core
	// (access denied, refused UAC) returned early and left the flag on: the
	// switch appeared to fail, and the pid was already dropped, so the running
	// core could no longer be stopped at all.
	a.curSetting().SetVPNMode(on)
	// Told to the elevated side here, now that the flag says the truth, rather
	// than two seconds into it by the poll. The reconcile used to run above the
	// flip, and a switch-on then wrote Adapter:false — the state it was leaving —
	// so the resident server spent the start-up window pointing the interfaces
	// away from the takeover it was about to be asked for, and the corrected
	// state waited for the next two-second tick. A resolver that answers during
	// that window was the machine's own DHCP one, and the first probes of the
	// new adapter went out against it and lost. (See app_dns_takeover.go for
	// what the state drives.)
	a.reconcileSystemDNS(false)
	if running == core.StoppedID {
		// The switch is off and nothing has ever run (the on case was refused
		// at the guard above). A warm-up may still have left routes behind from
		// a switch that never reached a node: the adapter stays — it costs
		// nothing idle and keeps the next switch-on instant — but its routes
		// must not, and with nothing behind it the countdown takes it apart.
		a.releaseTunRoutes()
		a.armTunIdleReclaim()
		return nil
	}
	if err := a.stopProfileLocked(); err != nil {
		// The stop failed, so the old core is still sitting on its inbound —
		// and which flag tells the truth depends on the way the switch was
		// headed. A switch that was coming on finds the plain core it was
		// leaving still serving (TUN never started): the flag stays off. A
		// switch that was going off finds the elevated core still capturing
		// (its stop was refused): the flag goes back on — the contract the
		// reverse direction keeps for the same failure, a flag saying off
		// while the machine is still taken over being the state no switch may
		// leave behind. The flags go back and Windows is re-pointed at the
		// proxy the surviving core serves (the on-path's hadSystemProxy
		// rollback; the off-path never touched it).
		a.curSetting().SetVPNMode(on)
		if hadSystemProxy {
			a.curSetting().SetSPModeSystemProxy(true)
			_ = a.applySystemProxy(true)
		}
		return err
	}
	if !on {
		// Turning TUN off ends the system-wide capture, but it does not have to
		// take the node with it: the same profile is restarted on the plain
		// mixed port, so the running lamp stays lit and whatever points at the
		// local inbound keeps working — and the system-proxy switch, if pressed
		// later, lands on a node that is already up instead of a stopped one.
		// Windows itself is not pointed anywhere: the proxy flag is off and is
		// left off, this is a direct machine with a live core behind the port.
		//
		// The front-end and its adapter come down at once rather than waiting
		// for the idle reclaim: with a node running, the countdown's
		// precondition (runningID == StoppedID) can never fire, and an adapter
		// that stays would be rights held for a capture nothing claims.
		a.releaseTunRoutes()
		if err2 := a.stopTunFront(); err2 != nil {
			a.logf("TUN 网卡随手拆除未完成: " + err2.Error())
		}
		if err := a.startProfileLocked(running); err != nil {
			// The TUN core is gone and the restart did not land: say so rather
			// than let the lamp lie. The reader re-picks a node as usual.
			a.notifyMessage("TUN 模式已关闭，但节点重启到本地端口失败: "+err.Error(), true)
			return err
		}
		a.notifyMessage("TUN 模式已关闭，节点已切回本地端口继续运行")
		return nil
	}
	// VPN start goes through RunElevated: if the user declines the UAC prompt
	// the error surfaces here and the node stays stopped, which is honest.
	// Announced here for the same reason as its counterpart in
	// SetSystemProxyMode: the frontend stays quiet when a mode is switched on, so
	// a switch that restarts the node has to say so from this side or the press
	// answers with nothing. applyPendingTun uses these same words for the
	// relaunched case, so both routes into "TUN is on" read alike.
	err := a.startProfileLocked(running)
	if err == nil {
		a.notifyMessage("TUN 模式已开启（已重启节点生效）")
		return nil
	}
	// The start failed after everything was already torn down — the Windows
	// proxy cleared, the old core stopped, the TUN start refused (a declined
	// UAC prompt, a missing wintun.dll…). This is the rollback the setting of
	// hadSystemProxy was kept for: TUN did not come up, so the flag stands
	// down, and the machine goes back to the system-proxy state it was in.
	// The restart runs on the plain path, where none of the TUN failure
	// causes apply; if even that fails, the flags stay consistent and the
	// reader is told, instead of the machine silently pointing at a port
	// nobody listens on.
	a.curSetting().SetVPNMode(false)
	if !hadSystemProxy {
		return err
	}
	a.curSetting().SetSPModeSystemProxy(true)
	if err2 := a.startProfileLocked(running); err2 == nil {
		a.notifyMessage("TUN 开启失败，已回退到系统代理模式")
	} else {
		// The restart failed, so no core listens on the local inbound — and the
		// Windows proxy is still pointed away from it (it was cleared before
		// the stop). Leaving the flag would claim an applied proxy that the
		// machine does not have; everything off is what the state actually is.
		a.curSetting().SetSPModeSystemProxy(false)
		a.notifyMessage("TUN 开启失败，节点未能启动，当前为直连状态", true)
	}
	return fmt.Errorf("TUN 开启失败: %w", err)
}

// SetSystemProxyMode toggles system proxy mode. Turning it on clears TUN mode
// for the same reason as the reverse case in SetVPNMode.
//
// A TUN core has no local inbound at all — the config it was started with only
// carries the tun inbound (see configbuilder) — so merely flipping the flag
// left Windows pointing at a port nobody listens on while the TUN core kept
// running. The switch is therefore applied the same way as the reverse one:
// stop, flip, restart the same profile, so it takes effect at once.
func (a *App) SetSystemProxyMode(on bool) error {
	// The same lock as the other switch, for the same reason: these two are the
	// one pair a reader can press at once — the rail, the tray, a hotkey or the
	// startup restore, from any direction, against whichever one is in flight.
	// The start/stop seat first, the same order SetVPNMode runs (see there).
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	a.modeMu.Lock()
	defer a.modeMu.Unlock()
	defer a.refreshTrayStatus()
	// After the flags have settled, whichever way they settled: what is written down is
	// the state the machine was left in, not what was asked for.
	defer a.rememberSpmode()
	// 节点先行，same guard as SetVPNMode: a Windows proxy pointed at the local
	// port with no core behind it is a machine with no internet, and the
	// reader picks the node before the mode now.
	if on && a.RunningProfile() == core.StoppedID {
		msg := "请先选择并启动一个节点，再开启系统代理"
		a.notifyMessage(msg, true)
		return fmt.Errorf("%s", msg)
	}
	running := a.RunningProfile()
	fromTun := on && a.curSetting().VPNMode()
	if fromTun && running != core.StoppedID &&
		(a.curMgr() == nil || a.curMgr().SnapshotProfile(running) == nil) {
		// The same check as the reverse switch, for the same reason: this path stops
		// the TUN core and restarts the running node on the plain port, and it needs
		// an id that still names something. Said before the core is stopped and
		// before either flag moves.
		msg := "当前节点已不在列表中（可能已被订阅更新替换），请刷新后重新选择节点，再切换系统代理"
		a.notifyMessage(msg, true)
		return fmt.Errorf("%s", msg)
	}
	if fromTun {
		// Flag first, for the same reason as in SetVPNMode: a kill that fails
		// must not leave TUN switched on. Stopping an elevated TUN core needs
		// elevation; if that fails the error surfaces — and the flags stay put,
		// same contract the reverse direction keeps for its own stop failure:
		// the TUN core is most likely still capturing, so a VPN flag already
		// written off would say "off" while the machine is still taken over.
		a.curSetting().SetVPNMode(false)
		if err := a.stopProfileLocked(); err != nil {
			a.curSetting().SetVPNMode(true)
			return err
		}
	}
	a.curSetting().SetSPModeSystemProxy(on)
	if running == core.StoppedID {
		// The on case was refused at the guard above, so this is switch-off with
		// nothing running — which still has to reach Windows. The flag and
		// the machine are two records of the same fact, and only one of them decides
		// whether there is a network: a proxy left pointing at the local port with no core
		// behind it is an offline machine — which is exactly the state this branch used to
		// answer with "nothing to do".
		return a.applySystemProxy(false)
	}
	if fromTun {
		// The core still holds the tun config it was started with, so the switch
		// is completed by restarting the same node on the plain port; that call
		// puts the Windows proxy in place as well.
		err := a.startProfileLocked(running)
		if err == nil {
			a.notifyMessage("系统代理已开启（已重启节点生效）")
			// TUN 已转为系统代理，适配器不再承载任何捕获 —— 当场把前端与网卡拆下，
			// 而不是依赖 3 秒闲置回收倒计时：节点此刻仍在运行，倒计时的前置条件
			// "runningID == StoppedID" 永远不会成立，网卡就会一直留在系统里。
			// 拆下后下一次切回 TUN 会重新授权并重建（状态栏会说明）。
			if err2 := a.stopTunFront(); err2 != nil {
				a.logf("TUN 网卡随手拆除未完成: " + err2.Error())
			}
			return nil
		}
		// The restart after the TUN teardown failed: no core listens on the
		// local inbound, so the flag set above would claim a proxy the machine
		// does not have — the one lie the reverse direction's rollback exists
		// to prevent, and the same contract applies here. The Windows proxy is
		// cleared with it (best effort), so the reader lands on an honest
		// direct state rather than an offline one. The adapter comes down with
		// everything else: nothing is running that would use it.
		a.curSetting().SetSPModeSystemProxy(false)
		if err2 := a.applySystemProxy(false); err2 != nil {
			a.logf("切换系统代理失败后的回退清理也未完成: " + err2.Error())
		}
		if err2 := a.stopTunFront(); err2 != nil {
			a.logf("TUN 网卡拆除未完成（直连态）: " + err2.Error())
		}
		a.notifyMessage("系统代理开启失败（节点在切换后未能重启），当前直连", true)
		return err
	}
	// Announced from here rather than by the frontend, for the reason written on
	// toggleSP in App.tsx: only this side knows whether a node had to be picked or
	// was restarted under the new mode. "Nothing running yet" was answered above;
	// this is the case where a node was already up, and it used to pass in
	// silence — switching the proxy off said so, switching it back on said
	// nothing at all, which reads as the press having been ignored.
	// Clearing it (on == false) stays quiet: the frontend reports that one.
	if err := a.applySystemProxy(on); err != nil {
		if on {
			// The flag was written above; the machine says otherwise. A green
			// switch over a failed registry write is the one state this function
			// must not leave — undo the flag, so the remembered mode and the
			// window's re-read both tell the truth, and hand the error back the
			// way applySystemProxy's own comment promises.
			a.curSetting().SetSPModeSystemProxy(false)
			a.notifyMessage("系统代理开启失败: "+err.Error(), true)
		} else {
			// The flag says off and Windows still points at the port — the
			// leftover every other branch in this file exists to prevent. The
			// switch reads off because that is what was asked; the error says the
			// machine did not get there, and the reader can answer it.
			a.notifyMessage("系统代理关闭失败: "+err.Error(), true)
		}
		return err
	}
	if on {
		a.notifyMessage("系统代理已开启")
	}
	return nil
}

// clearStaleSystemProxy removes a proxy entry this app wrote in a session that ended
// without its exit path — a crash, a kill — and that no mode claims now.
//
// Narrow on purpose: only a server naming this machine's own inbound is cleared. A proxy
// the reader configured, or another program's, is theirs and is left alone. The runtime
// mode flags are not persisted, so after a crash they read off — which is exactly the
// state this function has to make true on the machine as well. Without it a crashed
// session left Windows pointing at a port no core was serving, and the UI offered no way
// back: the switch that would clear it refuses to act before a node is started.
func (a *App) clearStaleSystemProxy() {
	if a.curSetting() == nil || a.curSetting().SPModeSystemProxy() {
		return
	}
	port := a.curSetting().InboundSocksPort
	if port <= 0 {
		return
	}
	enabled, server, err := sysproxy.Current()
	if err != nil || !enabled || strings.TrimSpace(server) == "" {
		return
	}
	lower := strings.ToLower(server)
	if !strings.Contains(lower, fmt.Sprintf("127.0.0.1:%d", port)) &&
		!strings.Contains(lower, fmt.Sprintf("localhost:%d", port)) {
		return
	}
	if err := sysproxy.Clear(); err != nil {
		a.logf("[system proxy] 无法清理上次异常退出残留的代理设置: " + err.Error())
		return
	}
	a.logf("[system proxy] 已清理上次异常退出残留的代理设置: " + server)
}

// applySystemProxy points Windows at the local port, or clears it. The error is
// handed back rather than swallowed so that the one caller that has to answer for
// the switch — SetSystemProxyMode, which the user just pressed — can say so; the
// rest (starting a node, stopping one, quitting) only log it, because those have
// a message of their own and a failed proxy write is not what they are about.
func (a *App) applySystemProxy(on bool) error {
	if on {
		// A PAC script decides per request, so when one is configured it takes the place
		// of the fixed server: the format and the bypass list have nothing to say about
		// it, and Windows holds only one of the two settings at a time anyway.
		if pac := strings.TrimSpace(a.curSetting().SystemProxyPAC); pac != "" {
			if err := sysproxy.SetPAC(pac); err != nil {
				a.logf("[system proxy] " + err.Error())
				return err
			}
			a.logf("[system proxy] 已开启（PAC）: " + pac)
			return nil
		}
		port := a.curSetting().InboundSocksPort
		if err := sysproxy.Set(a.curSetting().SystemProxyFormat, a.curSetting().InboundAddress, port, port,
			a.curSetting().SystemProxyBypass); err != nil {
			a.logf("[system proxy] " + err.Error())
			return err
		}
		a.logf(fmt.Sprintf("[system proxy] 已开启: %s:%d",
			a.curSetting().InboundAddress, port))
		return nil
	}
	if err := sysproxy.Clear(); err != nil {
		a.logf("[system proxy] " + err.Error())
		return err
	}
	a.logf("[system proxy] 已关闭")
	return nil
}

// rememberSpmode records which of the two modes the app is in, so that the next launch
// can put it back (see applyRememberedSpmode). Called from both switches on the way out,
// after their flags have settled — including the paths where a switch was undone, since
// what this reads is the flags rather than the request.
func (a *App) rememberSpmode() {
	s := a.curSetting()
	if s == nil {
		return
	}
	modes := []string{}
	if s.SPModeSystemProxy() {
		modes = append(modes, "system_proxy")
	}
	if s.VPNMode() {
		modes = append(modes, "vpn")
	}
	// A copy through applySetting rather than a field write: the settings pointer is
	// swapped by other paths (a node start remembers its id, the settings panel replaces
	// the whole struct), and a write into the copy this call happens to hold is an update
	// nobody reads — the remembered mode the next launch restores would be a stale one.
	if err := a.applySetting(s.WithRememberSpmode(modes)); err != nil {
		a.logf("[settings] 无法记录本次模式: " + err.Error())
	}
	// The mode is a fact of the backend, not of the window: the tray's two
	// switches, a global hotkey and the remembered-mode restore at startup all
	// change it without anything being pressed in the UI, and the rail kept
	// showing the last value that window itself chose — until the next press,
	// which asked against a mode that had already moved. This is the one moment
	// every record of the mode agrees (both switches defer here, whichever way
	// their flags settled), so it is the moment to tell the window to re-read.
	// The event carries no payload: GetSPMode is the answer, and re-reading it
	// keeps this the announcement of a settled state rather than a claim about one.
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "spmode:changed")
	}
}

// applyRememberedSpmode puts the last session's mode back on, when the reader has asked
// for that (remember_spmode) and there was one. It runs once, a moment after the window
// is up: a mode switch can stop and start a node, and on the TUN path it can ask for
// administrator rights, and both of those want something on screen to explain them.
//
// The mode is named before it is applied, because the two paths announce their outcomes
// differently and the TUN one replaces this process with an elevated copy — a note
// written after the call would never be seen.
func (a *App) applyRememberedSpmode() {
	s := a.curSetting()
	if s == nil || !s.RememberSpmodeOn || len(s.RememberSpmode) == 0 {
		return
	}
	wantProxy, wantVPN := false, false
	for _, m := range s.RememberSpmode {
		switch m {
		case "system_proxy":
			wantProxy = true
		case "vpn":
			wantVPN = true
		}
	}
	// The two are mutually exclusive by construction — switching one on clears the other
	// — so a file carrying both is answered with TUN, which is the stronger reading.
	// 节点先行 applies to the restore too: the remembered node comes up first,
	// and a session that ended without a usable node restores nothing — the
	// reader picks one, and the mode switch is one press away.
	switch {
	case wantVPN || wantProxy:
		if a.RunningProfile() == core.StoppedID {
			id := s.RememberID
			if id == core.StoppedID || a.curMgr() == nil || a.curMgr().GetProfile(id) == nil {
				a.notifyMessage("上次的模式未恢复：请先选择并启动一个节点", true)
				return
			}
			a.notifyMessage("正在恢复上次的节点…")
			if err := a.StartProfile(id); err != nil {
				a.notifyMessage("恢复上次模式失败: "+err.Error(), true)
				return
			}
		}
		if wantVPN {
			if a.curSetting().VPNMode() {
				// The node above came up straight into its tun config (the flag
				// was remembered on), so there is nothing left to switch.
				a.notifyMessage("TUN 模式已恢复")
				return
			}
			a.notifyMessage("正在恢复上次的 TUN 模式…")
			if err := a.SetVPNMode(true); err != nil {
				a.notifyMessage("恢复 TUN 模式失败: "+err.Error(), true)
			}
			return
		}
		if err := a.SetSystemProxyMode(true); err != nil {
			a.notifyMessage("恢复系统代理失败: "+err.Error(), true)
		}
	}
}
