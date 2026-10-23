package main

import (
	"eagle/internal/clash"
	"eagle/internal/configbuilder"
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/setting"
	"eagle/internal/sub"
	"fmt"
)

// StartProfile starts the given profile (TUN/VPN when enabled).
// SetCurrentGroup persists the subscription the UI is showing, so a restart comes
// back to it instead of always landing on the first tab. Selecting a tab counts —
// whether by click or by hovering it — because from the user's point of view
// "the one I was just looking at" is the one to reopen.
func (a *App) SetCurrentGroup(id int) error {
	s := a.curSetting()
	if s == nil || s.CurrentGroup == id {
		return nil
	}
	if !a.curMgr().HasGroup(id) {
		return fmt.Errorf("group %d not found", id)
	}
	// A copy and a swap, not a write into the live object — and the copy has to
	// be taken inside applyMu (see mutateSetting), or a save landing in between
	// writes the old tab back: the group the reader picked was lost, and the
	// next launch came back on it.
	return a.mutateSetting(func(next *setting.Setting) error {
		next.CurrentGroup = id
		return nil
	})
}

// recordHotSnapshot remembers what the running config was built from. Called after a
// start, and never after a hot switch: the switch moves a selector inside the same config,
// so the content that config carries is still the content recorded at its start.
func (a *App) recordHotSnapshot() {
	if a.curController() == nil || a.curMgr() == nil {
		return
	}
	nodes := a.curController().HotNodes()
	keys := make(map[int]string, len(nodes))
	for id := range nodes {
		if ent := a.curMgr().SnapshotProfile(id); ent != nil {
			keys[id] = sub.BeanKey(ent, false)
		}
	}
	a.hotMu.Lock()
	a.hotKeys = keys
	a.hotMu.Unlock()
}

// hotSnapshotMatches answers whether the store still holds the node the running config
// was built from. No recorded snapshot answers no: a restart is slower, never wrong.
func (a *App) hotSnapshotMatches(id int) bool {
	a.hotMu.Lock()
	key, ok := a.hotKeys[id]
	a.hotMu.Unlock()
	if !ok || a.curMgr() == nil {
		return false
	}
	ent := a.curMgr().SnapshotProfile(id)
	return ent != nil && sub.BeanKey(ent, false) == key
}

// tryHotSwitch moves the running core to another node without restarting it, and reports
// whether it could.
//
// This is the whole point of the selector the config now carries (see
// configbuilder.proxyGroup): the node wanted is already inside the running config, so the
// only thing that changes is which of that selector's outbounds the proxy tag is holding.
// The core keeps running, and the tun adapter it holds is never touched — which, under TUN,
// is the difference between a switch and a two-second disconnect with the adapter rebuilt
// around it.
//
// Anything that cannot be answered here — the node is not in the running config, its
// content changed since that config was built, no Clash API is configured, the call fails —
// returns false, and the caller starts the core again. That is the old behaviour, and it is
// still the right one whenever the config and the node list have drifted apart: a new
// subscription, an edited node, a different group.
func (a *App) tryHotSwitch(id int) bool {
	s := a.curSetting()
	if s == nil || s.CoreBoxClashAPI <= 0 || a.curController() == nil {
		return false
	}
	if a.RunningProfile() == id {
		// Already on it — unless the row was edited after the config was built, in which
		// case the core is not running what the store now holds. "Already on it" would be
		// the one answer that leaves that edit unapplied forever; falling through to the
		// restart path is what makes a double click on an edited row mean something.
		return a.hotSnapshotMatches(id)
	}
	tag, ok := a.curController().HotNodes()[id]
	if !ok {
		return false
	}
	// Same check for the node being switched to: its content may have changed since the
	// outbound it answers to was built.
	if !a.hotSnapshotMatches(id) {
		return false
	}
	err := clash.New(s.CoreBoxClashAPI, s.CoreBoxClashAPISecret).
		Select(configbuilder.ProxyTag, tag)
	if err != nil {
		a.logf("[hotswitch] " + err.Error())
		return false
	}
	// Nothing else would tell the app which node is up: the core kept running, so its own
	// record — the one RunningProfile reads — was never touched. Leaving it alone is why
	// the window kept naming the old node and never refreshed the exit address: the
	// table's running row and the corner both read from here, and the frontend refreshes
	// the address only when the running id changes.
	a.curController().SwitchedTo(id)
	// The connections in the pool were opened through the outbound that was up a moment
	// ago, and a kept-alive one stays bound to it: reusing one would answer with the
	// previous node's address. Drop them, forget the cached answer, and let the warm-up
	// open the new path while the window draws the row.
	a.dropExitConns()
	a.exitProbeClear()
	go crashlog.Guard("warmExitProbe", a.warmExitProbe)
	a.rememberStarted(id)
	name := ""
	if ent := a.curMgr().SnapshotProfile(id); ent != nil {
		name = ent.DisplayName()
	}
	// To the log rather than to the status bar: the front end already announces the switch
	// it asked for, and a second line about it would be the same sentence twice. What this
	// one adds is the part a reader only needs when something looks wrong — that the core
	// was not restarted, which is exactly what the log is for.
	a.logf(fmt.Sprintf("<<< 已切换到 [%s]（核心未重启）", name))
	return true
}

