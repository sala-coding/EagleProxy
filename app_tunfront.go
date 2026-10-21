package main

import (
	"crypto/sha256"
	"eagle/internal/configbuilder"
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/tunfast"
	"eagle/internal/tunserv"
	"eagle/internal/util"
	"eagle/internal/vpn"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// tunFrontStartGrace is how long a freshly started front-end is watched before it is
// believed. RunElevated answers with a pid whether or not the core lives: a front-end
// launched while another one still holds EagleTun dies within a fraction of a second,
// leaving a pid that looks exactly like a healthy one. This is the window in which
// that death is still visible.
//
// It is not how long the front-end takes to come up — measured runs put that at 1.7s
// to 3.8s, mostly wintun creating the adapter and installing its routes, and waiting
// that long here would only delay the main core for no extra certainty. One second
// covers the failure it exists to catch, and the rest of the startup overlaps with the
// main core's own.
const tunFrontStartGrace = 1000 * time.Millisecond

// tunIdleReclaim is how long an adapter with no node behind it is kept before it is
// taken apart.
//
// An adapter is left idle in two situations that want the same answer: warmed up
// before the reader has picked a node, and released when TUN mode is switched off. In
// both, handing the machine back happens at once — routes gone, network restored — and
// the device stays, which is what makes switching TUN back on instant. That is worth
// keeping only while the reader might come back — three seconds, the span of one
// glance at the node list — and the device the next start needs is built again by
// the server's own warm-up, off the reader's path.
const tunIdleReclaim = 3 * time.Second

// tunRouteSoftWait extends the route-take wait for the one error that says the
// adapter is coming but not here yet — a first-boot wintun build whose on-link
// address lands late. Everything else answers within the plain 5s window.
const tunRouteSoftWait = 30 * time.Second

// tunRouteWait is how long a node start waits for the adapter to appear before giving
// up on capturing. The front-end builds the interface during its own start-up, which
// runs alongside the main core's rather than before it, so a start that has the core
// answering on its inbound can still be a moment ahead of the adapter — measured at
// about a second end to end. The ceiling is for the case where no adapter is coming at
// all, which is worth saying out loud rather than waiting on forever.
const tunRouteWait = 5 * time.Second

// startVPN dispatches to the tun implementation the user picked.
//
// NekoRay carries the same pair: "Internal Tun" puts the tun inbound into the
// main core (and elevates the whole app with it), while "External Tun" starts a
// second, elevated core that only owns the tun and hands traffic back to the
// main core's mixed inbound. The internal one is the default there and here —
// it has one core to reason about and no cross-process hop.
func (a *App) startVPN(id int) error {
	// The leftovers this used to clear here unconditionally — a broken wintun adapter
	// from a core killed part-way through creating it, and a front-end process nobody
	// is tracking — are now cleared from ensureTunFront, and only when there is a
	// record saying a previous run failed to tidy up. Doing it on every start meant two
	// PowerShell starts on the way to every TUN start, which was most of the wait.
	//
	// Tun2Socket is the only TUN path: the front-end holds the adapter, the main core
	// runs the plain config behind it. The old single-core shape (tun inbound inside the
	// main config) is gone — it could not survive a mode change without rebuilding the
	// adapter, which is the cost this split exists to avoid.
	return a.startVPNExternal(id)
}

// startVPNExternal runs the two-process TUN setup: a fixed, elevated front-end that
// holds the adapter and forwards everything to the main core's inbound, and the main core
// itself running a plain config behind it.
//
// The front-end is started only when it is not already running, and never touched
// afterwards — nodes, routing profiles and outbound modes all change on the main-core
// side, and each of those changes costs a core restart but no adapter. That asymmetry is
// the whole reason this process split exists.
func (a *App) startVPNExternal(id int) error {
	// The whole path below can wait for an administrator prompt, a warm-up and
	// the core's start-up sequence, and runningID only moves late in it. The
	// idle-reclaim timer that a previous stop armed fires inside that window
	// otherwise, and takes apart the front-end this start is about to use.
	a.tunWake.Store(true)
	defer a.tunWake.Store(false)
	a.cancelTunIdleReclaim()
	ent := a.curMgr().SnapshotProfile(id)
	if ent == nil {
		return fmt.Errorf("profile %d not found", id)
	}
	if a.curController() == nil {
		return fmt.Errorf("backend not initialised")
	}
	// The front-end and the main core start together.
	//
	// They do not need each other to boot: the front-end's one outbound names the
	// core's inbound port, and sing-box dials an outbound per connection rather than
	// at startup, so the core may be a moment behind. Starting the core only after
	// the front-end was up put an administrator prompt, an adapter open and a config
	// read in front of every start for nothing.
	frontCh := make(chan error, 1)
	go func() {
		// A panic here would take the process down with no reader-facing answer; the
		// call below is on a goroutine of its own for the first time, so it gets the
		// same courtesy every other long-lived goroutine in this app gets.
		defer func() {
			if r := recover(); r != nil {
				frontCh <- fmt.Errorf("TUN 前端启动失败: %v", r)
			}
		}()
		frontCh <- a.ensureTunFront()
	}()

	a.notifyMessage("正在启动 TUN 节点…")
	// The main core runs a plain config — mixed inbound, the node selector, the routing
	// rules. Nothing in it needs administrator rights; elevation belongs to the
	// front-end alone, which is the second half of the bargain.
	a.logf(fmt.Sprintf(">>> starting main core for [%s] %s (tun front-end starting alongside)", ent.DisplayType(), ent.DisplayName()))
	err := a.curController().Start(id)
	if err != nil {
		// The core is not coming up, so nothing should be captured: a front-end with
		// no core behind it is a socks port nobody listens on, and every packet
		// handed to it goes nowhere. The front-end may still be behind a prompt;
		// its answer is collected off this call so a declined prompt does not turn
		// into an orphaned adapter — and the adapter itself stays, because it is the
		// expensive half and the next start reuses it.
		go func() {
			if ferr := <-frontCh; ferr == nil {
				a.releaseTunRoutes()
				a.armTunIdleReclaim()
			}
		}()
		return err
	}
	if ferr := <-frontCh; ferr != nil {
		// The front-end did not come up — a declined prompt, a missing wintun.dll, a
		// broken adapter that could not be rebuilt. The core is up on a port nothing
		// points at; stopping it keeps "TUN failed" from leaving a core nobody asked
		// for, and the node-first flow can put it back with one press.
		// The locked form: this runs inside startProfileLocked's hold of
		// startStopMu, and the wrapper would wait on the lock this goroutine
		// already holds.
		_ = a.stopProfileLocked()
		return ferr
	}
	// The adapter has something behind it now, so the countdown that would have
	// taken an idle one apart is no longer wanted.
	a.cancelTunIdleReclaim()
	// Only now does the capture go up: the routes are what make every packet
	// take the adapter, and until this line there was no port behind it to
	// take them anywhere. This is also the step that used to happen inside the
	// core, at start-up, which put the machine behind a tunnel with nothing on
	// the far side of it for as long as the warm-up lasted.
	if err := a.takeTunRoutes(); err != nil {
		return a.handleTunCaptureFailure(err)
	}
	return nil
}

// startCapture brings the capture up around a core that is already running: the
// front-end that holds the adapter, and the routes that point the machine at it.
//
// It is the half of startVPNExternal that does not touch the main core, for the
// case where the core is already serving the config this mode needs (see
// coreServesCurrentMode). Then a switch into TUN is the capture, and the node
// behind it is never interrupted.
//
// The front-end comes up alongside the routes rather than before them: the wait
// for the adapter is inside takeTunRoutes, and the core behind it is already
// serving, so there is nothing on this side to wait for.
func (a *App) startCapture() error {
	// The whole path below can wait for an administrator prompt, a warm-up and
	// the adapter's own start-up, and the idle-reclaim timer that a previous
	// stop armed fires inside that window otherwise — and takes apart the
	// front-end this start is about to use.
	a.tunWake.Store(true)
	defer a.tunWake.Store(false)
	a.cancelTunIdleReclaim()
	frontCh := make(chan error, 1)
	go func() {
		// A panic here would take the process down with no reader-facing answer;
		// the call below is on a goroutine of its own for the first time, so it
		// gets the same courtesy every other long-lived goroutine in this app
		// gets.
		defer func() {
			if r := recover(); r != nil {
				frontCh <- fmt.Errorf("TUN 前端启动失败: %v", r)
			}
		}()
		frontCh <- a.ensureTunFront()
	}()
	if err := a.takeTunRoutes(); err != nil {
		// The front-end may still be behind a prompt; its answer is collected
		// off this call so a declined prompt does not turn into an orphaned
		// adapter — and the adapter itself stays, because it is the expensive
		// half and the next start reuses it.
		go func() {
			if ferr := <-frontCh; ferr == nil {
				a.releaseTunRoutes()
				a.armTunIdleReclaim()
			}
		}()
		return err
	}
	if ferr := <-frontCh; ferr != nil {
		// The front-end did not come up — a declined prompt, a missing
		// wintun.dll, a broken adapter that could not be rebuilt. The routes
		// are up over an adapter nobody serves, so they come back down.
		a.releaseTunRoutes()
		a.armTunIdleReclaim()
		return ferr
	}
	// The adapter has something behind it now, so the countdown that would have
	// taken an idle one apart is no longer wanted.
	a.cancelTunIdleReclaim()
	return nil
}

// ensureTunFront starts the standalone tun front-end when it is not already running, and
// does nothing when it is. That "does nothing" is the entire reason the process exists:
// it holds the adapter, so leaving it alone is what lets nodes, routing profiles and
// outbound modes change — and TUN mode itself be switched off and on — without the
// adapter being torn down and rebuilt under them.
//
// The config it runs is fixed (see configbuilder.BuildTunFront) and depends on exactly
// one thing — the main core's inbound port — so it is rewritten here on every start: a
// front-end that is not running cannot be reading the file, and one that is never
// reaches this function.
func (a *App) ensureTunFront() error {
	// One at a time. A mode switch and a node start can arrive together, and the loser
	// of that race would otherwise launch a second elevated core: it would find no pid
	// recorded yet — the winner is still sitting behind the UAC prompt — and conclude
	// that no front-end exists.
	a.tunFrontMu.Lock()
	defer a.tunFrontMu.Unlock()

	// The exit path does not wait for a front-end build, so a build that has not started
	// yet must not start: building an adapter for an app that is closing is how a
	// front-end ends up with nobody left to stop it.
	if a.quitting.Load() {
		return fmt.Errorf("程序正在退出")
	}
	if pid, _ := a.vpnState(); pid > 0 {
		if vpn.IsRunning(pid) {
			if !a.tunFrontConfigStale() {
				return nil
			}
			// The live front-end is carrying a config this app would no longer
			// write: the reader moved the local port, or turned the inbound
			// credential on. The socks hop is baked into the running process, so
			// reuse would be a tunnel into a port nobody listens on (or a hop
			// refused at every handshake) with the tray still saying TUN is on.
			// Rebuilding it costs the adapter, and a dead tunnel costs everything.
			a.logf("[vpn] TUN 前端配置已变更（本地端口或鉴权），重建前端")
			if err := a.stopTunFront(); err != nil {
				return err
			}
		} else {
			// The recorded front-end is gone. Drop it before anything else reads the
			// state: a pid left here pointing at nothing is what later makes a stop
			// believe it has nothing to do.
			a.setVPNState(0, 0)
		}
	}
	if a.curSetting() == nil {
		return fmt.Errorf("backend not initialised")
	}
	// The front-end's one outbound points at the main core's mixed inbound, so a
	// port the main core will not listen on (it is only built for a valid one, see
	// buildSingBox) leaves the front-end forwarding into nothing — the adapter up,
	// capturing packets, dropping every one of them. Refused here, where there is
	// still a reader to tell, rather than discovered as "TUN 开了但上不了网".
	if !util.IsValidPort(a.curSetting().InboundSocksPort) {
		return fmt.Errorf("本地入站端口 %d 不可用：TUN 前端就是往它转发，请先在设置里改成有效端口（1-65535）",
			a.curSetting().InboundSocksPort)
	}
	exe, err := core.ResolveCore(a.curCorePath(), a.curDataDir())
	if err != nil {
		return err
	}
	// the adapter is built by the wintun driver, and a missing DLL otherwise
	// surfaces as a process that dies immediately with a message about an
	// unnamed module
	if !core.WintunReady(exe, a.curDataDir()) {
		return fmt.Errorf("未找到 wintun.dll：TUN 模式需要它，请把 wintun.dll 放到 %s（或核心目录 %s）后重试",
			a.curDataDir(), filepath.Dir(exe))
	}
	// A record on disk means the last run never got to tidy up: it was killed, or it
	// crashed on the way out. Only then is the sweep worth what it costs — two
	// PowerShell starts, which is more than the front-end's own start in some runs. A
	// clean record means there is nothing out there to find.
	if a.tunFrontWasLeftBehind() {
		if a.sweepTunLeftovers() {
			// The record named a front-end that is alive and born from the very config
			// this build would write: it IS this start's front-end. Carrying on here
			// meant a second one behind a live adapter — the server refuses the start
			// and the reaper takes the adopted one down (twenty seconds of waiting for
			// nothing), and the legacy path pays a UAC for a process that dies on the
			// occupied adapter. The caller starts the main core and the capture; this
			// function's job is done.
			return nil
		}
	}
	a.notifyMessage("正在启动 TUN 网卡（需要管理员授权）…")
	path, err := a.writeTunFrontConfig()
	if err != nil {
		return err
	}
	// the front-end cannot be piped (UAC detaches it), so sing-box is told to
	// write its log here and the app tails the file into the panel.
	//
	// The previous run's log is kept, not deleted: a front-end that refuses to come
	// up writes its reason here (and on stderr, see tunserv.spawnChild), and clearing
	// the file on the next start erased the only evidence of that failure before
	// anyone could read it. Renaming can fail while the old process still holds the
	// file open — then it is simply kept where it is and appended to.
	coreLog := a.coreLogPath()
	_ = os.Rename(coreLog, coreLog+".prev")
	if pid, err := a.launchTunFront(exe, path); err != nil || pid > 0 {
		return err
	}
	// It came up and died. The likeliest cause is a leftover the sweep above had no
	// evidence of — an orphan from a build that never wrote the pid file, or an adapter
	// left broken while nothing was watching. One sweep and one more try, before this
	// is called a failure: it is also what lets the first launch after an upgrade clear
	// a front-end left by the version before it.
	a.logf("[vpn] TUN 前端未能启动，清理残留后重试")
	if a.sweepTunLeftovers() {
		// The clean-up found a live front-end after all, and adopted it: the launch this
		// path just watched die was the second one fighting it over the adapter.
		return nil
	}
	pid, err := a.launchTunFront(exe, path)
	if err != nil {
		return err
	}
	if pid <= 0 {
		msg := fmt.Sprintf("TUN 前端启动后立即退出，请查看 %s", filepath.Join(a.curDataDir(), "vpn-core.log"))
		a.notifyMessage(msg, true)
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// launchTunFront starts the elevated front-end and returns its pid once it has been
// confirmed alive, or 0 when it never came up. An error means the start itself failed —
// a declined UAC above all — which is the caller's to report.
func (a *App) launchTunFront(exe, cfgPath string) (int, error) {
	// The resident-server path first. What it buys over the ShellExecute below is the
	// whole point of the split: no UAC (the scheduled task carries the token), no
	// process-elevation round-trip, and a front-end whose life is independent of this
	// one — which is the shape karing keeps with its own service. Every failure here
	// falls through to the legacy path, so a missing or declined installation costs a
	// slower start, never a broken one.
	if a.ensureTunServer() {
		if pid, err := a.spawnFrontViaServer(exe, cfgPath); err == nil {
			return pid, nil
		} else {
			a.logf("[tunserv] server 启动失败，回落直启: " + err.Error())
			// The start that just timed out may still be sitting in the server's
			// command slot, and a server that reaches it later raises a second
			// front-end behind the one about to be started here — two processes
			// fighting over one adapter. The stop cancels that command, and takes
			// down the server's own child too, if it managed to raise one.
			//
			// Replace rather than queue, and this is the only place that wants it:
			// queueing (SendCommand's form) would deliver the start we have given up
			// on, raise a front-end, and then kill it — with this side's own
			// front-end starting in the middle of that, on the same adapter.
			_ = tunserv.ReplaceCommand(a.curDataDir(), tunserv.Command{Action: tunserv.ActionStop})
		}
	}
	pid, err := vpn.RunElevated(exe, []string{"run", "-c", cfgPath}, a.curDataDir())
	if err != nil {
		// a declined UAC lands here: TUN is off, which is honest
		a.notifyMessage("未启动 TUN 网卡（授权被拒绝或启动失败）", true)
		return 0, err
	}
	// Recorded before it is confirmed, so a stop arriving inside the confirmation
	// window can still reach it; cleared again below if it turns out to have died.
	a.setVPNState(pid, core.StoppedID)
	// RunElevated reports a pid whether or not the core lives, so being handed one is
	// not the same as having a front-end. Confirm it is still there before it is
	// written down: a dead pid recorded as a live one is how a later stop ends up
	// killing nothing while reporting success.
	if !vpn.AliveAfter(pid, tunFrontStartGrace) {
		a.setVPNState(0, 0)
		return 0, nil
	}
	// Written down before anything else decides what to do with it, because the record is
	// what lets a later launch recognise a front-end this one could not take back down.
	a.writeTunFrontPID(pid, cfgPath, false)
	if a.quitting.Load() {
		// The reader quit while the prompt was up, so this front-end outlived the reason
		// for it. The kill below needs elevation and will get no answer from a window that
		// is closing — which is exactly why the record above went down first.
		//
		// An error rather than a bare "nothing came up", so that the caller stops here:
		// ensureTunFront reads a zero pid as "died on the way up" and would sweep and try
		// again, raising a second prompt on the way out of a program that is closing.
		a.logf(fmt.Sprintf("<<< 程序正在退出，收回刚启动的 TUN 前端 (pid %d)", pid))
		_ = vpn.StopElevated(pid)
		return 0, fmt.Errorf("程序正在退出")
	}
	if keepOnExit := a.keepTunFront(); keepOnExit {
		// The whole point of the keep flag subtlety-free: the front-end survives this
		// process, and the next launch reads the pid file to adopt it — swapping a
		// dead-at-start adapter build for an already-running one, which is where the
		// seconds it used to spend go.
		a.logf(fmt.Sprintf("<<< 程序正在退出，保留 TUN 前端 (pid %d，下下次启动复用)", pid))
	}
	a.startTunFrontTail()
	go crashlog.Guard("watchTunFront", func() { a.watchTunFront(pid) })
	a.logf(fmt.Sprintf(">>> started tun front-end (pid %d)", pid))
	return pid, nil
}

// watchTunFront notices the front-end dying while TUN mode is still on.
//
// The gap this closes is real: the 08:37 run of 2026-10-14 shows it — the front-end
// raised, the main core followed, then the adapter open ran past wintun's own time
// budget ("open interface take too much time") and the front-end died with nobody
// told: no rollback had a pid to stop, the pid file kept the number, and TUN looked
// on — the main core serving an inbound with nothing capturing traffic.
//
// Death here means exactly one thing has changed: whoever killed the front-end was
// careful to update the bookkeeping first (mode switch off, node restart). So when
// the process goes without bookkeeping, TUN still claims lives it should not — and
// a single re-ensure restores both an adapter and the record, without touching a
// main core that is innocent. One retry, not a loop: an adapter the machine refuses
// twice is a fault worth a reader's eyes, not a countdown the machine handles.
// Death is declared only on consecutive misses: OpenProcess can fail once for
// reasons that have nothing to do with the process (a security product's hook,
// a transient system hiccup), and one such flicker must not be read as a death
// — the watcher below used to take a single miss as final, and one of its
// follow-up paths self-deadlocked on the mutex (see the note at vpnState),
// which silenced the traffic polling for the rest of the session with nothing
// in the log to say so.
// maxTunFrontRestarts / tunFrontRestartWindow bound the watcher's recoveries the way
// maxCoreRestarts bounds the supervisor's: three inside a minute, then the mode stands
// down. The window drains rather than resets, so a front-end that lives a while lets the
// count fall away on its own — same arithmetic as onCoreExit's restarts.
const (
	maxTunFrontRestarts   = 3
	tunFrontRestartWindow = time.Minute
)

// noteTunFrontRestart records a watcher recovery attempt and reports whether one more
// is allowed. Pruning happens here (sliding window), so a quiet front-end never trips it.
func (a *App) noteTunFrontRestart() bool {
	a.tunFrontRestartMu.Lock()
	defer a.tunFrontRestartMu.Unlock()
	now := time.Now()
	kept := a.tunFrontRestarts[:0]
	for _, t := range a.tunFrontRestarts {
		if now.Sub(t) < tunFrontRestartWindow {
			kept = append(kept, t)
		}
	}
	a.tunFrontRestarts = kept
	if len(kept) >= maxTunFrontRestarts {
		return false
	}
	a.tunFrontRestarts = append(kept, now)
	return true
}

// standDownTUN closes TUN mode from a background path — the watcher's recovery and the
// route retry's fallback — taking the switch's own seat first (startStopMu → modeMu,
// see the lock order there). Those paths used to flip the flag and hand the machine's
// routing around with no lock held while a switch could be mid-flight: the flag, the
// Windows routing table and the tray could each end up describing a different machine.
//
// cleanup runs under the seat, before the flag goes down — the routes and the pid record
// that belonged to the dead front-end. Nothing happens when the mode is already off:
// whoever turned it off owns the machine now, and saying otherwise would put a
// "已关闭 TUN" toast under a switch that is still in progress.
func (a *App) standDownTUN(cleanup func(), msg string) {
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	a.modeMu.Lock()
	defer a.modeMu.Unlock()
	st := a.curSetting()
	if st == nil || !st.VPNMode() {
		return
	}
	a.releaseTunRoutes()
	if cleanup != nil {
		cleanup()
	}
	st.SetVPNMode(false)
	a.rememberSpmode()
	a.refreshTrayStatus()
	a.notifyMessage(msg, true)
}

func (a *App) watchTunFront(pid int) {
	misses := 0
	// The resolver tick, and it starts due: a front-end that has just come up has an
	// adapter with no resolver of its own for as long as it takes the first pass to
	// run, and that window is the leak (see ensureTunResolvers).
	ticks := resolverTickTicks
	for {
		if vpn.IsRunning(pid) {
			misses = 0
			if a.quitting.Load() || a.tunStopping.Load() {
				return
			}
			if ticks++; ticks >= resolverTickTicks {
				ticks = 0
				a.ensureTunResolvers()
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		misses++
		if a.quitting.Load() || a.tunStopping.Load() {
			return
		}
		if misses >= 3 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if a.quitting.Load() || a.tunStopping.Load() {
		return
	}
	// vpnState takes the same mutex this path must not already hold: sync.Mutex
	// is not reentrant, and a Lock here followed by the read it wraps was a
	// self-deadlock that froze every later vpnState reader — the traffic poll
	// above all — for the rest of the session. Read through the accessor alone.
	cur, _ := a.vpnState()
	if cur != pid {
		// The record already moved on — a stop, a restart or a warm reclaim
		// replaced us; whoever owns the seat now will watch theirs.
		a.logf(fmt.Sprintf("[vpn] TUN 前端 (pid %d) 已退出，但记录已换主 (pid %d)，交由新的所有者处理", pid, cur))
		return
	}
	if a.curSetting() == nil || !a.curSetting().VPNMode() {
		// The mode itself is off; the warm reclaim owns whatever remains.
		a.logf(fmt.Sprintf("[vpn] TUN 前端 (pid %d) 已退出，TUN 模式已关闭，不再重启", pid))
		return
	}
	a.logf(fmt.Sprintf("[vpn] TUN 前端意外退出 (pid %d)，清理残留网卡后自动重启一次", pid))
	// A front-end that left without being asked to is the one case where the
	// adapter is certainly suspect: it died holding a wintun device, and that
	// device — abandoned, status not OK — is what turns the retry below into
	// the same failure a second time. Clearing it first is what makes "restart
	// once" mean a restart rather than a re-enactment.
	a.clearTunAdapterAfterStop()
	// The whole recovery is one recovery per session, not per watcher: every
	// successful ensure below spawns a fresh watcher with its own counter, and a
	// front-end that dies seconds after coming up turned "restart once" into a
	// permanent ~2.5s cleanup → ensure → death loop (see noteTunFrontRestart).
	if !a.noteTunFrontRestart() {
		a.standDownTUN(func() {
			a.setVPNState(0, 0)
			a.clearTunFrontPID()
		}, fmt.Sprintf("TUN 前端反复退出（%d 次），TUN 模式已关闭（当前直连），可重新开启", maxTunFrontRestarts))
		return
	}
	if err := a.ensureTunFront(); err != nil {
		// The flag still says TUN while nothing stands behind it: the switch reads
		// green over a machine that is plain direct, and the reader's next press
		// asks to turn off what is already off. Stand the mode down with the
		// adapter — the message's advice becomes the state, and the remembered
		// mode and the window (spmode:changed) follow the truth.
		//
		// The capture goes too, and so does the record: the process that held both
		// is gone, and the routes it installed do not leave with it. Without this
		// the message's "直接" was a description of the switch while every packet
		// still went into an adapter nobody was reading.
		//
		// Through standDownTUN: this runs on the watcher's goroutine with no lock
		// held, and the flag flip used to race a mode switch in flight for the
		// seat (see standDownTUN).
		a.standDownTUN(func() {
			a.setVPNState(0, 0)
			a.clearTunFrontPID()
		}, "TUN 前端异常退出且自动重启未成功，TUN 模式已关闭（当前直连），可重新开启")
		return
	}
	// The adapter is a new one, and a new adapter captures nothing: the routes went
	// with the device that died. The node itself never stopped, so if one is still
	// up the capture has to come back with the adapter — otherwise "recovered" would
	// describe a TUN mode that is on, healthy and carrying nothing at all.
	if a.runningID() != core.StoppedID {
		if err := a.takeTunRoutes(); err != nil {
			// Recovered the process but not the capture: the same decision the switch-on
			// path makes, made here as well (see handleTunCaptureFailure). The watcher
			// holds no switch lock, so the stand-down half has to take the seat itself
			// (see handleTunCaptureFailureFromWatcher) — the plain form would flip the
			// flag beside a switch that is still running.
			_ = a.handleTunCaptureFailureFromWatcher(err)
			return
		}
	}
	a.notifyMessage("TUN 前端异常退出后已自动恢复")
}

// The two waits around the resident server, both counted in the heartbeat's own units
// (ServerAlive declares a server dead after 12 seconds without a beat).
const (
	// serverExitWait is how long a server that has been asked to exit is given. It has
	// to cover the worst case it can be in — a start it is in the middle of, which the
	// start path itself allows twenty seconds for — because the alternative is what
	// this waited out wrongly before: a start posted to a server that had already
	// begun leaving, which is a command nobody reads.
	serverExitWait = 15 * time.Second
	// serverStartWait is how long a cold task start is given before the caller falls
	// back to launching the front-end itself.
	serverStartWait = 10 * time.Second
	// serverBornWait is how long a task start is given to produce any sign of a process
	// at all before it is read as one that started nothing. It is not a shorter version
	// of serverStartWait: a server that exists writes its own log line immediately, so
	// four seconds with neither a heartbeat nor a new line is evidence, not impatience —
	// and it is the difference between a TUN start that waits ten seconds for a task
	// pointing at a deleted executable and one that waits four.
	serverBornWait = 4 * time.Second
)

// resolverTickTicks is the watcher's resolver cadence: twenty passes of its 500ms
// sleep, which is the resident server's own ten seconds.
const resolverTickTicks = 20

// ensureTunResolvers keeps the adapter pointed at its own resolver, every ten seconds,
// for as long as a front-end is up.
//
// Only the resident server used to do this, and that made the machine's name resolution
// depend on a process nothing promises is there. A front-end started directly — the
// server was leaving, or its start timed out — and a front-end adopted from a previous
// run both come up with the adapter's resolver list EMPTY, and Windows then resolves
// through whatever the physical interface carries: in clear text, past the tunnel, and
// answered by the ISP. The reader meets it as "one browser works and the others do not":
// a browser carrying its own DoH (Brave) never asks the system, and every other one asks
// the machine, which asks the ISP.
//
// So this side asks too, on the same cadence, and asks the server for help when it
// cannot do it itself: the task is only ever RUN here, never created — a UAC prompt on a
// timer would be a worse fault than the one it repairs.
func (a *App) ensureTunResolvers() {
	// Only while a node is serving. The address written below is answered by the main
	// core's hijack, and with no core there is nothing behind it: an adapter pointed at
	// a peer that will not answer is a machine that cannot resolve a name at all —
	// worse than the leakage this exists to prevent. The idle state (front-end up, no
	// node) is the reader's own doing and the teardown's business, and the teardown
	// hands the resolver back: with the node gone the machine's names have to come
	// from somewhere, and the adapter is not it.
	name := configbuilder.TunAdapterName()
	serving := a.nodeServing()
	what := "写入"
	note := ""
	if serving {
		if err := tunfast.SelfDNS(name); err != nil {
			note = err.Error()
		}
	} else {
		what = "收回"
		// The elevated side does the same (see tunserv); this is the app's own best
		// effort, and it may not hold the rights to write an interface's resolver.
		if err := tunfast.ClearSelfDNS(name); err != nil {
			note = err.Error()
		}
	}
	a.vpnMu.Lock()
	changed := note != a.dnsNote
	a.dnsNote = note
	a.vpnMu.Unlock()
	if changed && note != "" {
		// Once per distinct failure, not once per pass: this runs forever, and this
		// process may simply not hold the token that writing an interface's resolver
		// needs — the server does.
		a.logf("[vpn] 网卡 DNS 未能" + what + "（交由常驻服务接管）: " + note)
	}
	if tunserv.ServerAlive(a.curDataDir()) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// Installed only. RunTask is a schtasks /Run, which the registering user's own
	// tasks accept without a prompt; creating one is the single step that needs UAC,
	// and ensureTunServer owns it (a launch, in the reader's own time).
	if sits, terr := tunserv.TaskInstalled(exe); terr == nil && sits {
		_ = tunserv.RunTask(exe)
	}
}

// ensureTunServer brings the resident server to life, at most one prompt ever.
//
// The chain is deliberately that boring: a heart on the wall answers "running";
// a registered task answers "one /Run away"; nothing else answers "create the
// task", which is the single step whose cost is a UAC prompt. Creation is
// attempted each time the server is absent, so a first run with the flag on
// asks once and every later run answers from the task registry. A declined
// prompt lands here as false, and the caller falls back to the ShellExecute
// path — which still works, slower and with its own prompt.
func (a *App) ensureTunServer() bool {
	if a.curDataDir() == "" {
		return false
	}
	if tunserv.ServerAlive(a.curDataDir()) {
		// A beating server answers every question below, including the one this process
		// asked earlier in the session and answered "not coming".
		a.tunServerGone.Store(false)
		if tunserv.ServerRunsSameBuild(a.curDataDir()) {
			return true
		}
		// A server from an older build is still running its old code — no adapter
		// preparation, no front-end sweep — and there is no way to make it run the
		// new one. Asking it to exit and starting it again is what keeps both
		// halves of this feature on one version; the cost is one /Run, and the
		// wait below is only as long as the server takes to notice.
		a.logf("[tunserv] 常驻服务来自旧版本，正在重启")
		_ = tunserv.SendCommand(a.curDataDir(), tunserv.Command{Action: tunserv.ActionExit})
		// Waited out against the heartbeat's own budget rather than a feeling about
		// how fast a process should be: the server reads its command slot between
		// pieces of work, and a start it is in the middle of can hold it for the
		// twenty seconds that start waits. Three seconds of patience — what this
		// allowed — is answered "it stayed" by a server that leaves ten seconds
		// later, and the start that follows then goes into the command slot of a
		// server already on its way out: unconsumed, twenty seconds of timeout, and
		// the direct launch after it, which knows nothing about the adapter's
		// resolver (see ensureTunResolvers).
		stopped := false
		exitDeadline := time.Now().Add(serverExitWait)
		for time.Now().Before(exitDeadline) {
			if !tunserv.ServerAlive(a.curDataDir()) {
				stopped = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !stopped {
			// Windows refuses a second instance of a running task, so a server
			// that will not leave is one this run keeps. Said out loud because the
			// alternative is a mystery: TUN working, but without the adapter
			// preparation and the sweep the app believes it has.
			a.logf("[tunserv] 旧版本常驻服务未退出，本次仍由它承担（重启程序或系统后生效）")
		}
	}
	// One probe at a time, and one per session. Ten seconds out of a reader's TUN switch
	// is worth paying once to learn whether the task starts at all; a second caller — the
	// resolver reconcile calls this too — has nothing to add to a probe already in flight,
	// and a session that has already watched the task not start knows what the next ten
	// seconds would say. Both shapes are in the log as identical detours a second apart,
	// in front of an adapter that is otherwise ready in under half a second.
	if a.tunServerGone.Load() || !a.tunServerProbe.CompareAndSwap(false, true) {
		return false
	}
	defer a.tunServerProbe.Store(false)
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// The log's stamp before the task is asked to run: the wait below compares against it
	// to tell a server that is starting from one that was never created.
	logBefore := tunserv.ServerLogStamp(a.logDir())
	installed, terr := tunserv.TaskInstalled(exe)
	switch {
	case terr == nil && installed:
		_ = tunserv.RunTask(exe)
	case terr != nil:
		// Reading the task list is the sort of thing Windows refuses randomly
		// (a locked file, a busy service); fall through and create, whose own
		// error path will say more if that fails too.
		fallthrough
	default:
		// The ELEVATED create (waited: schtasks' own verdict decides), then the
		// registry is re-read to be sure — the one pairing that teaches "the
		// prompt was answered" apart from "the task actually exists", the pair
		// the first cut of this function kept confused.
		if err := tunserv.InstallTask(exe, func(exe string, args []string, dir string) (int, error) {
			return vpn.RunElevatedWait(exe, args, dir)
		}); err != nil {
			a.logf("[tunserv] 任务安装未完成: " + err.Error())
			return false
		}
		if sits, terr := tunserv.TaskInstalled(exe); terr != nil || !sits {
			a.logf("[tunserv] 安装后未查到任务，回落直启")
			return false
		}
		a.logf("[tunserv] 计划任务已安装（本次授权一次，后续 TUN 启动不再弹 UAC）")
		_ = tunserv.RunTask(exe)
	}
	// Ten seconds, not four: this is a scheduled task starting cold — a process create
	// plus a task-scheduler round trip — and the four seconds this allowed were
	// answered "not ready" by servers that were merely still starting. Falling back
	// early is not free: the direct launch that follows is the one shape that has to
	// apply the adapter's resolver itself (see ensureTunResolvers).
	// The wait is cut short by evidence rather than by a shorter guess: a server that
	// exists writes its first log line the moment the process is there, so a start that
	// has produced neither a heartbeat nor a new line by serverBornWait never created a
	// process at all — the shape a /Run produces when the task points at an executable
	// that has moved or been replaced, where schtasks still answers success. The remaining
	// six seconds would say the same thing.
	startedAt := time.Now()
	bornDeadline := startedAt.Add(serverBornWait)
	readyDeadline := startedAt.Add(serverStartWait)
	for time.Now().Before(readyDeadline) {
		if tunserv.ServerAlive(a.curDataDir()) {
			a.logf("[tunserv] 常驻服务已就绪")
			return true
		}
		if time.Now().After(bornDeadline) && !tunserv.ServerLogStamp(a.logDir()).After(logBefore) {
			a.logf("[tunserv] 常驻服务进程未出现（任务可能指向已不存在的程序），本次使用直启")
			a.tunServerGone.Store(true)
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.logf("[tunserv] 常驻服务未在 10 秒内就绪，本次使用直启（网卡 DNS 由本进程接管）")
	// Remembered for the rest of the session: the task may point at an executable that
	// no longer exists (the folder moved, an update replaced it) and Windows answers a
	// /Run for such a task with success while starting nothing. Paying the wait again at
	// every TUN start buys no new information and costs the reader the adapter's whole
	// head start (see prewarmTunServer, which clears this when a server does come up).
	a.tunServerGone.Store(true)
	return false
}

// prewarmTunServer brings the resident server up while the reader is still finding
// their node, so that the first TUN start of the session does not pay the cold
// task start (up to serverStartWait of waiting for a heartbeat that has not begun).
//
// The quiet half is the whole design: a task that is installed is Run without a
// prompt, and one that is not is left alone — creating it is the single step that
// costs a UAC prompt, and a prompt at startup, before the reader has asked for
// anything, is a worse fault than the seconds this exists to save. The first
// enable still owns that prompt, exactly as before.
//
// An old-build server is restarted rather than kept: the same exit-and-rerun
// ensureTunServer would do at TUN start, moved off the reader's path. Both waits
// are bounded by the same budgets that path uses, and every failure here is a
// line in the log — the enable that follows retries the whole thing.
func (a *App) prewarmTunServer() {
	if a.curDataDir() == "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if sits, terr := tunserv.TaskInstalled(exe); terr != nil || !sits {
		return
	}
	if tunserv.ServerAlive(a.curDataDir()) {
		a.tunServerGone.Store(false)
		if tunserv.ServerRunsSameBuild(a.curDataDir()) {
			a.logf("[tunserv] 启动预热: 常驻服务已在运行")
			return
		}
		a.logf("[tunserv] 启动预热: 常驻服务来自旧版本，正在重启")
		_ = tunserv.SendCommand(a.curDataDir(), tunserv.Command{Action: tunserv.ActionExit})
		exitDeadline := time.Now().Add(serverExitWait)
		for time.Now().Before(exitDeadline) {
			if !tunserv.ServerAlive(a.curDataDir()) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	_ = tunserv.RunTask(exe)
	readyDeadline := time.Now().Add(serverStartWait)
	for time.Now().Before(readyDeadline) {
		if tunserv.ServerAlive(a.curDataDir()) {
			a.tunServerGone.Store(false)
			a.logf("[tunserv] 启动预热: 常驻服务已就绪，后续 TUN 启动免冷等待")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.logf("[tunserv] 启动预热: 常驻服务未就绪，TUN 启动时再试")
}

// spawnFrontViaServer hands the start to the resident server and waits for the
// child it raised. The pid book is the receipt: the server writes it with the
// config's own mtime, so a stale record is told from a fresh one by the file it
// points at rather than by hope.
func (a *App) spawnFrontViaServer(exe, cfgPath string) (int, error) {
	// The receipt has to be newer than this send. The book may name a front-end
	// this very start is about to replace — an adopted one above all — and
	// accepting it means reporting a start by the life of a process the start
	// then kills, which reads as "died immediately" while the real child is
	// still on its way.
	sent := time.Now()
	if err := tunserv.SendCommand(a.curDataDir(), tunserv.Command{
		Action: tunserv.ActionStart,
		Core:   exe,
		Config: cfgPath,
	}); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if a.quitting.Load() {
			return 0, fmt.Errorf("程序正在退出")
		}
		info := tunserv.FrontPIDOf(a.curDataDir())
		if info.FreshFrom(sent) && vpn.IsRunning(info.PID) {
			a.setVPNState(info.PID, core.StoppedID)
			if !vpn.AliveAfter(info.PID, tunFrontStartGrace) {
				// The child can still die on its first seconds (the adapter open
				// that once ran past its budget); a front-end that left this
				// early is a dead start, not a started front-end.
				//
				// The pid record stays, and that is the server's own contract: a
				// front-end that died this way left the adapter behind, and the
				// record is the only evidence the next start has to sweep for it.
				// Clearing it here skipped the repair and left a device every later
				// start failed to open (see server_windows.go's start branch).
				a.setVPNState(0, 0)
				return 0, fmt.Errorf("tunserv: 前端启动后立即退出")
			}
			tunserv.WriteFrontPID(a.curDataDir(), *info)
			if a.quitting.Load() && !a.keepTunFront() {
				a.logf(fmt.Sprintf("[tunserv] 程序正在退出，收回刚启动的前端 (pid %d)", info.PID))
				_ = a.stopTunFrontViaServer(info.PID)
				return 0, fmt.Errorf("程序正在退出")
			}
			a.startTunFrontTail()
			go crashlog.Guard("watchTunFront", func() { a.watchTunFront(info.PID) })
			a.logf(fmt.Sprintf(">>> started tun front-end via server (pid %d)", info.PID))
			return info.PID, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, fmt.Errorf("tunserv: 前端未在 20 秒内就绪")
}

// stay surprises out of stop paths and keep paths both: one bool, read at the
// moments that decide whether the front-end dies with the app.
func (a *App) keepTunFront() bool {
	if a.updating.Load() {
		// An update is the one exit that means the build is changing: the front-end
		// runs from the same executable being replaced, and Windows will not let a
		// running image be overwritten. Kept is for the next launch of this build.
		return false
	}
	// Worth keeping only while TUN mode is on. An adapter that survives for a mode
	// that is off is a network connection the reader cannot account for, and the
	// seconds it saves are seconds nobody is about to spend — the idle countdown
	// would take it apart a minute later anyway.
	return a.curSetting() != nil && a.curSetting().KeepTunFrontOnExit && a.curSetting().VPNMode()
}

// startTunFrontTail streams the front-end's log file into the panel. Shared by
// both launch paths and adoption, because the reader sees one stream either
// way.
func (a *App) startTunFrontTail() {
	// A previous tail may still be draining the same log file (an adopted
	// front-end that died and was re-raised, a retried spawn); its channel gets
	// closed here, or the old goroutine would keep reading -- and emit every
	// log line twice -- for as long as the app lives.
	a.vpnMu.Lock()
	old := a.vpnTailStop
	stopTail := make(chan struct{})
	a.vpnTailStop = stopTail
	if old != nil {
		// closed inside the hold so a concurrent stopTunFront can never close
		// the same channel twice
		close(old)
	}
	a.vpnMu.Unlock()
	go crashlog.Guard("tailVPNLog", func() { a.tailVPNLog(a.coreLogPath(), stopTail) })
}

// stopTunFrontViaServer posts stop and waits for the child to be reaped, with
// its own ceiling. It is used by exactly one reach: a quit that must not raise
// a prompt, and a server-owned front whose death is certain (the server holds
// the token) — so on a timeout the caller has something to log, not a mystery.
func (a *App) stopTunFrontViaServer(pid int) error {
	if err := tunserv.SendCommand(a.curDataDir(), tunserv.Command{Action: tunserv.ActionStop}); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		info := tunserv.FrontPIDOf(a.curDataDir())
		if info == nil || info.PID != pid || !vpn.IsRunning(info.PID) {
			a.setVPNState(0, 0)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("tunserv: stop 未在时限内完成")
}

// armTunIdleReclaim starts the countdown that takes an idle adapter apart.
func (a *App) armTunIdleReclaim() {
	a.tunIdleMu.Lock()
	defer a.tunIdleMu.Unlock()
	if a.tunIdleTimer != nil {
		a.tunIdleTimer.Stop()
	}
	// The callback runs on a timer goroutine of its own, outside every Guard the
	// start paths wrap: a panic in the teardown it drives would otherwise take the
	// whole process with it and leave nothing written down.
	a.tunIdleTimer = time.AfterFunc(tunIdleReclaim, func() {
		crashlog.Guard("reclaimIdleTunFront", a.reclaimIdleTunFront)
	})
}

// cancelTunIdleReclaim stops the countdown: something wants the adapter again.
func (a *App) cancelTunIdleReclaim() {
	a.tunIdleMu.Lock()
	defer a.tunIdleMu.Unlock()
	if a.tunIdleTimer != nil {
		a.tunIdleTimer.Stop()
		a.tunIdleTimer = nil
	}
}

// reclaimIdleTunFront takes an adapter apart once it has been idle long enough that
// nobody is coming back for it.
//
// What it finds is re-checked rather than assumed, because the answer can change while
// the countdown runs: a node that started in the meantime means the adapter is in use,
// and taking apart one the reader just started a node on would be far worse than
// leaving an idle one. stopTunFront is the whole teardown — front-end killed, device
// cleared, record dropped — the same path a mode switch used to take.
func (a *App) reclaimIdleTunFront() {
	a.tunIdleMu.Lock()
	a.tunIdleTimer = nil
	a.tunIdleMu.Unlock()

	if a.quitting.Load() {
		return
	}
	if a.tunWake.Load() {
		// A node start is somewhere between the UAC prompt and setting runningID.
		// Its own exit path cancels the countdown (success) or arms it again
		// (failure), so the idle teardown simply stands down here.
		return
	}
	if a.runningID() != core.StoppedID {
		return
	}
	if pid, _ := a.vpnState(); pid <= 0 {
		return
	}
	a.logf("<<< TUN 前端闲置超时（无节点在使用），前端进程退出；网卡保留，下次开启 TUN 秒开")
	_ = a.stopTunFront()
}

// tunFrontConfigPath is the fixed config the elevated front-end runs. It is both what
// the front-end is started with and the only thing in a command line that says a
// process is ours — see reapOrphanTunFronts. Empty when there is no data directory,
// which callers read as "nothing to match on".
func (a *App) tunFrontConfigPath() string {
	if a.curDataDir() == "" {
		return ""
	}
	return filepath.Join(a.curDataDir(), "temp", "tun-front.json")
}

// reapOrphanTunFronts ends every front-end still running on this app's front-end
// config, keeping keep when it is the one that should live on, and reports how many
// it ended.
//
// The pid the app hands around is a claim, not a fact. It is held in memory, so a
// restart forgets it; RunElevated can report a core that died on its first config
// line; and a kill that never reached shutdown leaves a live front-end nobody is
// tracking. The config path survives all three, because it is in the process's own
// command line.
func (a *App) reapOrphanTunFronts(keep int) (int, error) {
	cfg := a.tunFrontConfigPath()
	if cfg == "" {
		return 0, nil
	}
	return vpn.StopOrphanTunFronts(cfg, keep)
}

// tunFrontPIDPath is where the running front-end's pid is written down. Unlike the
// copy in memory it outlives the process, so a run that never got to tidy up can be
// recognised by the next one.
func (a *App) tunFrontPIDPath() string {
	if a.curDataDir() == "" {
		return ""
	}
	return filepath.Join(a.curDataDir(), "temp", "tun-front.pid")
}

// writeTunFrontPID records a front-end that is now up. The record inks the
// config file's content hash at spawn time — the only observable that tells a
// later run "this front-end was born from the config I would write myself",
// immune to the rewrite every TUN enable performs on the very same file — and
// the server flag, which changes whose book the next stop belongs to.
func (a *App) writeTunFrontPID(pid int, cfgPath string, server bool) {
	path := a.tunFrontPIDPath()
	if path == "" || pid <= 0 {
		return
	}
	hash := ""
	if cfgPath != "" {
		if data, err := os.ReadFile(cfgPath); err == nil {
			sum := sha256.Sum256(data)
			hash = hex.EncodeToString(sum[:])
		}
	}
	if err := tunserv.WriteFrontPID(a.curDataDir(), tunserv.FrontPID{
		PID: pid, Server: server, Config: cfgPath, ConfigHash: hash,
	}); err != nil && server {
		// Server-owned children are cleared by the server itself; a missing
		// write from app side stays harmless there. Legacy children keep the
		// old semantics: the record that remains is the sweep's only friend.
		a.logf("[tunserv] pid 写入失败: " + err.Error())
	}
}

// readTunFrontPIDInfo reads what the file says, cross-format.
func (a *App) readTunFrontPIDInfo() tunserv.FrontPID {
	info := tunserv.FrontPIDOf(a.curDataDir())
	if info == nil {
		return tunserv.FrontPID{}
	}
	return *info
}

// clearTunFrontPID drops the record. Only called once the front-end behind it is
// confirmed gone: the file is the one thing that tells a later run there may still be
// something out there holding the adapter, and clearing it early would throw that away.
func (a *App) clearTunFrontPID() {
	if path := a.tunFrontPIDPath(); path != "" {
		_ = os.Remove(path)
	}
}

// tunFrontWasLeftBehind reports whether a previous run wrote down a front-end and never
// cleared it — which is exactly the case where something may still be holding EagleTun.
func (a *App) tunFrontWasLeftBehind() bool {
	path := a.tunFrontPIDPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// sweepTunLeftovers clears what an earlier run can leave behind on the adapter: a
// front-end process nobody is tracking, and a wintun adapter left broken by one that
// died part-way through creating it.
//
// Both cost a PowerShell start, which is most of what a TUN start used to spend doing
// nothing at all, so this runs on evidence rather than on every start — see
// tunFrontWasLeftBehind, and the retry in ensureTunFront for the one case evidence
// cannot cover.
//
// Adoption comes before the sweep now, because the keep flag turned "a front-end
// left behind" from a problem into an asset: the record carries the config's mtime,
// so a surviving front-end is only adopted when the config on disk is the very one
// it was born from. Anything else — a mismatched config, a dead process — goes to
// the sweep exactly as before.
// It answers whether it adopted one: an adopted front-end is this start's front-end, and
// the caller has to stop there rather than launch a second one over a live adapter.
func (a *App) sweepTunLeftovers() bool {
	if info := a.readTunFrontPIDInfo(); info.PID > 0 && vpn.IsRunning(info.PID) {
		if a.adoptTunFront(info) {
			return true
		}
	}
	if n, err := a.reapOrphanTunFronts(0); err != nil {
		a.logf("[vpn] " + err.Error())
	} else {
		if n > 0 {
			a.logf(fmt.Sprintf("<<< 已结束 %d 个残留的 TUN 前端进程", n))
		}
		a.clearTunFrontPID()
	}
	if n, err := vpn.CleanStaleTunAdapters(); err != nil {
		a.logf("[vpn] " + err.Error())
	} else if n > 0 {
		a.logf(fmt.Sprintf("<<< 已清理 %d 个异常的 TUN 网卡", n))
	}
	return false
}

// adoptTunFront reconnects the app's books to a front-end that survived the
// previous run, answering whether the adoption was right or the sweep must go
// on. Two guards decide "yes": the keep flag is on, and the config the pid
// record was born from is content-identical to what is on disk (sha256) — a
// stale front-end whose config has moved on is a trap, not an asset, because
// rerouting through a tun inbound built for other MTUs/ports reads fine and
// behaves wrong.
//
// A config mismatch is answered, where the server is the owner, by a stop the
// server can carry out without a prompt; the flow then falls back to the sweep,
// and the very next ensure raises a fresh front-end from the current config.
func (a *App) adoptTunFront(info tunserv.FrontPID) bool {
	if !a.keepTunFront() {
		return false
	}
	cfgPath := a.tunFrontConfigPath()
	current := ""
	if cfgPath != "" {
		if data, err := os.ReadFile(cfgPath); err == nil {
			sum := sha256.Sum256(data)
			current = hex.EncodeToString(sum[:])
		}
	}
	if current == "" || info.ConfigHash == "" || info.ConfigHash != current {
		// Mismatch. The server case is answered here, prompt-free; the legacy
		// case falls to the sweep, whose answer has always been a stop.
		if info.Server {
			a.logf(fmt.Sprintf("[tunserv] 复用前先停掉旧配置的前端 (pid %d)", info.PID))
			_ = tunserv.SendCommand(a.curDataDir(), tunserv.Command{Action: tunserv.ActionStop})
			time.Sleep(600 * time.Millisecond)
		}
		return false
	}
	a.setVPNState(info.PID, core.StoppedID)
	a.startTunFrontTail()
	// The adopted process dies like any other, and without a watcher nothing
	// would notice: the books would keep a dead pid, TUN would sit showing
	// "on" with no capture, and only a stop or the idle reclaim would find it.
	go crashlog.Guard("watchTunFront", func() { a.watchTunFront(info.PID) })
	a.logf(fmt.Sprintf(">>> adopted running tun front-end (pid %d)", info.PID))
	return true
}

// writeTunFrontConfig renders the front-end's fixed configuration and writes it, adding
// the log destination the app tails.
func (a *App) writeTunFrontConfig() (string, error) {
	// The MTU in the config is not always the one in the settings: a value below IPv6's
	// floor cannot be given to an interface carrying an IPv6 address, and the core would
	// refuse to build the adapter at all (see configbuilder.TunMTU). Said out loud here
	// rather than adjusted in silence, because the log panel is where a reader looks when
	// TUN behaves unlike the settings say.
	if mtu := configbuilder.TunMTU(a.curSetting()); mtu != a.curSetting().VPNMTU {
		a.logf(fmt.Sprintf("提示: 已启用 IPv6，VPN MTU 由 %d 提升为 %d（IPv6 要求网卡 MTU 不小于 %d）",
			a.curSetting().VPNMTU, mtu, mtu))
	}
	cfg := a.tunFrontConfig()
	path := a.tunFrontConfigPath()
	if path == "" {
		return "", fmt.Errorf("data directory is not set")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// tunFrontConfig is the front-end config as this app would write it now: the
// builder's output plus the two things this side adds (the MTU guard, and where the
// front-end's log goes). One function, so that the file on disk, the process that
// read it and the staleness check below cannot end up disagreeing about what "the
// config" is.
func (a *App) tunFrontConfig() map[string]interface{} {
	cfg := configbuilder.BuildTunFront(a.curSetting())
	// Guard the MTU against the worst case where the builder and the setting
	// disagree: a tun inbound carrying an IPv6 address cannot use less than 1280.
	if inb, ok := cfg["inbounds"].([]interface{}); ok && len(inb) > 0 {
		if m, ok := inb[0].(map[string]interface{}); ok {
			m["mtu"] = configbuilder.TunMTU(a.curSetting())
		}
	}
	cfg["log"] = map[string]interface{}{
		"level":     a.curSetting().LogLevel,
		"timestamp": true,
		"output":    a.coreLogPath(),
	}
	return cfg
}

// coreLogPath is where the core writes its own log: the log folder, beside the
// categorised logs and the crash record.
//
// One place, because three of them named it and a fourth that did not would be a
// log in a folder of its own — which is the thing this arrangement is for.
func (a *App) coreLogPath() string {
	return filepath.Join(a.logDir(), "vpn-core.log")
}

// tunFrontConfigStale reports whether the front-end living right now was started
// with a different config from the one this app would write now. The file is the
// record of what that process read — it is rewritten on every launch, and nothing
// else writes it — so a difference between the two is exactly "the reader changed
// something the front-end carries": the port its socks hop points at, or the
// credential on that hop.
//
// Both sides are compared as parsed JSON rather than as bytes: the file is written
// indented and this side builds maps, and a byte comparison would call every config
// stale and rebuild the adapter on every node start.
func (a *App) tunFrontConfigStale() bool {
	if a.curSetting() == nil || a.curDataDir() == "" {
		return false
	}
	raw, err := os.ReadFile(a.tunFrontConfigPath())
	if err != nil {
		// Nothing on disk: no evidence either way, and the launch path writes the
		// file before it starts anything.
		return false
	}
	var have map[string]interface{}
	if json.Unmarshal(raw, &have) != nil {
		// A file that will not parse is not a config anybody can reuse.
		return true
	}
	data, err := json.Marshal(a.tunFrontConfig())
	if err != nil {
		return false
	}
	var want map[string]interface{}
	if json.Unmarshal(data, &want) != nil {
		return false
	}
	return !reflect.DeepEqual(want, have)
}

// stopTunFront brings the tun front-end down, adapter and all — the expensive ending,
// and now a rare one.
//
// It runs on the paths that leave no capture behind and nothing using the adapter: the
// core giving up (the crash path), the app going away with keep_tun_front off, the
// program being replaced, the front-end's own config going stale, and the two mode
// switches when the config moved and the core had to be rebuilt with it.
//
// It does NOT run when a mode switch leaves the node alone: the front-end holds the
// adapter and nothing else, so an idle one costs a device that is already there and
// makes the next switch back to TUN a matter of taking the routes again.
func (a *App) stopTunFront() error {
	// Whatever brought this front-end up, no countdown should outlive it.
	a.cancelTunIdleReclaim()
	// Told to the death watcher before the kill: this teardown is ours. Without the
	// mark, a kill that races the 500ms poll window would read as an accident to the
	// watcher, which would re-ensure while this teardown is mid-flight — a second
	// prompt behind the first.
	a.tunStopping.Store(true)
	defer a.tunStopping.Store(false)
	pid, _ := a.vpnState()
	if pid <= 0 {
		// Nothing in the books, but the disk can still say a front-end was born
		// here — the shape after a restart, or after a run that died on its way
		// out. That record is the only evidence that a broken adapter may be
		// waiting for the next start, so it is read before "nothing to stop".
		if a.tunFrontWasLeftBehind() {
			a.sweepTunLeftovers()
		}
		return nil
	}
	a.vpnMu.Lock()
	if chanStop := a.vpnTailStop; chanStop != nil {
		a.vpnTailStop = nil
		close(chanStop) // closed inside the hold: no double close against startTunFrontTail
	}
	a.vpnMu.Unlock()
	// The server's hand first, when it is on: no UAC, no taskkill, and the child
	// is gone because the resident token takes it down without asking anyone.
	// A timeout falls through to the legacy kill below, so a wedged server can
	// never trap a front-end nobody can stop.
	if tunserv.ServerAlive(a.curDataDir()) {
		if err := a.stopTunFrontViaServer(pid); err == nil {
			// Guarded: a concurrent ensureTunFront may have recorded a new
			// front-end while the stop ran — its record is not ours to erase.
			a.clearFrontRecordIfMine(pid)
			// And the resident server goes with its child. The loop keeps the
			// adapter alive on a ten-second tick (ensureAdapter, internal/tunserv),
			// so a teardown that left the server running was undone before this
			// line finished printing — "拆下网卡" in the log, the device back a
			// breath later, in every mode the switch just left. The next TUN start
			// answers from the task registry: one /Run, no prompt (ensureTunServer
			// already re-runs the task after its own exit-and-restart for a stale
			// build, so this is the same road, walked one time earlier).
			_ = tunserv.SendCommand(a.curDataDir(), tunserv.Command{Action: tunserv.ActionExit})
			a.logf("<<< stopped tun front-end (server)")
			return nil
		}
		a.logf("[tunserv] server stop 超时，改走直杀")
	}
	if err := vpn.StopElevated(pid); err != nil {
		a.logf("[vpn] front-end stop failed (可能需要管理员权限): " + err.Error())
		// The pid stays put. A front-end that could not be killed is still the one
		// holding the adapter, and clearing it here is what used to make it
		// unreachable for the rest of the run. Before giving up, sweep by config
		// path: the process holding EagleTun may not be this pid at all.
		if n, sErr := a.reapOrphanTunFronts(0); sErr == nil && n > 0 {
			a.clearFrontRecordIfMine(pid)
			a.logf(fmt.Sprintf("<<< 已结束 %d 个残留的 TUN 前端进程", n))
			return nil
		}
		// The record stays: it is the only thing that will tell the next run there may
		// still be something out there holding the adapter.
		return err
	}
	a.clearFrontRecordIfMine(pid)
	a.logf("<<< stopped tun front-end")
	return nil
}

// clearTunAdapterAfterStop clears whatever the front-end left on the adapter.
//
// The front-end is killed rather than asked to leave — taskkill /F is the only
// stop that works on a core that has wedged — and a wintun device abandoned that
// way comes back as a broken adapter that makes the NEXT start fail outright:
//
//	create adapter: Cannot create a file when that file already exists.
//	open existing adapter: Element not found.
//
// That is a permanent failure, not a slow one: every later start dies on it, and
// nothing in the app clears the device on its own once the pid record is gone.
// So the stop pays for the sweep instead of the start. Closing TUN costs one
// PowerShell round trip nobody is timing; the same call on the way up sat in
// front of every connect, which is why it was moved off that path in the first
// place. Only adapters whose status is not OK are removed, so a healthy adapter
// another VPN is using is left alone.
func (a *App) clearTunAdapterAfterStop() {
	n, err := vpn.CleanStaleTunAdapters()
	if err != nil {
		a.logf("[vpn] " + err.Error())
		return
	}
	if n > 0 {
		a.logf(fmt.Sprintf("<<< 已清理 %d 个异常的 TUN 网卡", n))
	}
}

// takeTunRoutes puts the adapter back in charge of the machine's default route;
// releaseTunRoutes hands the machine back to its physical adapter. Together they
// are the whole cost of switching TUN mode on and off.
//
// The adapter itself is not theirs to touch: it belongs to the front-end core,
// which keeps it across both switches, so neither direction pays for a PnP
// create or remove — the seconds Windows spends on those, and the reason a TUN
// switch used to read as "building something" rather than as a switch. What
// moves is two /1 routes and an interface metric per family (see
// internal/tunfast), which is microseconds.
//
// The ordering is the app's to get right, and both directions have one. Taking
// runs after the main core is up, so the capture never points at a socks port
// with nobody behind it. Releasing runs as soon as the main core stops: a tunnel
// with nothing to forward through is a machine with no internet, and handing the
// routes back is what ends that.
// It answers an error when the capture never went up, and the caller has to treat that as
// "TUN is not in force": a green switch over a direct connection is the one state this
// function's silence used to produce (see the failure note at the bottom).
// tunRouteRetries and tunRouteRetryInterval are the backstop for a capture that was not up
// inside the start's own window. The front-end may still be configuring the adapter — a
// first wintun build has been seen to take well over half a minute ("open interface take
// too much time to finish") — and closing TUN for that would throw away a tunnel that was
// about to carry traffic.
const (
	tunRouteRetries       = 30
	tunRouteRetryInterval = time.Second
	// The first attempts come in a tight burst. A retry that answers "not yet"
	// is usually the adapter's address still being assigned — done in tens of
	// milliseconds once it actually starts — and a whole second between asks
	// turned a nearly-ready adapter into a nearly-ready capture for up to one
	// more second than the work took. The burst covers that class; the slow
	// interval behind it keeps the long tail (a first wintun build measured in
	// tens of seconds) from burning CPU asking the same question.
	tunRouteFastRetries  = 6
	tunRouteFastInterval = 150 * time.Millisecond
)

// tryTakeTunRoutes makes one attempt: find the adapter, take the default route, and cancel
// the idle countdown when it worked. The waiting loop below is built on it, and so is the
// background retry (see retryTunRoutes).
func (a *App) tryTakeTunRoutes() error {
	link, err := a.tunLink()
	if err != nil {
		return err
	}
	if err := link.Take(); err != nil {
		return err
	}
	a.cancelTunIdleReclaim()
	return nil
}

func (a *App) takeTunRoutes() error {
	// The adapter is built by the front-end's own start-up, which overlaps with the
	// main core's rather than preceding it — by the time the core answers on its
	// inbound, the interface can still be a moment away. Waiting for it is what keeps
	// a switch-on from silently capturing nothing, and the wait is bounded by the
	// front-end's own start: about a second, with the ceiling below as the answer for
	// a front-end that never gets there.
	deadline := time.Now().Add(tunRouteWait)
	// The adapter exists the moment the front-end raises it, but Windows assigns
	// the on-link address on its own clock — a first boot's wintun creation has
	// been slow enough (seen live: the interface answered for thirty seconds)
	// that five seconds knew only "no address to route", gave up, and left the
	// switch reading as on with nothing captured — until the front-end's own
	// tun inbound timed the same slowness out and self-destructed into the
	// auto-recovery smear that cleaned up after it. The soft window extends the
	// wait for exactly that class of answer; everything else keeps the short one.
	softDeadline := time.Now().Add(tunRouteSoftWait)
	var lastErr error
	for {
		// The core has to be able to answer before the machine's packets are pointed
		// at it: the front-end hands everything it receives to the core's own inbound,
		// and a capture taken before that inbound answers is a machine whose traffic
		// goes nowhere. The core is a second and a half from opening it after a
		// switch, and the front-end is already up by then — so this waits, the same
		// way the adapter's address is waited for.
		if ctrl := a.curController(); ctrl == nil || !ctrl.InboundReady() {
			lastErr = fmt.Errorf("核心尚未就绪：核心的入站端口还没有开始监听")
			if !time.Now().Before(softDeadline) {
				break
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		err := a.tryTakeTunRoutes()
		if err == nil {
			a.logf(">>> TUN 网卡已接管默认路由")
			// The capture just moved the machine's whole route picture. NCSI's first
			// probe of this adapter went out while it had no routes and lost, and its
			// next try follows a passive backoff of minutes — the adapter works while
			// the system says it does not. The re-probe needs three things in place
			// before it can succeed, and the resident service's own care only
			// guarantees the third of them on its ten-second tick:
			//   - the adapter's own resolver, so the probe's first step — resolving
			//     www.msftconnecttest.com — is answered by the hijack at all. Routes
			//     cannot be taken before the address exists, so this call lands the
			//     resolver the moment it becomes possible, instead of up to one tick
			//     later.
			//   - the interfaces' resolvers, pointed at the takeover (the force gets
			//     it past the 30-second throttle).
			//   - the stack told to look again.
			if err := tunfast.SelfDNS(configbuilder.TunAdapterName()); err != nil {
				a.logf("[vpn] 适配器 DNS 未就位（接管路径）: " + err.Error())
			}
			// The rest is bookkeeping for the system's re-probe, and none of it is
			// worth the lock this call chain holds: startVPNExternal runs under
			// modeMu/hotkeyMu (the switch or the hotkey that asked for the node),
			// and reconcileSystemDNS with force can reach ensureTunServer — a
			// possible UAC prompt and readiness wait — inside that hold, parking
			// every other mode switch and hotkey behind it. Async, the way
			// emitRunningState's own reconcile has always run.
			go func() {
				a.reconcileSystemDNS(true)
				a.wakeNCSI()
			}()
			return nil
		}
		lastErr = err
		if strings.Contains(lastErr.Error(), "carries no address") && time.Now().Before(softDeadline) {
			// The interface needs Windows to finish with it; keep asking.
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if !time.Now().Before(deadline) {
			break
		}
		// 50ms rather than 200: this loop is the last thing between a ready core and
		// packets being captured, and what it does per pass — one interface-table read,
		// and when the adapter is there a few IP Helper writes — is microseconds. A
		// fifth of a second of granularity was most of the wait the reader saw.
		time.Sleep(50 * time.Millisecond)
	}
	a.logf("[vpn] TUN 路由接管失败: " + lastErr.Error())
	return fmt.Errorf("TUN 路由接管失败: %w", lastErr)
}

// retryTunRoutes keeps after a capture the start's window did not get, in the background.
//
// "Not up yet" and "will never be up" look the same from inside that window: the front-end
// is alive in both, and a first wintun build can outlast the whole soft wait (the front-end's
// own log says so: "open interface take too much time to finish"). The retry ends the moment
// the reason for a capture does — the mode was switched off, the node stopped, the
// front-end died — and only a run that finishes with the front-end still alive and nothing
// captured stands the mode down.
func (a *App) retryTunRoutes() {
	if !a.tunRouteRetry.CompareAndSwap(false, true) {
		return
	}
	go crashlog.Guard("retryTunRoutes", func() {
		defer a.tunRouteRetry.Store(false)
		for i := 0; i < tunRouteRetries; i++ {
			if i < tunRouteFastRetries {
				time.Sleep(tunRouteFastInterval)
			} else {
				time.Sleep(tunRouteRetryInterval)
			}
			if a.quitting.Load() || a.curSetting() == nil || !a.curSetting().VPNMode() {
				return
			}
			if a.runningID() == core.StoppedID {
				return
			}
			pid, _ := a.vpnState()
			if pid <= 0 || !vpn.IsRunning(pid) {
				break
			}
			if err := a.tryTakeTunRoutes(); err == nil {
				a.logf(">>> TUN 网卡已接管默认路由（后台补上）")
				// Same three-part readiness the synchronous capture guarantees: the
				// adapter's resolver, the interfaces', and the wake-up. The probe
				// that lost before the routes existed should not be the last word
				// on this adapter's connectivity.
				if err := tunfast.SelfDNS(configbuilder.TunAdapterName()); err != nil {
					a.logf("[vpn] 适配器 DNS 未就位（后台补上）: " + err.Error())
				}
				a.reconcileSystemDNS(true)
				a.wakeNCSI()
				a.notifyMessage("TUN 网卡已就绪")
				return
			}
		}
		// Through standDownTUN: this runs on the retry's own goroutine with no
		// lock held, and the bare flag flip used to race a mode switch in flight
		// for the seat — the flag could say off while the switch was putting it
		// back on (see standDownTUN).
		a.standDownTUN(nil, "TUN 路由接管失败，已关闭 TUN 模式（当前直连）")
	})
}

// handleTunCaptureFailure decides what a capture that did not go up means. The front-end
// alive is the shape a slow adapter build has, and the one the background retry can still
// finish; the front-end gone is the shape nothing is coming to fix, and the mode is stood
// down with it — a switch that says on over a direct connection is the state this exists to
// prevent (see the failure note on takeTunRoutes).
// handleTunCaptureFailure is the form for a caller that ALREADY holds the switch seat
// (startStopMu + modeMu): startVPNExternal and startCapture reach it from inside
// startProfileLocked / SetVPNMode, where taking the seat again would self-deadlock —
// sync.Mutex is not reentrant, which is why the locked and lock-free forms are two
// functions rather than one with a flag.
func (a *App) handleTunCaptureFailure(err error) error {
	if pid, _ := a.vpnState(); pid > 0 && vpn.IsRunning(pid) {
		a.logf("[vpn] " + err.Error() + "；前端仍在，后台继续接管路由")
		a.notifyMessage("TUN 网卡尚未就绪，正在后台接管路由…")
		a.retryTunRoutes()
		return nil
	}
	a.captureFailureStandDown(err)
	return err
}

// handleTunCaptureFailureFromWatcher is the lock-free-caller form: watchTunFront runs
// on its own goroutine holding nothing, so the stand-down half takes the seat itself —
// and re-checks the world under it. A front-end that came up while this waited belongs
// to whoever started it (its own capture path owns the routes: the retry, not a
// stand-down); a mode already off belongs to the switch that took it down, and this
// then says nothing.
func (a *App) handleTunCaptureFailureFromWatcher(err error) error {
	if pid, _ := a.vpnState(); pid > 0 && vpn.IsRunning(pid) {
		a.logf("[vpn] " + err.Error() + "；前端仍在，后台继续接管路由")
		a.notifyMessage("TUN 网卡尚未就绪，正在后台接管路由…")
		a.retryTunRoutes()
		return nil
	}
	a.startStopMu.Lock()
	a.modeMu.Lock()
	defer a.startStopMu.Unlock()
	defer a.modeMu.Unlock()
	st := a.curSetting()
	if st == nil || !st.VPNMode() {
		return err
	}
	if pid, _ := a.vpnState(); pid > 0 && vpn.IsRunning(pid) {
		a.logf("[vpn] " + err.Error() + "；前端仍在，后台继续接管路由")
		a.notifyMessage("TUN 网卡尚未就绪，正在后台接管路由…")
		a.retryTunRoutes()
		return nil
	}
	a.captureFailureStandDown(err)
	return err
}

// captureFailureStandDown is the shared body: release the capture, drop the flag and
// leave the machine direct. Seat held by the caller in both forms above.
func (a *App) captureFailureStandDown(err error) {
	a.releaseTunRoutes()
	a.curSetting().SetVPNMode(false)
	a.rememberSpmode()
	a.refreshTrayStatus()
	a.notifyMessage(err.Error()+"，已关闭 TUN 模式（当前直连）", true)
}

func (a *App) releaseTunRoutes() {
	link, err := a.tunLink()
	if err != nil {
		// No adapter is the ordinary answer — TUN mode has been switched on with
		// nothing warmed up yet, or off after the adapter was reaped — and it is
		// not worth a line. Anything else is the interface table refusing to be
		// read, which is.
		if !errors.Is(err, tunfast.ErrNotFound) {
			a.logf("[vpn] " + err.Error())
		}
		return
	}
	// A release that fails leaves the machine behind a tunnel with nothing to
	// forward through, which is the one outcome this path exists to prevent, so it
	// is retried before it is reported. Three attempts: the failure that matters —
	// a route the table refuses to give up — does not improve with a fourth, and
	// every attempt is a few microseconds when it works.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if lastErr = link.Release(); lastErr == nil {
			a.logf("<<< TUN 网卡已交还默认路由（适配器保留）")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.logf("[vpn] 交还默认路由失败: " + lastErr.Error())
}

// tunLink finds the adapter by the name the config asked for.
func (a *App) tunLink() (*tunfast.Link, error) {
	return tunfast.Find(configbuilder.TunAdapterName())
}

// tailVPNLog streams the elevated core's log file into the panel until stop is
// closed. Only complete lines are consumed: the offset is parked at the last
// newline, so a line still being written is picked up on the next poll.
func (a *App) tailVPNLog(path string, stop <-chan struct{}) {
	var offset int64
	for {
		select {
		case <-stop:
			return
		case <-time.After(300 * time.Millisecond):
		}
		f, err := os.Open(path)
		if err != nil {
			continue // file may not exist yet, retry
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			continue
		}
		if info.Size() < offset {
			offset = 0 // truncated or recreated, start over
		}
		if info.Size() > offset {
			_, _ = f.Seek(offset, io.SeekStart)
			data, _ := io.ReadAll(f)
			if cut := strings.LastIndexByte(string(data), '\n'); cut >= 0 {
				offset += int64(cut + 1)
				for _, line := range strings.Split(strings.ReplaceAll(string(data)[:cut], "\r\n", "\n"), "\n") {
					if line != "" {
						a.logf(line)
					}
				}
			}
		}
		_ = f.Close()
	}
}
