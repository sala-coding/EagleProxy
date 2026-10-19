package main

import (
	"context"
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/db"
	"eagle/internal/logstore"
	"eagle/internal/setting"
	"eagle/internal/sub"
	"eagle/internal/tray"
	"eagle/internal/tunserv"
	"eagle/internal/vpn"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateRepo is the GitHub repository (owner/name) the update check reads.
//
// This project's own repository is the default — a dev build then checks updates
// the same way a packaged one does, and a fork's build can redirect it at build
// time:
//
//	wails build -ldflags "-X main.updateRepo=owner/name"
//
// It used to name another project's repository — nekoray's — which meant this
// check announced THAT project's release tag as "a new version" and the download
// step was pointed at its assets: the day that project shipped a Windows .exe,
// "update" would have moved its build over this executable. A default that
// points at this project's own home is the same lesson told the other way: the
// updates must come from the place this program calls home, not from wherever
// the variable happens to have been left pointing.
var updateRepo = "sala-coding/EagleProxy"

// appVersion is this project's own release number, set at build time the same
// way updateRepo is:
//
//	wails build -ldflags "-X main.appVersion=v1.2.3 -X main.updateRepo=owner/name"
//
// Empty by default: a build nobody stamped reports the toolchain's revision
// instead (see GetAbout), and a dev build says what it is. The update check
// still orders releases by the build time, not by this string.
var appVersion = ""

// App is the Wails application backend.
type App struct {
	ctx context.Context

	// The two paths in both forms: as the reader wrote them (raw — what the settings
	// field shows and what is saved) and as the absolute paths the rest of the app
	// runs on. They part company only when a relative path is typed: "config" or
	// "sing-box.exe" is read against the folder the executable sits in (see
	// resolveAppPath), which is what lets the whole folder be copied and still work.
	dataDir     string
	dataDirRaw  string
	corePath    string
	corePathRaw string

	// The three core pointers are swapped (never mutated in place) and read from
	// every goroutine in the app. The pointer itself may be touched only through
	// the accessors below — see curSetting/curController/curMgr.
	mgr        *db.Manager
	setting    *setting.Setting
	controller *core.Controller
	stateMu    sync.Mutex

	logs *logstore.Store

	// One mode switch at a time. SetVPNMode and SetSystemProxyMode each stop the
	// core, restart it under the new shape, write the registry and wait on
	// elevation; four things can press them (the window's switch, the tray, a
	// global hotkey, the remembered-mode restore at startup) and none of them
	// knew about the others — two switches interleaving raced their stop/start
	// pairs into each other. The two methods never call one another (the
	// system-proxy path only flips the setting's own flag), so a plain mutex
	// holds, and a switch that waits for the one in front of it is honest queueing
	// rather than a lost press.
	modeMu sync.Mutex

	// One press at a time for the keys that start, stop or switch a node. The hotkey
	// layer dispatches every press on its own goroutine, so two presses a moment
	// apart both read the state as it stood before either of them ran: both saw
	// "nothing running", both started a node, and the second stopped what the first
	// had just brought up. Serialised, a double press means what it says — on, then
	// off — instead of a race whose winner decides.
	hotkeyMu sync.Mutex

	// One start/stop at a time, across every source. StartProfile and StopProfile
	// used to be reachable unlocked — the webview bindings, the tray, the crash
	// supervisor, the startup restore — and each of those raced its stop/start
	// pair into whichever was already mid-flight: two starts both read StoppedID
	// and both started a core, a stop landing under a start tore its TUN
	// sequence apart. The lock sits outside modeMu and hotkeyMu (this one is
	// taken first, by the mode switches too), and the locked bodies never take
	// it again — StartProfile's internal stop calls stopProfileLocked directly,
	// since sync.Mutex is not reentrant. Lock order is startStopMu → modeMu →
	// nothing, and startStopMu → hotkeyMu → nothing: no path runs the other
	// way.
	startStopMu sync.Mutex

	// What each profile looked like when the running config was built. A core reads one
	// config once, so a node edited after the start has an outbound in it that no longer
	// describes the row the reader is looking at; the hot-switch path compares the store
	// against this before moving a selector, and falls back to a restart when they differ
	// rather than reporting a switch that changed nothing. Guarded because a start writes
	// it while the UI goroutine reads it.
	hotMu   sync.Mutex
	hotKeys map[int]string

	// The VPN bookkeeping is touched by the UI goroutine (start/stop) and read
	// by trafficLoop, so it is guarded: an unsynchronised int read while another
	// goroutine writes it is a data race even when both values are "valid".
	vpnMu        sync.Mutex
	vpnPID       int
	vpnProfileID int
	// dnsNote is the last adapter-resolver failure this process reported, so the
	// ten-second tick that keeps the resolver applied says it once rather than on
	// every pass (see ensureTunResolvers). Guarded by vpnMu as well: a launch and an
	// adoption can each leave a watcher behind for a moment, and two goroutines
	// writing one string field is a data race even when they write the same value.
	dnsNote string
	// The machine-DNS takeover's bookkeeping, under a lock of its own: the reconcile runs
	// from the traffic poll and from every start and stop at once, and both of the things
	// below are about not doing the same work twice — the state file is written only when
	// it says something new, and the elevated server is asked for at most every thirty
	// seconds (see app_dns_takeover.go).
	sysDNSMu       sync.Mutex
	sysDNSState    tunserv.State
	sysDNSStateSet bool
	sysDNSAsked    time.Time
	// closed in stopVPN to end the goroutine tailing the VPN core's log file
	vpnTailStop chan struct{}
	// shutdownOnce keeps the exit path to one run. Wails calls OnShutdown after the
	// tray's own quit has already been through it (see QuitApp), and the second pass
	// would stop what is already stopped and sweep the front-ends again for nothing.
	shutdownOnce sync.Once
	// tunFrontMu serialises bringing the front-end up. Two things can ask for it at the
	// same time now — the warm-up that runs when TUN mode is switched on, and the start
	// of a node — and without this the second would find no pid, conclude that nothing
	// is running, and raise a second administrator prompt behind the first one's.
	tunFrontMu sync.Mutex
	// The resident-server probe, and the answer it reached. Both exist for the ten-second
	// wait in ensureTunServer: once per session that wait is a legitimate cold task start,
	// and every TUN start after it is a detour in front of the adapter for a server that
	// is not coming — which is exactly what the reader meets as "TUN takes forever". The
	// answer is cleared whenever a heartbeat appears, so a server that came up by any
	// other route is used again without restarting the app.
	tunServerProbe atomic.Bool
	tunServerGone  atomic.Bool
	// True while the app is deliberately taking the front-end down. The death watcher
	// must not treat its own mangement as an accident: a stop that lands inside the
	// watcher's 500ms window would otherwise read a recycled failure onto an intact
	// session, and the retry would fight the same teardown that caused it.
	tunStopping atomic.Bool
	// The countdown that takes an adapter with no node behind it apart. Armed
	// whenever one is left idle, cancelled by anything that wants it back; guarded
	// because it is armed on one goroutine and cancelled on another. The fired
	// timer re-checks the world rather than trusting its own age, which is what
	// Stop cannot do after the fact.
	tunIdleMu    sync.Mutex
	tunIdleTimer *time.Timer
	// One background capture retry at a time (see retryTunRoutes). A start that did not
	// get its routes inside the window may be followed by another start; two retries
	// taking the same routes is harmless but two rollbacks are not.
	tunRouteRetry atomic.Bool
	// True while a TUN node start is on its way — from the first wait of the
	// warm-up through the core's own start-up sequence. A fired idle-reclaim
	// timer reads this before touching anything: its window overlaps the whole
	// (slow) start, much of which runs before runningID is set.
	tunWake atomic.Bool
	// Set once the exit path has begun. A warm-up started moments before the reader quit
	// can still be sitting behind its administrator prompt, and the front-end it finishes
	// building would then outlive the app that asked for it — the orphan this code spends
	// so much effort cleaning up, created on the way out.
	quitting atomic.Bool
	// Set while an update is being installed. The exit that follows is the one exit
	// that must NOT keep the front-end (see keepTunFront): the update replaces this
	// executable, and every process running from it — the front-end, the resident
	// server — holds the image Windows refuses to overwrite.
	updating atomic.Bool
	// The update check and the update download, one at a time. Neither the tray item nor
	// the About card's button disables itself while a request is out, so a second press
	// used to mean a second request, a second toast for one answer, and — for the
	// download — two writers on one temp file and one swap script.
	updateBusy atomic.Bool

	// Messages waiting for a frontend to show, and whether one is listening yet.
	// Switching TUN on restarts the app (the tun adapter needs the administrator
	// token), so the "now pick a node" message is raised in a fresh process whose
	// webview is still booting — an event sent then would land nowhere. Those
	// messages are queued until OnDomReady instead.
	noticeMu      sync.Mutex
	noticeReady   bool
	pendingNotice []Notice

	// Last totals seen from the Clash API. trafficLoop turns them into per-profile
	// deltas; ResetMetrics rebaselines them so a manual reset is not immediately
	// undone by the traffic that happened between the previous poll and now.
	trafficMu sync.Mutex
	lastUp    int64
	lastDown  int64

	// The poll behind ExitAddress asked "what is my address" on a timer; a
	// fresh http.Transport per call left its keep-alive sockets parked behind
	// it for 90 seconds each time, and the handle count grew with the poll.
	// One client per proxy target is re-created only when the target changes.
	exitMu   sync.Mutex
	exitKey  string
	exitHTTP *http.Client
	// One address probe at a time. The corner asks every two seconds; a tunnel that
	// is still coming up can hold a request for its whole timeout, so the asks used
	// to stack up behind one another — each one waiting on the one before it — and
	// the answer the corner finally showed was the oldest of the pile. A second ask
	// while one is in flight now answers "nothing new" and lets the flight finish.
	exitProbeMu   sync.Mutex
	exitProbeBusy bool
	// The last answer and when it was taken. A start's warm-up writes it, the poll hands
	// it out while it is fresh, and a hot switch clears it — the address belongs to the
	// node that answered it.
	exitProbeAt   time.Time
	exitProbeLast map[string]string

	// Bookkeeping for the supervisor: a core that exits on its own is started again
	// (see onCoreExit). coreGen identifies the controller the exit came from — a
	// replaced controller stops its own core on purpose, and that exit is not news.
	// expectStopPID is the pid of the process a deliberate stop was raised for, or 0
	// (and -1 for "there was nothing to stop") when none was; the exit that answers
	// that stop is the one carrying this pid. A boolean could not name the process,
	// and that was two failures in one: a stop that produced no exit left the mark up
	// for the next real crash to consume, and a stop whose kill failed had its mark
	// taken down — so the death that eventually arrived read as a crash and the
	// reader's own stop was undone by a restart. restarts holds the moments of the
	// recent automatic restarts, which is what keeps a core that crashes in a loop
	// from being restarted in a loop.
	coreMu        sync.Mutex
	coreGen       int
	expectStopPID int
	restarts      []time.Time

	// The test run in progress, if any. A group test takes one node at a time and
	// can take minutes, and the reader may have decided about a node long before
	// its turn comes — so the run is cancellable rather than something to sit
	// through. CancelSpeedtest ends it; each worker checks the context before the
	// next node is handed out.
	speedtestMu     sync.Mutex
	speedtestCtx    context.Context
	speedtestCancel context.CancelFunc
	// speedtestDone is closed when the run registered above has finished its own
	// tail — workers drained, report written. Cancelling a run only asks it to stop;
	// the next run waits on this before it truncates the log file and starts its own
	// work, so one run at a time is a fact rather than an intention (see
	// takeOverSpeedtest).
	speedtestDone chan struct{}

	// subMu serialises the subscription walks: the per-group auto-update loop,
	// the background "update all" timer and the manual bindings all funnel their
	// sub.Update calls through it. Two walks racing the same group would interleave
	// snapshots and commits — a tree referencing nodes the other has just deleted.
	subMu sync.Mutex
}

// Accessors for the three core pointers. They are swapped on reload / applySetting
// while any goroutine of the app (traffic loop, hotkeys, tray, Wails bindings) may
// be reading them, so the pointer itself is read and written only under lock.
// Every value read goes through one of these.

func (a *App) curMgr() *db.Manager {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.mgr
}

func (a *App) curSetting() *setting.Setting {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.setting
}

func (a *App) curController() *core.Controller {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.controller
}

func (a *App) setMgr(m *db.Manager) {
	a.stateMu.Lock()
	a.mgr = m
	a.stateMu.Unlock()
}

func (a *App) setSetting(s *setting.Setting) {
	a.stateMu.Lock()
	a.setting = s
	a.stateMu.Unlock()
}

func (a *App) setController(c *core.Controller) {
	a.stateMu.Lock()
	a.controller = c
	a.stateMu.Unlock()
}

// NewApp creates the backend.
func NewApp() *App { return &App{} }

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// The window's caption icon is dealt with in domReady rather than here: at this
	// point the window does not exist yet, so there would be nothing to talk to.
	a.loadAppConfig()
	if err := a.reload(false); err != nil {
		println("init error:", err.Error())
	}
	// The app's own DNS forwarder, up before anything can ask it anything: the config
	// the core is handed names its two ports, and a port nobody is listening on is a
	// core whose every resolution fails. It falls back by itself if it cannot bind (see
	// app_dns.go and dnsstub.Endpoints).
	a.startDNSStub()
	// Write a marker straight away so text-logs is created on every launch and
	// it is obvious whether file logging works.
	if a.logs != nil {
		a.logs.Section("core", "启动 · "+time.Now().Format("2006-01-02 15:04:05")+" · "+a.logDir())
	}
	// A previous run can have left the adapter capturing. That is the shape after a
	// kill: the stop path never ran, so the routes it took are still in the table,
	// and nothing here starts a core on its own — a machine that began this way
	// would have no internet at all until someone switched TUN off by hand. The
	// adapter itself is welcome to stay (see SetVPNMode); only its capture is not.
	// A no-op when there is nothing to hand back.
	a.releaseTunRoutes()
	// The resolvers a previous run took over go back as well, and for the same reason: a
	// crash skipped the exit path, so the state file still says "taken over" while the
	// forwarder that answered on 53 died with that process. The server has its own
	// watchdog for this now (see tunserv.AppHeartStale), but the file is emptied here
	// too: a starting app is proof the old one is not running, and the machine's names
	// belong to the resolvers it had until a node asks for the takeover again.
	a.releaseSystemDNS()
	// And the same for the Windows proxy: a session that crashed left the machine pointed
	// at this app's inbound port, which no core is serving in a process that has just
	// started. The runtime flag reads off after a crash, so nothing else would ever
	// clear it (see clearStaleSystemProxy).
	a.clearStaleSystemProxy()
	// StartMinimal — the startup card's "启动时最小化" — puts the window into the
	// tray from the first frame rather than on the desktop. Hidden, not merely
	// minimised: a minimised window still holds a taskbar button, and a window
	// the reader asked not to see should not ask for one. The tray exists either
	// way (main.go sets it up right after this), so the window stays reachable.
	if a.curSetting() != nil && a.curSetting().StartMinimal {
		runtime.WindowHide(a.ctx)
	}
	// Every long-lived goroutine runs under crashlog.Guard: a panic in any of them
	// takes the process down without main's own deferred handler ever running, which
	// left crashes with nothing written down at all (see crashlog.Guard).
	go crashlog.Guard("trafficLoop", a.trafficLoop)
	// The stamp the resident server reads to tell a crash from a quiet moment. Its own
	// goroutine rather than a line in trafficLoop: that loop's period is a setting, and
	// the server's 12-second budget must not depend on one (see appHeartbeatLoop).
	go crashlog.Guard("appHeartbeat", a.appHeartbeatLoop)
	// The subscription timer, and the only background timer there is: the periodic
	// data refresh that used to live beside it (auto_refresh_min, in what was
	// app_auto.go) was taken out — the list is re-read by the events that change it
	// and by the reader's own actions, and a clock that re-reads it every N minutes
	// was a setting, a loop and a field to explain for something nothing waited on.
	go crashlog.Guard("subUpdateLoop", a.subUpdateLoop)
	go crashlog.Guard("applyPendingTun", a.applyPendingTun)
	// The resident server, warm before the first TUN enable wants it. Read-only on
	// this side's books (dataDir, the task registry) and prompt-free by design — a
	// task that is not installed is left for the first enable to create — so the
	// only thing a failure costs is the line it writes.
	go crashlog.Guard("prewarmTunServer", a.prewarmTunServer)
}