// StartProfile starts a profile, hot-switching inside the running config when it can and
// rebuilding the core when it cannot.
func (a *App) StartProfile(id int) error {
	// Every source — webview binding, tray, crash supervisor, startup restore —
	// queues here (see startStopMu); the body is the locked form, which the
	// mode switches call directly while holding the same lock.
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	return a.startProfileLocked(id)
}

func (a *App) startProfileLocked(id int) error {
	return a.startProfileLockedOn(id, nil)
}

// startProfileLockedOn is startProfileLocked with a front-end settlement hook.
// Only a TUN start can call it with a hook: the front-end's answer can sit
// behind a UAC prompt (see tunFrontSwitchWait), and past that window the
// completion runs on a background goroutine — this hook is what carries the
// caller's own bookkeeping there (the success line, the flag-and-proxy
// rollback a mode switch applies on a failure). A plain start passes nil and
// gets the plain answer.
func (a *App) startProfileLockedOn(id int, frontSettled func(error)) error {
	// An explicit start is the reader asking for the node again, so the stop
	// intent from an earlier press is spent (see stopRequested). Cleared here,
	// at the one entrance every start goes through — the binding, the tray, the
	// hotkeys, the crash supervisor and the startup restore.
	a.stopRequested.Store(false)
	// Nothing here touches the crash count. A start that answers success is not proof the
	// core will stay up — the rule-set failure of a real session started fine and exited
	// two seconds later, five times in a minute — so clearing the count on success made
	// the supervisor restart it for ever. The count is kept by onCoreExit alone, which
	// appends an entry only for an exit nobody asked for and forgets entries older than
	// its window: exactly the "three crashes in a minute" the limit is meant to say.
	if a.curController() == nil {
		return fmt.Errorf("backend not initialised")
	}
	// Checked before anything is torn down, and said in the reader's terms. The id
	// can be one that no longer names anything: a subscription update replaced the
	// node, the reader deleted it, its group was removed. Everything below stops the
	// old core first, so discovering it further down meant stopping a working node to
	// start nothing, and answering "profile N not found" to someone who had every
	// reason to think the node existed — they had just clicked its row.
	if a.curMgr() == nil || a.curMgr().SnapshotProfile(id) == nil {
		return fmt.Errorf("节点 %d 已不在列表中（可能已被订阅更新替换），请刷新后重新选择", id)
	}
	defer a.emitRunningState()
	// The expected-stop flag is not touched here. It belongs to the stop that raised
	// it, and onCoreExit consumes it when that stop's exit lands — clearing it on
	// entry is what used to misfire the mode switch: the stop and this start are two
	// separate calls, the old core's exit runs on its own goroutine, and a callback
	// arriving after this line had already taken the flag down read the planned stop
	// as a crash and started the node again under the switch's own start. A stop that
	// never raises its exit takes the flag down itself (see unmarkExpectedStop).
	// A node that is already in the running config is switched to through the core's own
	// API, without stopping anything — see tryHotSwitch. Checked before the mode branches
	// because it holds in both of them: under TUN the same config carries the tun inbound,
	// and not restarting is the whole point there.
	if a.tryHotSwitch(id) {
		return nil
	}
	// Nothing to switch between: the node wanted is not in the running config (a new
	// subscription, an edited node, another group), or no core is up. A core runs one
	// config, so replacing it means stopping the old one first — and that is now the only
	// remaining reason for a restart, which is why it lives here rather than in each
	// caller. Callers used to do it themselves, and that is exactly why the switch never
	// took the hot path: whoever stopped the core first had already emptied the map of
	// what could be switched between.
	if a.RunningProfile() != core.StoppedID {
		// Already inside startStopMu: the locked form, not the wrapper (a
		// sync.Mutex does not re-enter). The switch's form, not the reader's:
		// the start below is what was asked for (see stopForSwitchLocked).
		if err := a.stopForSwitchLocked(); err != nil {
			return err
		}
	} else if pid, _ := a.vpnState(); pid > 0 {
		if err := a.stopForSwitchLocked(); err != nil {
			return err
		}
	}
	if a.curSetting().VPNMode() {
		if err := a.startVPN(id, frontSettled); err != nil {
			return err
		}
		a.recordHotSnapshot()
		// The direct branch remembers the id below and the routing rebuild does the
		// same (ApplyRouting); leaving the TUN branch out meant "记住上次节点" held the
		// node from before the mode went on — tray, hotkeys and autostart all
		// restarted a node the reader had already switched away from.
		a.rememberStarted(id)
		a.exitProbeClear()
		go crashlog.Guard("warmExitProbe", a.warmExitProbe)
		return nil
	}
	// 节点先行：a core started with neither mode on listens on a port nothing
	// points at yet — and that is by design now. It is the state the node-first
	// flow puts the machine in: the reader picks a node first, and the TUN or
	// system-proxy switch on top of it is what points Windows at this port.
	if err := a.curController().Start(id); err != nil {
		return err
	}
	a.recordHotSnapshot()
	a.rememberStarted(id)
	a.exitProbeClear()
	go crashlog.Guard("warmExitProbe", a.warmExitProbe)
	if a.curSetting().SPModeSystemProxy() {
		if err2 := a.applySystemProxy(true); err2 != nil {
			// The node is up and the flag claims a proxy the machine does not
			// have — a green switch over a failed write, the one state the
			// system-proxy paths refuse to leave. Stand the flag down: the switch
			// reads off, which is the machine's truth, and pressing it again is
			// the path that answers its own failures.
			a.curSetting().SetSPModeSystemProxy(false)
			a.notifyMessage("系统代理写入失败: "+err2.Error(), true)
		}
	}
	return nil
}

