package main

import (
	"eagle/internal/configbuilder"
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/netx"
	"eagle/internal/setting"
	"eagle/internal/sysproxy"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SetVPNMode toggles TUN mode. Turning it on clears the system proxy, because
// both hijack the machine's network path: with both on, traffic is captured
// twice and which one actually takes effect is undefined.
//
// The node keeps running across the switch whenever the config it is running is
// the config the new mode needs — which is the usual case, because the mode
// enters the config in one place only (see coreServesCurrentMode). Then the
// switch is the capture coming up or going down, and nothing else: no stop, no
// start, and no gap in the traffic. When the config does differ, the core is
// rebuilt on the new one, as it always was.
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
	//
	// On a goroutine of its own: the flag is already flipped when it is spawned,
	// so the state it writes is the settled one — but this call can reach
	// ensureTunServer (a UAC install and up to a ten-second readiness wait)
	// through its due throttle, and parking that inside startStopMu + modeMu
	// would queue every other switch, hotkey and tray press behind a dialog
	// (the same hazard takeTunRoutes was moved async for, see app_tunfront.go).
	go crashlog.Guard("reconcileSystemDNS", func() { a.reconcileSystemDNS(false) })
	if running == core.StoppedID {
		// The switch is off and nothing has ever run (the on case was refused
		// at the guard above). An earlier switch may still have left routes
		// behind from one that never reached a node: the adapter stays — it costs
		// nothing idle and keeps the next switch-on instant — but its routes
		// must not, and with nothing behind it the countdown takes it apart.
		a.releaseTunRoutes()
		a.armTunIdleReclaim()
		return nil
	}
	if !on {
		// TUN off. The routes go back to the physical adapter first: the machine
		// is direct from here on, and the config the new mode would run is
		// written against that routing.
		a.releaseTunRoutes()
		// The node keeps running when the core that is running is already the
		// core this mode would start. Then the switch is the capture coming
		// down and nothing else: the local inbound keeps listening, the
		// system-proxy switch finds a node that is already up, and the reader's
		// traffic does not go through a stop and a start to get there.
		//
		// The front-end stays as it is. It holds the adapter and nothing else:
		// the capture is gone, the device sits idle, and the next switch back
		// to TUN is a matter of taking the routes again rather than of a fresh
		// authorisation and a fresh device.
		if a.coreServesCurrentMode(running) {
			a.notifyMessage("TUN 模式已关闭，节点继续运行")
			return nil
		}
		// The config moved, so the core is rebuilt on the plain port. The
		// front-end goes with it: a capture with nothing behind it is a
		// machine whose traffic enters a tunnel that leads nowhere.
		//
		// The switch's form: the node is restarted below, so this is a step in
		// the mode change rather than a stop (see stopForSwitchLocked).
		if err := a.stopForSwitchLocked(); err != nil {
			// The core is still running, so the node is still serving — and the
			// capture is already down, which is what the flag says. Nothing to
			// undo here: the off path never took the system proxy away.
			return err
		}
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
	// TUN on. The core keeps running when the config it is running is already
	// the config this mode needs: then the switch is the capture coming up —
	// the front-end that holds the adapter and the routes that point the
	// machine at it — and the node behind it is never interrupted.
	if a.coreServesCurrentMode(running) {
		// The same shape the restart path below keeps (see tunFrontSwitchWait):
		// the capture's completion can sit behind the reader's own answer to a
		// UAC prompt, and past this switch's window it is carried out by a
		// background goroutine — this hook is that switch's bookkeeping taken
		// there. The core never stopped, so the undo is the proxy pointer alone;
		// that is true whichever way the answer arrived.
		settled := func(ferr error) {
			if ferr == nil {
				a.notifyMessage("TUN 模式已开启")
				return
			}
			a.curSetting().SetVPNMode(false)
			if hadSystemProxy {
				a.curSetting().SetSPModeSystemProxy(true)
				if err2 := a.applySystemProxy(true); err2 != nil {
					a.logf("[system proxy] TUN 开启失败后的回退也未完成: " + err2.Error())
				}
			}
			a.notifyMessage("TUN 开启失败: "+ferr.Error(), true)
		}
		a.notifyMessage("正在启动 TUN 节点…")
		if err := a.startCapture(settled); err != nil {
			// The capture did not come up (a declined prompt, a missing
			// wintun.dll, a broken adapter). The flag goes back off, and the
			// system proxy it took away goes back on: the core never stopped,
			// so putting the machine back is a matter of the proxy pointer
			// alone.
			a.curSetting().SetVPNMode(false)
			if hadSystemProxy {
				a.curSetting().SetSPModeSystemProxy(true)
				if err2 := a.applySystemProxy(true); err2 != nil {
					a.logf("[system proxy] TUN 开启失败后的回退也未完成: " + err2.Error())
				}
			}
			a.notifyMessage("TUN 开启失败: "+err.Error(), true)
			return fmt.Errorf("TUN 开启失败: %w", err)
		}
		// A start that handed its front-end completion to the background — the
		// prompt still open — has not captured anything yet: said as the wait
		// it is, and the completion's own message follows when the reader
		// answers.
		if a.tunFrontAwaiting.Swap(false) {
			a.notifyMessage("TUN 正在等待管理员授权，确认弹窗后自动接管")
			return nil
		}
		a.notifyMessage("TUN 模式已开启")
		return nil
	}
	// The switch's form: both directions below start a core again (see
	// stopForSwitchLocked).
	if err := a.stopForSwitchLocked(); err != nil {
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
	// VPN start goes through RunElevated: if the user declines the UAC prompt
	// the error surfaces here and the node stays stopped, which is honest.
	// Announced here for the same reason as its counterpart in
	// SetSystemProxyMode: the frontend stays quiet when a mode is switched on, so
	// a switch that restarts the node has to say so from this side or the press
	// answers with nothing. applyPendingTun uses these same words for the
	// relaunched case, so both routes into "TUN is on" read alike.
	//
	// The front-end's answer can sit behind the UAC prompt itself, and past
	// this switch's window (see tunFrontSwitchWait) the completion is carried
	// out by a background goroutine — this hook is that switch's bookkeeping
	// taken there: the success line when the prompt was answered and the
	// capture came up, and the rollback below when it was not.
	settled := func(ferr error) {
		if ferr == nil {
			a.notifyMessage("TUN 模式已开启（已重启节点生效）")
			return
		}
		a.rollbackTunSwitchOn(running, hadSystemProxy)
		a.notifyMessage("TUN 开启失败: "+ferr.Error(), true)
	}
	err := a.startProfileLockedOn(running, settled)
	if err == nil {
		// A start that handed its front-end completion to the background — the
		// prompt still open (see tunFrontSwitchWait) — has not captured
		// anything yet: said as the wait it is, and the completion's own
		// message follows when the reader answers the dialog.
		if a.tunFrontAwaiting.Swap(false) {
			a.notifyMessage("TUN 正在等待管理员授权，确认弹窗后自动接管")
			return nil
		}
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
	a.rollbackTunSwitchOn(running, hadSystemProxy)
	return fmt.Errorf("TUN 开启失败: %w", err)
}

// rollbackTunSwitchOn undoes a TUN switch-on whose start failed after the
// teardown had already run — the Windows proxy cleared, the old core stopped,
// the TUN start refused (a declined UAC prompt, a missing wintun.dll…). TUN
// did not come up, so the flag stands down, and the machine goes back to the
// system-proxy state it was in. The restart runs on the plain path, where none
// of the TUN failure causes apply; if even that fails, the flags stay
// consistent and the reader is told, instead of the machine silently pointing
// at a port nobody listens on.
//
// Shared by the switch's own failure path and the background completion of a
// front-end that answered past the switch's window (see finishTunFrontPending)
// — the two must leave the same machine behind, so they are the one body.
// The seats are held by the caller in both cases. The remembered mode and the
// tray follow from the caller (SetVPNMode's defers, or the completion's own
// re-record), so they are not touched here.
func (a *App) rollbackTunSwitchOn(running int, hadSystemProxy bool) {
	a.curSetting().SetVPNMode(false)
	if !hadSystemProxy {
		return
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
}

// SetSystemProxyMode toggles system proxy mode. Turning it on clears TUN mode
// for the same reason as the reverse case in SetVPNMode.
//
// The node keeps running across the switch whenever the config it is running is
// the config the new mode needs (see coreServesCurrentMode): the local inbound
// is there in both modes — the tun inbound lives in the front-end, not in the
// main core — so the switch is the capture coming down and the Windows proxy
// going up, and the node is never interrupted. When the config does differ, the
// core is rebuilt on the new one, as it always was.
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
	// keepCore answers whether the node survives the switch untouched. It is
	// asked only when the switch really is a switch between two modes — from TUN
	// to the system proxy — and the flag it is written for is read further down.
	keepCore := false
	if fromTun {
		// TUN gives way to the system proxy. The routes go back to the physical
		// adapter first: the machine is direct from here on, and the config the
		// new mode would run is written against that routing.
		a.releaseTunRoutes()
		// The node keeps running when the core that is running is already the
		// core this mode would start: the local inbound is listening either way
		// (the tun inbound lives in the front-end, not in the main core), so
		// the switch is the capture coming down and the Windows proxy going up.
		keepCore = a.coreServesCurrentMode(running)
		// Flag first, for the same reason as in SetVPNMode: a kill that fails
		// must not leave TUN switched on. Stopping an elevated TUN core needs
		// elevation; if that fails the error surfaces — and the flags stay put,
		// same contract the reverse direction keeps for its own stop failure:
		// the TUN core is most likely still capturing, so a VPN flag already
		// written off would say "off" while the machine is still taken over.
		a.curSetting().SetVPNMode(false)
		if !keepCore {
			if err := a.stopProfileLocked(); err != nil {
				a.curSetting().SetVPNMode(true)
				return err
			}
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
	if fromTun && keepCore {
		// The core never stopped, so the Windows proxy goes up on top of the
		// node that is already serving. The front-end stays as well: the
		// adapter is idle now, and the next switch back to TUN is a matter of
		// taking the routes again rather than of a fresh authorisation.
		if err := a.applySystemProxy(true); err != nil {
			a.curSetting().SetSPModeSystemProxy(false)
			a.notifyMessage("系统代理开启失败: "+err.Error(), true)
			return err
		}
		a.notifyMessage("系统代理已开启")
		return nil
	}
	if fromTun {
		// The config moved, so the core is rebuilt on the plain port; that call
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

// coreServesCurrentMode reports whether the core that is running is already the
// core the current mode would start.
//
// A mode switch reaches the config in one place only: the mapping inbound a
// first-profile naive / hysteria2 / tuic node is reached through, which exists
// because a direct connection under TUN would fall back into the tunnel it is
// serving (see configbuilder.needExternal). Everything else about the config is
// the same in both modes — the local inbound is in it either way, because the
// tun inbound lives in the front-end and not in the main core.
//
// So when the config the running core was started with and the config this mode
// builds are the same, there is nothing to rebuild for, and the node keeps
// running: the capture comes up or goes down around it, and the traffic does
// not go through a stop and a start to get there.
//
// The comparison is against the config the core is really running, not against
// one rebuilt for the mode it was started in: the reader may have changed a
// setting since, and a core that does not carry the current settings is a core
// that has to be rebuilt whatever the mode does.
func (a *App) coreServesCurrentMode(id int) bool {
	ctrl := a.curController()
	if ctrl == nil {
		return false
	}
	// A core that is running but not answering is a core to rebuild, not one to
	// keep: a mode switch used to be the moment a hung core was replaced, and
	// that is worth keeping.
	if !ctrl.InboundReady() {
		return false
	}
	running := ctrl.RunningConfig()
	if running == nil {
		return false
	}
	mgr, s := a.curMgr(), a.curSetting()
	if mgr == nil || s == nil {
		return false
	}
	ent := mgr.SnapshotProfile(id)
	if ent == nil {
		return false
	}
	res, err := configbuilder.BuildConfig(ent, mgr, s, false)
	if err != nil {
		// A config that cannot be built is a reason to rebuild the core the old
		// way, not a reason to keep one that may not fit the new mode.
		a.logf("[mode] 生成当前模式的配置失败: " + err.Error())
		return false
	}
	return reflect.DeepEqual(running, res.Config)
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
	sysproxy.SetUndoFile(a.sysproxyUndoPath())
	if sysproxy.HasUndo() {
		// A takeover is on record and the normal Clear never ran — the crash
		// this function exists for. The record holds the machine's own setting
		// from before the takeover, and restoring it beats any guess: it is
		// right for a fixed server, for the PAC form (which the old
		// enabled+server test read as "no proxy" and left behind), and for a
		// reader whose proxy was another program's entirely.
		if err := sysproxy.Clear(); err != nil {
			a.logf("[system proxy] 无法还原异常退出前的原始代理设置: " + err.Error())
			return
		}
		a.logf("[system proxy] 已还原异常退出前的原始代理设置")
		return
	}
	// No record (a build from before the undo file, or one whose record went
	// missing): fall back to "did this app write what is there" — the fixed
	// server naming this machine's inbound, or this very setting's PAC URL.
	port := a.curSetting().InboundSocksPort
	if port <= 0 {
		return
	}
	enabled, server, pac, err := sysproxy.Current()
	if err != nil {
		return
	}
	lower := strings.ToLower(server)
	ours := enabled && strings.TrimSpace(server) != "" &&
		(strings.Contains(lower, fmt.Sprintf("127.0.0.1:%d", port)) ||
			strings.Contains(lower, fmt.Sprintf("localhost:%d", port)))
	if !ours {
		if want := strings.TrimSpace(a.curSetting().SystemProxyPAC); want != "" && pac == want {
			ours = true // the PAC we wrote, with ProxyEnable off: still ours
		}
	}
	if !ours {
		return
	}
	if err := sysproxy.Clear(); err != nil {
		a.logf("[system proxy] 无法清理上次异常退出残留的代理设置: " + err.Error())
		return
	}
	a.logf("[system proxy] 已清理上次异常退出残留的代理设置: " + server)
}

// sysproxyUndoPath is where the pre-takeover proxy snapshot lives: beside the
// other machine-state files the resident server and the crash cleanup read,
// under the live data directory (which can move, so it is read per call rather
// than remembered).
func (a *App) sysproxyUndoPath() string {
	dir := a.curDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "temp", "sysproxy-undo.json")
}

// applySystemProxy points Windows at the local port, or clears it. The error is
// handed back rather than swallowed so that the one caller that has to answer for
// the switch — SetSystemProxyMode, which the user just pressed — can say so; the
// rest (starting a node, stopping one, quitting) only log it, because those have
// a message of their own and a failed proxy write is not what they are about.
func (a *App) applySystemProxy(on bool) error {
	// The undo record tracks the live data directory (see sysproxy.SetUndoFile);
	// set per call so a directory switch takes effect without a restart.
	sysproxy.SetUndoFile(a.sysproxyUndoPath())
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
	// Read and written inside applyMu (see mutateSetting): the flags come from
	// the object that is live under the lock, not from a copy an intervening
	// save may have replaced.
	if err := a.mutateSetting(func(next *setting.Setting) error {
		modes := []string{}
		if next.SPModeSystemProxy() {
			modes = append(modes, "system_proxy")
		}
		if next.VPNMode() {
			modes = append(modes, "vpn")
		}
		next.RememberSpmode = modes
		return nil
	}); err != nil {
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
	if a.curCtx() != nil {
		runtime.EventsEmit(a.curCtx(), "spmode:changed")
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
			if wantVPN {
				// The flag goes down BEFORE the node comes up, so the profile
				// starts straight into its tun config — one core start, no live
				// switch. The other order started the node twice: once in the
				// plain mixed-inbound config, and then again under TUN, and the
				// first (useless) start was on the reader's path. (app.go's
				// applyPendingTun does it this way too: "the flag goes down
				// before the node comes up ... one core start, no live switch".)
				a.curSetting().SetVPNMode(true)
				// The same call SetVPNMode makes once the flag is true: the
				// elevated side has to be told that the takeover is wanted.
				a.reconcileSystemDNS(false)
			}
			// The seat and the locked form, the way StartProfile takes them,
			// with the restore's own settlement hook: a TUN start whose
			// front-end answer sits behind the reader's answer to a UAC prompt
			// hands the rest to a background goroutine (see
			// tunFrontSwitchWait), and the flag set above stands down with a
			// start that never landed — from that goroutine, through this hook.
			a.startStopMu.Lock()
			settled := func(ferr error) {
				if ferr == nil {
					a.notifyMessage("TUN 模式已恢复")
					return
				}
				a.curSetting().SetVPNMode(false)
				a.refreshTrayStatus()
				a.logf("[vpn] " + ferr.Error())
			}
			err := a.startProfileLockedOn(id, settled)
			a.startStopMu.Unlock()
			if err != nil {
				if wantVPN {
					// The flag stands down with the node that never came up.
					a.curSetting().SetVPNMode(false)
				}
				a.notifyMessage("恢复上次模式失败: "+err.Error(), true)
				return
			}
		}
		if wantVPN {
			if a.curSetting().VPNMode() {
				// The node above came up straight into its tun config (the flag
				// was remembered on), so there is nothing left to switch.
				a.refreshTrayStatus()
				a.notifyMessage("TUN 模式已恢复")
				return
			}
			// The node was already running when the restore ran: it came up in
			// the plain config, and the switch is what puts it under TUN.
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