// applyPendingTun consumes a tun request left behind by an older build. The internal-tun
// path — which saved this request before relaunching itself through UAC — is gone, but a
// settings file from that era can still carry the flag set; consuming it here is what
// keeps such a file from raising a surprise prompt on every launch.
func (a *App) applyPendingTun() {
	if a.curSetting() == nil || !a.curSetting().PendingTun {
		return
	}
	// Consume the request whatever happens next: leaving it set would retry on
	// every launch, and each retry that lacks the token would raise a UAC prompt
	// of its own.
	profile := a.curSetting().PendingTunProfile
	a.curSetting().PendingTun = false
	a.curSetting().PendingTunProfile = core.StoppedID
	if err := a.curSetting().Save(); err != nil {
		a.logf("[vpn] " + err.Error())
	}

	if !vpn.IsAdmin() {
		a.logf("[vpn] 已取消待处理的 TUN 请求：本次运行没有管理员权限")
		// Worth saying out loud: the user asked for TUN one consent dialog ago and
		// this launch came back without the token, so nothing was enabled.
		a.notifyMessage("TUN 模式需要管理员权限，本次未能开启", true)
		return
	}
	if profile != core.StoppedID && a.curMgr().GetProfile(profile) != nil {
		// The flag goes down before the node comes up, so the profile starts
		// straight into its tun config — one core start, no live switch.
		a.curSetting().SetVPNMode(true)
		if err := a.StartProfile(profile); err != nil {
			a.logf("[vpn] " + err.Error())
			return
		}
		a.notifyMessage("TUN 模式已开启（已重启节点生效）")
		return
	}
	// The profile the request was made for is gone, and 节点先行 has no
	// capture to offer without one. Said in the handover's own words, which
	// is the message this path exists to deliver across the restart.
	a.notifyMessage("请先选择并启动一个节点，再开启 TUN 模式", true)
}