// rememberStarted mirrors nekoray's DataStore::UpdateStartedId: when "remember
// last proxy" is enabled the started profile becomes the one the tray's
// "start last profile" item uses, otherwise the memory is cleared.
func (a *App) rememberStarted(id int) {
	s := a.curSetting()
	if s == nil {
		return
	}
	want := core.StoppedID
	if s.RememberEnable {
		want = id
	}
	if s.RememberID == want {
		return
	}
	// Inside the lock, and compared again there: the check above can be read
	// from a setting another writer has since replaced (see mutateSetting).
	if err := a.mutateSetting(func(next *setting.Setting) error {
		if next.RememberEnable {
			next.RememberID = id
		} else {
			next.RememberID = core.StoppedID
		}
		return nil
	}); err != nil {
		a.logf("[remember] " + err.Error())
	}
}

// StopProfile stops the running profile.
func (a *App) StopProfile() error {
	// Same queue as StartProfile (see startStopMu); the mode switches call
	// stopProfileLocked directly while holding it.
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	return a.stopProfileLocked()
}

func (a *App) stopProfileLocked() error {
	defer a.emitRunningState()
	// The reader wants the node down (see stopRequested): said before the kill,
	// so an exit that lands after the process died — but before the supervisor
	// looked at it — is still read as this press rather than as a crash. The
	// next start clears it.
	a.stopRequested.Store(true)
	return a.stopCoreLocked()
}

