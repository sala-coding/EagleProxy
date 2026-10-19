package main

import (
	"eagle/internal/core"
	"eagle/internal/db"
	"eagle/internal/logstore"
	"eagle/internal/setting"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// maxCoreRestarts is how many automatic restarts are allowed inside
// coreRestartWindow before the supervisor gives up and says so. nekoray carries the
// same arrangement (ExternalProcess.cpp: an auto-restart limited by a rate), and the
// two numbers here are its shape: three tries, then the core is left down and the
// window says why rather than flickering between started and stopped.
const (
	maxCoreRestarts   = 3
	coreRestartWindow = time.Minute
)

// reload re-reads groups, profiles and settings from the data directory.
//
// keepCore is set when the data root itself did not move, i.e. a plain refresh.
// Rebuilding the controller there would kill a live core, reset runningID (so
// the status bar and the running row lose the node) and drop the runtime
// system-proxy / TUN flags — leaving Windows proxying a port nothing listens
// on. So the controller is kept and only re-pointed at the fresh data.
// SetDataDir passes false: the config path and data root both change.
func (a *App) reload(keepCore bool) error {
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err
	}
	a.setMgr(db.NewManager(a.dataDir))
	if err := a.curMgr().Load(); err != nil {
		// A failed load on first launch leaves the setting nil, and every
		// binding is about to be asked — the webview has booted either way.
		// The nil-deref GetSPMode already defends against was the state this
		// early return produced; defaults answer what those bindings read,
		// the same "everything off" reading, instead of panicking into the
		// process. Later reloads keep their previous setting, which is the
		// older data the reader was seeing anyway.
		if a.curSetting() == nil {
			a.setSetting(setting.Load(a.dataDir))
		}
		return err
	}
	prevSetting := a.curSetting()
	a.setSetting(setting.Load(a.dataDir))
	a.curSetting().InheritRuntime(prevSetting)
	// Another data directory is another routing profile, and with it another pair of
	// resolvers for the forwarder to stand behind (see app_dns.go). Nothing is up on the
	// first call of a launch; from the second on it is.
	a.syncDNSStub()

	if a.logs == nil {
		a.logs = logstore.New(a.logDir())
	} else {
		a.logs.SetDir(a.logDir())
	}

	if keepCore && a.curController() != nil {
		a.curController().SetManager(a.curMgr())
		a.curController().SetSetting(a.curSetting())
		a.curController().SetCorePath(a.corePath)
		return nil
	}

	old := a.curController()
	a.setController(core.New(a.curMgr(), a.curSetting(), a.dataDir))
	a.curController().SetCorePath(a.corePath)
	// The generation moves before the old controller is stopped, not after. Stopping
	// it fires its OnExit, and the guard in onCoreExit is what tells a controller
	// being replaced from a core that has crashed: a stale generation is ignored as
	// bookkeeping. Bumped afterwards, this very stop went through the crash path —
	// the supervisor started the node again on top of whatever the reload was doing,
	// and the reader was told the core had exited. markExpectedStop is deliberately
	// not used here: that flag belongs to a stop the profile asked for, and consuming
	// it would swallow the next genuine crash.
	a.coreMu.Lock()
	a.coreGen++
	gen := a.coreGen
	a.coreMu.Unlock()
	if old != nil {
		_ = old.Stop()
	}
	a.curController().OnLog = a.logf
	a.curController().OnExit = func(id, pid int) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "core:exit", id)
		}
		a.onCoreExit(gen, id, pid)
	}
	return nil
}