// subUpdateLoop is the app's subscription timer, and the only one: it auto-updates a
// group when its own interval (sub_interval, minutes) or, failing that, the settings'
// global fallback (sub_auto_update, hours) has come due. The tick is one minute, so an
// interval of N minutes fires somewhere between N and N+1 minutes after the last
// update — manual updates included, they refresh lastup too.
//
// A second, global timer used to run beside it with the same job and a minutes-based
// clock of its own (auto_sub_update_min, since removed). Two timers over one clock
// overlapped in the one way that matters: the minutes one refreshed every group's
// lastup on every tick, so this one's per-group intervals could never come due, and
// the interval in the group editor was a field nobody read. What was worth keeping is
// here: per group, due on its own clock. A group goes out of the rotation by having no
// interval (and no global fallback), which is one answer to one question rather than a
// second flag saying the same thing about the first.
func (a *App) subUpdateLoop() {
	for {
		time.Sleep(time.Minute)
		// a.ctx is never nil after startup, so quitting is the real exit signal
		// — and the loop takes it every minute, not only between long walks.
		if a.ctx == nil || a.quitting.Load() {
			return
		}
		s := a.curSetting()
		if s == nil {
			continue
		}
		now := time.Now().Unix()
		for _, gid := range a.curMgr().TabOrder() {
			// A snapshot: this loop reads the group's name, URL and interval
			// over a minute-long tick while the UI may be editing the live one
			// (UpdateGroup / SpliceGroup) under the manager's lock.
			g := a.curMgr().SnapshotGroup(gid)
			if g == nil || g.Archive || g.URL == "" {
				continue
			}
			// A group's own interval (minutes) wins. A group without one falls
			// back to the settings' global one — SubAutoUpdate, read in hours
			// (the subscription card's "自动更新（小时）"); the default is negative
			// so a settings file that never touched the field keeps the timer off,
			// and a group with neither interval is simply never fetched on a clock.
			interval := g.SubInterval
			if interval <= 0 {
				interval = s.SubAutoUpdate * 60
			}
			if interval <= 0 {
				continue
			}
			if now-g.SubLastUpdate < int64(interval)*60 {
				continue
			}
			a.logf(">>> 定时更新订阅: " + g.Name)
			// subMu serialises with the background timer and the bindings: two
			// walks on one group were snapshots overwriting each other.
			a.subMu.Lock()
			_, err := sub.Update(a.curMgr(), s, a.runningID(), g.URL, gid, a.logf, a.runningID)
			a.subMu.Unlock()
			// Announced either way: a walk that fails halfway has still rewritten
			// part of the batch on disk, and the table must re-read. -1 keeps the
			// reader on the group they are looking at, which this walk need not be
			// the one of.
			a.emitGroupsChanged(-1)
			if err != nil {
				a.logf("定时更新失败 (" + g.Name + "): " + err.Error())
				continue
			}
		}
	}
}