// stopForSwitchLocked is the stop a switch makes on its way to starting
// something else (a node switch, a mode change, a restart). It clears the
// intent rather than setting it: the start that follows is what the reader
// asked for, and marking this stop as one would leave the supervisor switched
// off for the rest of the session — a crash after a single switch then went
// unnoticed, unlogged and unrestarted.
func (a *App) stopForSwitchLocked() error {
	defer a.emitRunningState()
	a.stopRequested.Store(false)
	return a.stopCoreLocked()
}

// stopCoreLocked is the stop itself, with no opinion about intent: the caller
// has already said whether this is the reader's stop or a step towards a start.
func (a *App) stopCoreLocked() error {
	if pid, _ := a.vpnState(); pid > 0 {
		return a.stopVPN()
	}
	if a.curController() == nil {
		return nil
	}
	// Said before the stop, so the exit it causes is recognised as asked for — see
	// onCoreExit, which would otherwise treat every stop as a crash and start the
	// node again.
	a.markExpectedStop()
	a.flushTraffic()
	err := a.curController().Stop()
	// The mark stays up when the stop failed, on purpose: the kill was asked for and
	// the process may still die of it a moment later, and that death is the stop
	// landing late rather than a crash. It names a pid, so it cannot swallow an exit
	// of anything else.
	//
	// The proxy is cleared only when the core is really gone, and only when no new one
	// has taken the seat while this stop was waiting for the old one to die. Two calls
	// (a switch, a tray press) can overlap here, and the late one used to clear the
	// entry the earlier one had just written for the core it started — traffic went
	// direct under a switch that said proxied, and the caller got a failure it did not
	// have.
	if a.runningID() == core.StoppedID && a.curSetting().SPModeSystemProxy() {
		a.applySystemProxy(false)
	}
	return err
}

// vpnState returns the elevated core's pid and profile in one atomic read, so
// callers never see a pid from one moment paired with a profile from another.
func (a *App) vpnState() (pid int, profileID int) {
	a.vpnMu.Lock()
	defer a.vpnMu.Unlock()
	return a.vpnPID, a.vpnProfileID
}

func (a *App) setVPNState(pid, profileID int) {
	a.vpnMu.Lock()
	a.vpnPID, a.vpnProfileID = pid, profileID
	a.vpnMu.Unlock()
}

// clearFrontRecordIfMine tears the front-end's record down only while it still
// names the pid the caller read when its stop began. A stop is slow — a server
// timeout, a kill — and ensureTunFront can raise and record a new front-end in
// that window; the unconditional clear stopTunFront used to do then erased the
// new owner's pid, and every later path read "no front-end" over a live
// elevated process holding the adapter. The check is the one watchTunFront has
// always run before reacting to a death (the "记录已换主" branch there); the
// clear and the read happen under the same hold, so no writer can land in
// between.
func (a *App) clearFrontRecordIfMine(pid int) {
	a.vpnMu.Lock()
	mine := a.vpnPID == pid
	if mine {
		a.vpnPID, a.vpnProfileID = 0, 0
	}
	a.vpnMu.Unlock()
	if !mine {
		a.logf(fmt.Sprintf("[vpn] TUN 前端记录已换主（pid %d 已不是当前记录），清理交由新的所有者", pid))
		return
	}
	a.clearTunFrontPID()
	a.clearTunAdapterAfterStop()
}