// markExpectedStop records that the core is about to be stopped on purpose, so the
// exit that follows is not mistaken for a crash. It is set by the paths that stop
// the core themselves — StopProfile (which every switch goes through), and shutdown
// — and consumed by onCoreExit.
//
// The process is named, not just the fact: the mark holds the pid of the core this
// stop is about (0 when nothing is up, so it matches no exit rather than the next
// one). It deliberately says nothing about the crash count: every failed start stops
// its own core on the way out, and clearing the count here made the rate limit count
// nothing (see onCoreExit, now the only writer of that count).
func (a *App) markExpectedStop() {
	pid := 0
	if a.curController() != nil {
		pid = a.curController().PID()
	}
	a.coreMu.Lock()
	if pid == 0 {
		// Nothing to stop: an exit cannot carry this mark, and leaving a matchable
		// one behind is how the next genuine crash goes unsupervised.
		a.expectStopPID = -1
	} else {
		a.expectStopPID = pid
	}
	a.coreMu.Unlock()
}



// onCoreExit decides what a core that has gone means. Asked-for exits are just the
// other half of a stop; an exit nobody asked for is a crash, and the core is started
// again on the same profile. nekoray supervises its core the same way
// (sys/ExternalProcess.cpp restarts it, with a rate limit on the retries); without
// this, a core that dies on one transient error leaves the window saying "not
// running" over a machine whose traffic has simply stopped moving, with nothing in
// the app tying the two together.
func (a *App) onCoreExit(gen, id, pid int) {
	a.coreMu.Lock()
	if gen != a.coreGen {
		// a controller that has been replaced: it stopped its own core on the way
		// out, and that exit is bookkeeping rather than news
		a.coreMu.Unlock()
		return
	}
	expected := pid != 0 && pid == a.expectStopPID
	a.expectStopPID = 0
	if expected || id == core.StoppedID {
		a.coreMu.Unlock()
		return
	}
	// Keep only the restarts inside the window, then count this one: a core that
	// cannot stay up is a fact worth telling the reader once, not a loop worth
	// running quietly.
	now := time.Now()
	kept := a.restarts[:0]
	for _, t := range a.restarts {
		if now.Sub(t) < coreRestartWindow {
			kept = append(kept, t)
		}
	}
	a.restarts = append(kept, now)
	attempt := len(a.restarts)
	a.coreMu.Unlock()

	if attempt > maxCoreRestarts {
		// Giving up is a state change the window and the tray have to hear about:
		// the running row went on naming the node that had just died, with nothing
		// left running to correct it.
		a.notifyMessage(fmt.Sprintf("核心反复退出（%d 次），已停止自动重启", maxCoreRestarts), true)
		// Standing down the shapes is the part giving up used to skip. Under TUN the
		// front-end goes on pushing the machine's whole traffic into a port nothing
		// listens on — the supervisor's surrender read as 直连 while every packet died
		// in the adapter; under system proxy the Windows setting still points at that
		// dead port. Both stand down here, the same sequence the TUN front-end's own
		// watcher uses when it cannot recover (watchTunFront), so the surrender leaves
		// a machine that is merely direct rather than one that cannot reach anything.
		if st := a.curSetting(); st != nil {
			if st.VPNMode() {
				a.releaseTunRoutes()
				if err := a.stopTunFront(); err != nil {
					a.logf("[vpn] 停止 TUN 前端失败（放弃自动重启路径）: " + err.Error())
				}
				st.SetVPNMode(false)
				a.notifyMessage("核心反复退出，TUN 模式已关闭（当前直连），可重新开启", true)
			}
			if st.SPModeSystemProxy() {
				if err := a.applySystemProxy(false); err != nil {
					a.logf("[sp] 清除系统代理失败（放弃自动重启路径）: " + err.Error())
				}
				st.SetSPModeSystemProxy(false)
				a.notifyMessage("核心反复退出，系统代理已关闭（当前直连），可重新开启", true)
			}
			a.rememberSpmode()
			a.refreshTrayStatus()
		}
		a.emitRunningState()
		return
	}
	a.notifyMessage("核心意外退出，正在自动重启…")
	if err := a.StartProfile(id); err != nil {
		a.notifyMessage("自动重启失败: "+err.Error(), true)
	}
}

func (a *App) runningID() int {
	if a.curController() == nil {
		return core.StoppedID
	}
	return a.curController().RunningID()
}