// shutdown runs the exit path once, whichever route arrives at it first — the tray's
// quit or the Wails shutdown that follows it.
func (a *App) shutdown() {
	a.shutdownOnce.Do(a.runShutdown)
}

// runShutdown mirrors nekoray's on_menu_exit_triggered: hand the system proxy back and
// stop the core before the process goes away, otherwise Windows would be left pointing
// at a local port with no listener.
func (a *App) runShutdown() {
	// Set before anything slow, so that a warm-up still sitting behind its administrator
	// prompt knows to stop rather than finish (see ensureTunFront and launchTunFront).
	a.quitting.Store(true)
	// Take the tray icon down first. Windows only removes an icon from the
	// notification area when the process asks it to (Shell_NotifyIcon with
	// NIM_DELETE); a process that simply ends leaves a ghost behind until the
	// pointer happens to pass over it, which reads as "it never really quit".
	// Doing it here, before anything slow, also means the icon is gone even if
	// stopping the core below takes a moment or asks for elevation.
	tray.Quit()
	// The core is about to be stopped on the way out; without this the supervisor
	// would read that exit as a crash and start a core in a process that is quitting.
	a.markExpectedStop()
	// One breadcrumb per stage: a jammed exit reads as "the app ignored me" unless
	// the file says which step never returned, and stderr carries it even when the
	// window is already closing.
	a.logf("[shutdown] stage: prepare")
	if a.curSetting() != nil && a.curSetting().SPModeSystemProxy() {
		a.applySystemProxy(false)
	}
	a.logf("[shutdown] stage: stop modes")
	if pid, _ := a.vpnState(); pid > 0 {
		// Quitting ends TUN mode: the front-end is a child of this app's choices, and
		// leaving an adapter-holding administrator process behind an exited window is
		// how orphans are made.
		_ = a.stopVPN()
		a.logf("[shutdown] stage: main core stopped")
		// The keep flag rewrites this ending. A front-end the resident server owns
		// has its own record and its own restart story: tearing it down now is
		// exactly the PnP cost the flag exists to avoid, and adopting it at the
		// next start is a read of one file. The same logic runs for a legacy
		// front-end — the record stays valid, whoever adopts it matches the
		// config before reuse, and the first TUN-off click reaps it — which is
		// what keeps both shapes one code path apart by a boolean.
		if a.keepTunFront() {
			a.setVPNState(0, 0)
			a.logf("[shutdown] stage: tun front-end kept (下下次启动复用)")
		} else {
			_ = a.stopTunFront()
			a.logf("[shutdown] stage: tun front-end stopped")
		}
	} else if a.curController() != nil {
		a.flushTraffic()
		_ = a.curController().Stop()
		a.logf("[shutdown] stage: main core stopped")
	}
	// The branch above only runs when the recorded pid says TUN was up — and it is
	// exactly the pid that cannot be trusted on the way out. A front-end whose start
	// died against an occupied adapter, or one recorded by a launch that has since
	// been superseded, is still out there holding EagleTun while that branch is
	// skipped altogether. The pid file is what survives to say so: stopTunFront above
	// clears it once the front-end is confirmed gone, so a clean exit pays nothing for
	// this, and an untracked front-end still cannot outlive the app.
	if a.tunFrontWasLeftBehind() {
		a.logf("[shutdown] stage: sweeping leftovers")
		a.sweepTunLeftovers()
	}
	// The server this run prewarmed or borrowed goes with the app — unless a
	// front-end was kept above, because a kept front-end is server-owned: the
	// record names it, and taking it down later (a stale config, a mode switch)
	// needs the token only the server holds. With nothing kept, a server left
	// running is an elevated process holding nothing, heart-beating for nobody;
	// the next launch prewarms its own in a second.
	if !a.keepTunFront() && a.dataDir != "" && tunserv.ServerAlive(a.dataDir) {
		_ = tunserv.SendCommand(a.dataDir, tunserv.Command{Action: tunserv.ActionExit})
		a.logf("[shutdown] stage: resident server dismissed")
	}
	// Tell the elevated side to put the machine's resolvers back first, while this process
	// can still write the file it reads. The server does the same on its own way out, and
	// whichever notices first, the reader's machine ends up as it was found (see
	// app_dns_takeover.go).
	a.releaseSystemDNS()
	// The heartbeat goes with it, and after the state: while it stands, the server reads
	// this process as alive and leaves the resolvers where the file above put them. A
	// keepTunFront exit leaves that server running, so the stamp is the one thing that
	// tells it the UI is gone rather than busy.
	if a.dataDir != "" {
		tunserv.ClearAppHeartbeat(a.dataDir)
	}
	// The forwarder goes last, and only now: the core was still asking it questions two
	// stages ago, and a port that vanishes under a core on its way out turns a clean stop
	// into failed resolutions in its log.
	a.stopDNSStub()
	a.logf("[shutdown] stage: dns forwarder stopped")
	a.logf("[shutdown] stage: closing logs")
	if a.logs != nil {
		a.logs.Close()
	}
	a.logf("[shutdown] stage: done")
}