func (a *App) stopVPN() error {
	// Only the main core stops here. The tun front-end keeps running — it holds the
	// adapter, and rebuilding that is exactly the cost this split exists to avoid. It
	// comes down when the app itself is going away (see stopTunFront and runShutdown),
	// not when a node or an outbound mode changes.
	//
	// Its routes are a different matter, and they do come down here: the adapter is
	// only worth capturing with if something is behind it to forward through, and a
	// core that just stopped is not. Leaving them would make "stop the node" mean
	// "no internet" — which is what it used to mean, until the next TUN switch.
	//
	// Its log tail keeps running with it for the same reason: the front-end is still
	// writing.
	if a.curController() == nil {
		return nil
	}
	hadCore := a.runningID() != core.StoppedID
	if hadCore {
		// Stopped on purpose. The non-VPN branch of StopProfile records the same
		// thing one step earlier; this path skipped it, and onCoreExit read the
		// planned stop as a crash — announcing "意外退出" and starting the node
		// again on top of the reader's own stop.
		a.markExpectedStop()
	}
	// The counters' last turn belongs to this stop, as it does on the direct
	// branch (StopProfile flushes before its own stop): the poll lands them on
	// disk only every thirty ticks, and a stop that skipped the flush lost the
	// last half minute of the node's attribution — the same act persisting a
	// different amount of traffic depending on which mode it ran under.
	a.flushTraffic()
	err := a.curController().Stop()
	a.releaseTunRoutes()
	// The adapter has nothing behind it now, so it is on the countdown that takes it
	// apart; a node started before that fires cancels it and reuses the device.
	a.armTunIdleReclaim()
	if hadCore {
		// Said only when there was a core to stop. A capture brought up by the mode
		// switch leaves the front-end running with no core behind it, and the node
		// start that follows passes through here — announcing "node stopped" to a
		// reader who is in the middle of starting one is worse than saying nothing.
		a.logf("<<< stopped main core (tun front-end kept)")
		if a.curSetting() != nil && a.curSetting().VPNMode() {
			a.notifyMessage("节点已停止（TUN 网卡保持）")
		} else {
			// A mode switch to system proxy tears its own adapter down right after
			// the restart lands (see SetSystemProxyMode) — from this line's point
			// of view, the node is simply stopped.
			a.notifyMessage("节点已停止")
		}
	}
	return err
}

// RestartProfile stops and starts the running core again.
func (a *App) RestartProfile() error {
	// One stop+start, one seat in the queue: without the hold, another source's
	// start could slip between the two halves and the restart would land on a
	// core it never stopped.
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	// The controller answers for every TUN shape alike: in the two-process setup the
	// front-end holds an adapter and a pid, not a profile.
	id := a.runningID()
	if id == core.StoppedID {
		return fmt.Errorf("core is not running")
	}
	// Checked before the stop, so a node that has gone is not answered by stopping a
	// working core and then failing to bring anything back (see StartProfile's own
	// guard, which is the second half of the same contract).
	if a.curMgr() == nil || a.curMgr().SnapshotProfile(id) == nil {
		return fmt.Errorf("当前节点 %d 已不在列表中（可能已被订阅更新替换），请重新选择节点", id)
	}
	// 节点先行 left no mode guard here: a node running with no mode on is the
	// ordinary state of the new flow, and a restart of it is exactly that.
	// A restart is one request: the stop is a step in it, not a stop the reader
	// asked to stay stopped (see stopForSwitchLocked).
	if err := a.stopForSwitchLocked(); err != nil {
		return err
	}
	return a.startProfileLocked(id)
}

// StartLastProfile starts the remembered profile (or the first of the current group).
func (a *App) StartLastProfile() error {
	// A reload that failed leaves the stores unreachable; tray and autostart still
	// reach this binding, so it says "nothing to start" instead of panicking.
	if a.curSetting() == nil || a.curMgr() == nil {
		return fmt.Errorf("not initialised")
	}
	if a.curSetting().RememberID >= 0 && a.curMgr().GetProfile(a.curSetting().RememberID) != nil {
		return a.StartProfile(a.curSetting().RememberID)
	}
	ents := a.curMgr().ProfilesWithOrder(a.curSetting().CurrentGroup)
	if len(ents) > 0 {
		return a.StartProfile(ents[0].ID)
	}
	return fmt.Errorf("no profile to start")
}
