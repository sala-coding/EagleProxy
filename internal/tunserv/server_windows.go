//go:build windows

package tunserv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/tunfast"
	"eagle/internal/util"
	"eagle/internal/vpn"
)

// The loop's beats are wall-clock intervals rather than tick counts, because the tick
// itself got short: a command waits for this loop, and the reader who pressed the
// switch waited with it — a 250 ms pass added up to that much to every start. The loop
// now wakes every commandPoll (below) and the two periodic jobs keep their own clocks.
const (
	// adapterCheckEvery is how often the loop asks whether the adapter is still there.
	// Frequent enough that a device taken away is back before the next TUN start, rare
	// enough that the table read it costs is invisible.
	adapterCheckEvery = 10 * time.Second

	// sysDNSEvery is how often the resolver takeover is reconciled. Faster than the
	// adapter check because it is the one thing here the reader is waiting on: the
	// takeover should land as the node starts, not up to ten seconds later.
	//
	// 250ms and not the old second: the NCSI wake waits on this clock — the app
	// writes the state file and then polls for every interface to carry the loopback
	// — and a state file the wake has just written sat unread for up to a second,
	// half the wake's whole budget, for nothing. The job costs two small file reads
	// and a no-op reconcile when nothing moved, and the loop itself already wakes
	// every commandPoll, so the shorter beat adds no passes at all.
	sysDNSEvery = 250 * time.Millisecond

	// commandPoll is how long the loop rests between passes. This is the delay a
	// command waits for before anything happens at all, so it is short; what the pass
	// itself costs is two small file reads and, when due, one of the jobs above.
	//
	// 100ms and not 25: the loop wakes forty times a second at 25, which measured 1.4%
	// of a core against 0.76% at the old quarter-second beat — for 75ms less latency on
	// a start that takes two seconds. A tenth of a second is the point where the wake
	// count stops being visible in the process's own CPU and the wait is still far below
	// every other stage of a switch-on.
	commandPoll = 100 * time.Millisecond
)

// adapterMu serialises adapter preparation between the start-up goroutine and
// the loop's periodic check.
var adapterMu sync.Mutex

// adapterReady and lastPrepareErr remember the previous answer, so the log says
// something when the answer changes rather than every ten seconds.
//
// preparedWant is the same idea for the work rather than the line: it records which
// request the device was last built for — "adapter" (a capture is up), "prewarm" (the
// app asked ahead of time) — so a tick that finds the same request does not build the
// device again. Preparing is CreateAdapter, a PnP creation measured at a second plus,
// and a create whose handle closes takes the device with it: without this memo the tick
// rebuilt it every ten seconds on a machine whose TUN was simply off.
//
// A memo and not a flag, because the earlier version was a flag that stayed true for
// the life of the process, and it took the two paths that need to act on an absent
// device with it: the cleanup of a broken leftover, and the rebuild after the device
// was removed on purpose (see ActionIdle, which clears it, and ensureAdapterNow, which
// clears it to force a real attempt).
var (
	adapterReady       bool
	preparedWant       string
	lastPrepareErr     string
	lastAdapterCleanup time.Time
	lastDNSNote        string
	// skipAdapterProbeUntil is when the open test may be asked again after it proved
	// the device unusable: the question costs fifteen seconds on a broken device, and
	// the answer cannot change inside the cleanup's own brake (see ensureAdapter).
	skipAdapterProbeUntil time.Time
)

// adapterWant answers which build request is on the table right now, from the state
// file rather than from a cached bool: the app rewrites it on every mode change, and
// that rewrite is exactly the moment the memo has to be reconsidered.
func adapterWant() string {
	st := ReadState(dataDir())
	switch {
	case st.Adapter:
		return "adapter"
	case st.Prewarm:
		return "prewarm"
	}
	return ""
}

// RunServer is the whole server program. It is entered with --tun-serve by the
// scheduled task, reads the app config once for the data directory, and then
// does two things forever: keep a heart on the wall, and eat one command at a
// time from the slot.
//
// The child (the sing-box front-end) is exactly one process, started by
// "start" — and left dead when it dies. A restart is the app's watchdog
// business or a fresh "start"; the server refusing to auto-respawn keeps the
// two sides from fighting over one adapter, the way two owners of one lease
// would.
func RunServer() {
	defer func() {
		if r := recover(); r != nil {
			log("server panic: %v", r)
		}
		// The machine's own resolvers go back before anything else this process does on its
		// way out. A server that exits with them still pointing at the app's forwarder —
		// which dies with the app — would leave the reader's names waiting out a timeout
		// before falling back to the resolvers they had (see tunfast.PrependResolvers).
		RestoreResolvers(dataDir())
		ClearHeartbeat(dataDir())
	}()

	log("server starting (pid %d)", os.Getpid())
	// A fresh process has no memory of the last apply, and the undo record cannot say
	// whether every interface took it: the process that wrote the record may have died
	// between them. One idempotent re-apply on the first tick settles it, and the flag
	// clears itself afterwards (see ReconcileResolvers).
	applyIncomplete = true
	// The adapter is built before anyone asks for it, in the background: this
	// process is already running and already elevated, and creating a wintun
	// adapter is a PnP device creation — seconds, and the one part of a TUN start
	// that nothing at the API level can make cheaper. Doing it here is what takes
	// those seconds off the reader's path.
	//
	// In the background rather than before the loop, because the app decides this
	// server is up by watching the heartbeat: a first beat that arrived only after
	// a three-second device creation would read as "no server" and send the app
	// down the slow path with a prompt of its own.
	// Named for what it signals, not for the flag it used to share a name with: the
	// package-level adapterReady says "the device is usable", this says "the start-up
	// preparation is finished" — and a local that shadows a package variable is a trap
	// for the next edit (this one caught today's: assigning to "adapterReady" here was a
	// compile error about a chan struct{} assigned a bool).
	adapterStartup := make(chan struct{})
	go func() {
		// Only the adapter is prepared here. A front-end left over from a previous
		// life is not this moment's business: the app may be about to adopt it, and
		// a sweep here would kill a front-end that is doing its job. The sweep
		// belongs where the intent is known — right before a new front-end is
		// spawned, see ActionStart.
		defer close(adapterStartup) // also on a panic: the main loop waits on this forever otherwise
		defer func() {
			if r := recover(); r != nil {
				log("adapter preparation panic: %v", r)
			}
		}()
		// Only when the device is wanted: a capture is up (Adapter), or the app asked for
		// it to be built ahead of the reader's first switch-on (Prewarm, see the app's
		// prewarmTunServer). This process now also serves system-proxy mode (the machine's
		// resolvers, see sysdns_windows.go), and an adapter prepared for a mode nobody is
		// in and nothing asked for is a network device that appears on the reader's list
		// for nothing.
		if st := ReadState(dataDir()); st.Adapter || st.Prewarm {
			ensureAdapter()
		}
	}()

	// The heart beats on its own goroutine, not once around the main loop: that
	// loop does synchronous work whose cost is its own — the PowerShell
	// enumeration behind reapFronts ("costs ten seconds and more"), a PnP
	// adapter build, the two-second kill wait in teardownAfterAppCrash — and a
	// beat written only between passes means any of those stalls the file past
	// ServerAlive's twelve seconds. The app then reads the server as dead: its
	// probe runs once per session (see tunServerGone), so one such stall sent
	// every TUN start down the direct-launch fallback for the rest of the
	// session, and the shutdown path skipped ActionExit for a server that was
	// alive all along.
	//
	// Registered after the outer defer, so it stops FIRST (defers run LIFO):
	// the wait below is what keeps a tick in flight from re-creating the heart
	// file a moment after the exit path has cleared it.
	heartbeatStop := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		fails := 0
		beat := func() {
			if err := Heartbeat(dataDir()); err != nil {
				// Said when it repeats rather than once: a single failure is a
				// locked temp folder or a full disk for one write, and the log
				// only earns its line when the failure is a condition.
				if fails%3 == 2 {
					log("heartbeat write failed: %v", err)
				}
				fails++
			} else {
				fails = 0
			}
		}
		beat() // first beat at once, as the loop's first pass used to give it
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-heartbeatStop:
				return
			case <-t.C:
				beat()
			}
		}
	}()
	defer func() {
		close(heartbeatStop)
		<-heartbeatDone
	}()

	// The live child, plus a channel the watcher closes once Wait has reaped it:
	// exec.Cmd documents ProcessState as writable only by Wait, and one reading
	// on the loop tick while Wait writes it from the watcher raced every 250
	// milliseconds. Every "is it still with us" check resolves on the channel.
	var child *childProc
	lastAdapterCheck := time.Now()
	lastSysDNS := time.Now()
	for {
		// The heart is written by its own goroutine (see above), deliberately
		// not here: this loop can stall for seconds inside one pass.

		// The machine's resolvers are reconciled on a faster beat than the adapter, and
		// this is the one piece of the elevated side the reader waits on directly: a node
		// starting means their names should stop being answered by whichever resolver on
		// the wire replies first, and a ten-second window of forged answers is exactly the
		// thing being fixed. Cheap to ask — one small file, read only when its stamp moved
		// is not worth arranging for a file this size.
		if time.Since(lastSysDNS) >= sysDNSEvery {
			lastSysDNS = time.Now()
			st := ReadState(dataDir())
			// The state file describes a machine that has a UI process behind it — the
			// resolver forwarder on 53 and the front-end both live there. A crash takes
			// the UI away without clearing the file, so a server obeying it would leave
			// the machine pointed at a port nobody holds (every name waiting out a
			// timeout) and a capture with no core behind it (no internet at all). The
			// heartbeat is what separates that from a tick that simply has not arrived
			// yet; when it has stopped, the machine is handed back in full.
			if (st.Adapter || st.Resolvers) && AppHeartStale(dataDir()) {
				log("app heartbeat is stale; handing the machine back")
				RestoreResolvers(dataDir())
				st = State{}
				if err := WriteState(dataDir(), st); err != nil {
					log("app state clear failed: %v", err)
				}
				if pid := teardownAfterAppCrash(dataDir(), child); pid > 0 {
					// The process is gone and its reaper will not need the slot; the
					// record and the books go with it so the next start does not adopt
					// an ending it never owned.
					child = nil
				}
			}
			ReconcileResolvers(dataDir(), st.Resolvers)
			// And the adapter's own resolver, which belongs to the node: on while
			// one is serving, away when there is none.
			reconcileAdapterResolver(st.Node)
		}

		// Checked now and then rather than once: the device can be taken away
		// while this process lives — the app's own cleanup removes a broken one,
		// a reader can uninstall it by hand — and the whole point here is that the
		// next start does not pay for a device that could have been built already.
		if time.Since(lastAdapterCheck) >= adapterCheckEvery {
			lastAdapterCheck = time.Now()
			if st := ReadState(dataDir()); st.Adapter || st.Prewarm {
				ensureAdapter()
			}
		}

		if cmd := ConsumeCommand(dataDir()); cmd != nil {
			switch cmd.Action {
			case ActionStart:
				// The core that follows opens the adapter, and two processes racing
				// to create the same device is how one of them ends up trying to
				// open an adapter that is still being built. Waiting for the
				// preparation already in flight is cheaper than that failure, and
				// it is a closed channel the moment the first one is done.
				<-adapterStartup
				if child != nil && child.cmd.Process != nil && !childReaped(child.exited) {
					log("start ignored: child pid %d already running", child.cmd.Process.Pid)
					break
				}
				app := appPathsOf()
				core, cfg := cmd.Core, cmd.Config
				if core == "" {
					core = app.Core
				}
				if cfg == "" {
					cfg = filepath.Join(app.DataDir, "temp", "tun-front.json")
				}
				if _, err := os.Stat(core); err != nil {
					log("start rejected: core %s missing", core)
					break
				}
				if _, err := os.Stat(cfg); err != nil {
					log("start rejected: config %s missing", cfg)
					break
				}
				// Whatever else is running on this config goes first: a front-end
				// the app lost track of holds the adapter and its addresses, and
				// the core about to start would fail configuring an interface that
				// is already configured — which the reader sees as "TUN will not
				// start". Only this process can clear it (the app is unelevated),
				// and here is where it knows the adapter is about to be needed.
				swept := reapFrontsIfAny(cfg, childPID(child))
				// The device is left to the core. Preparing it here is a PnP create
				// (measured 0.86s on this machine) done *before* the child is spawned,
				// so it buys the child nothing it could not do itself — the child's own
				// start-up is where the interface is configured anyway, and a create
				// inside that start overlaps work this serial call cannot.
				//
				// What the check used to catch — "the name is taken and the device
				// cannot be opened", the leftover a killed core leaves — is caught by
				// the child's own failure instead, and repaired there (see the exit
				// watcher below). The interface table is still read, because a line
				// saying whether this start had a device to open is the one thing the
				// timing argument needed and a spawn alone does not tell.
				checkAt := time.Now()
				name := configbuilder.TunAdapterName()
				_, findErr := tunfast.Find(name)
				if d := time.Since(checkAt); d > 20*time.Millisecond {
					log("adapter lookup before start: %v (present=%v)", d.Round(time.Millisecond), findErr == nil)
				}
				c, errF, err := spawnChild(core, cfg)
				if err != nil {
					log("spawn failed: %v", err)
					break
				}
				child = &childProc{cmd: c, exited: make(chan struct{})}
				log("child started (pid %d)", c.Process.Pid)
				hash, _ := hashOf(cfg)
				_ = WriteFrontPID(dataDir(), FrontPID{PID: c.Process.Pid, Server: true, Config: cfg, ConfigHash: hash})
				if swept {
					// A book-named front-end died to make room, so front-ends
					// the book never knew may exist too. The net runs once,
					// with this child named as keep, behind the start.
					go reapStrays(cfg, c.Process.Pid)
				}
				// The resolver lands the moment the core gives the adapter its
				// address, not on the next ten-second tick. Order is the whole
				// point: the app takes the capture as soon as its core answers,
				// Windows probes the interface right after, and a probe that
				// meets an adapter without a resolver fails into a slow retry
				// backoff — the tray's "no internet" that outlived minutes of
				// perfectly good tunnelling. Resolver first, capture second,
				// probe passes on the first try.
				go func(name string) {
					deadline := time.Now().Add(30 * time.Second)
					for time.Now().Before(deadline) {
						if !tunfast.Addressed(name) {
							time.Sleep(200 * time.Millisecond)
							continue
						}
						if err := tunfast.SelfDNS(name); err != nil {
							log("adapter dns apply failed: %v", err)
						}
						return
					}
					log("adapter address never appeared; dns left to the periodic tick")
				}(configbuilder.TunAdapterName())
				// The pid book stays on disk when the child leaves, and that is
				// deliberate. The app reads its presence as "a front-end may have
				// died holding the adapter", and a wintun adapter abandoned by a
				// killed core is exactly what makes every later start fail with
				// "create adapter: Cannot create a file when that file already
				// exists" — for good, until someone clears the device by hand.
				// Clearing the record here threw that evidence away: the next
				// start skipped its sweep, the broken adapter stayed, and TUN
				// never came back. Whoever confirms the adapter is gone (the app,
				// in stopTunFront / sweepTunLeftovers) removes the file.
				go func(cp *childProc, f *os.File) {
					_ = cp.cmd.Wait()
					code := 0
					if cp.cmd.ProcessState != nil {
						code = cp.cmd.ProcessState.ExitCode()
					}
					var size int64
					var stderr string
					if f != nil {
						if st, err := f.Stat(); err == nil {
							size = st.Size()
						}
						// The message decides whether the device is worth repairing, so
						// it is read here while the handle is still open (see below).
						if size > 0 && size <= 64<<10 {
							if _, err := f.Seek(0, io.SeekStart); err == nil {
								if b, rerr := io.ReadAll(io.LimitReader(f, 64<<10)); rerr == nil {
									stderr = string(b)
								}
							}
						}
						_ = f.Close()
					}
					close(cp.exited)
					// The code and whether stderr said anything are the two facts that
					// tell "the core refused its config" apart from "something killed
					// it": a refused start writes stderr and exits on its own, a killed
					// one leaves the file empty. Both used to read as a bare "child
					// exited", which is why a failed TUN start had no evidence in it.
					if size > 0 {
						log("child exited (code %d; stderr %d bytes in %s)", code, size, errLogPath())
					} else {
						log("child exited (code %d; no stderr)", code)
					}
					// A start that died by itself on a device it could not open is the
					// leftover a killed core leaves behind: CreateAdapter answers "already
					// exists", OpenAdapter answers "element not found", and every later
					// start fails the same way until the device is cleared. Measured on
					// this machine: that exact pair in tun-front.err.log, and the app sat
					// 30 seconds on "adapter address never appeared" before the tick
					// noticed. Repairing here is what turns that into one retry.
					//
					// Guarded by asked: a child this server stopped exits code 1 with an
					// empty stderr too, and repairing on that made every TUN switch-off
					// pay a PowerShell sweep of the device tree (measured: 13 seconds of
					// "cleared 0 broken adapter(s)" between a stop and the next start).
					// Matched on the failure rather than on the exit code: a core that
					// refused a field of its config also exits code 1 with a message, and
					// sweeping the device tree after every one of those would cost the
					// reader 13 seconds per failed start to fix nothing.
					if !cp.asked.Load() && strings.Contains(stderr, "configure tun interface") {
						log("child could not configure the adapter; repairing it")
						ensureAdapterNow()
					}
				}(child, errF)

			case ActionStop:
				pid := childPID(child)
				if pid <= 0 {
					// Adopt legacy books: a front-end the old app path started
					// still answers to stop.
					if info := FrontPIDOf(dataDir()); info != nil && !info.Server {
						pid = info.PID
					}
				}
				if pid > 0 {
					if child != nil {
						child.asked.Store(true)
					}
					kill(pid)
					log("child stopped on command (pid %d)", pid)
				}
				child = nil

			case ActionIdle:
				// Order matters: a device a live core holds open cannot be removed, so
				// the child goes first and its exit is waited for. The book is read as
				// well as the child, because a front-end this server never raised (an
				// adopted one, or one the app started by hand) holds the device just as
				// firmly and is not in `child`.
				pid := childPID(child)
				if pid <= 0 {
					if info := FrontPIDOf(dataDir()); info != nil {
						pid = info.PID
					}
				}
				if pid > 0 {
					if child != nil {
						child.asked.Store(true)
					}
					kill(pid)
					// taskkill is synchronous, but teardown lags it by a beat: the
					// handles close as the process finishes dying, and the removal
					// below is the one call that needs them closed.
					for i := 0; i < 20 && vpn.IsRunning(pid); i++ {
						time.Sleep(100 * time.Millisecond)
					}
					log("child stopped for idle (pid %d)", pid)
				}
				child = nil
				name := configbuilder.TunAdapterName()
				removed, found, err := vpn.RemoveTunAdapter(name)
				if err != nil {
					log("adapter removal failed: %v", err)
					found = 0 // the sweep below is the only remaining way in
				} else {
					log("adapter removal: %s (removed %d of %d)", name, removed, found)
				}
				if found == 0 {
					// Nothing answered to the connection name. Either the device is
					// already gone, or it is in the broken state where it no longer
					// presents one — and that second shape is what makes every later
					// start fail. The sweep removes it, and it belongs here rather than
					// in the app: the call needs the token, which is this process's.
					if n, sErr := vpn.CleanStaleTunAdapters(configbuilder.TunAdapterName()); sErr != nil {
						log("adapter cleanup failed: %v", sErr)
					} else if n > 0 {
						log("cleared %d broken adapter(s) after teardown", n)
					}
				}
				// The device is gone on purpose, so the memo of what it was built for is
				// gone with it: the next request — a switch-on, or the prewarm the app
				// arms again — has to build one rather than be told the work was already
				// done (see preparedWant). This is the clear that a process-lifetime flag
				// could not make, and the reason a fresh app session found the tick
				// silent over an absent device.
				adapterMu.Lock()
				preparedWant = ""
				adapterMu.Unlock()
				// Stays resident. Turning TUN off is a mode change, not the end of the
				// session: the reader switches it back on, and the next start should pay
				// for a device rather than for a process, a UAC run and a driver load.
				// The tick will not rebuild what was just removed — the app clears
				// Adapter and Prewarm in the state file before it sends this.
				log("server idle: device removed, waiting for the next start")

			case ActionExit:
				if pid := childPID(child); pid > 0 {
					if child != nil {
						child.asked.Store(true)
					}
					kill(pid)
					log("child stopped for exit (pid %d)", pid)
				}
				log("server exiting on command")
				ClearHeartbeat(dataDir())
				return
			default:
				log("unknown action %q ignored", cmd.Action)
			}
		}

		if child != nil && childReaped(child.exited) {
			child = nil // reaped by the watcher above; the slot stays free
		}
		time.Sleep(commandPoll)
	}
}

// frontConfigPath is the fixed config the front-end runs: the file the app
// writes, and the one string in a command line that names a process as ours.
func frontConfigPath() string {
	return filepath.Join(dataDir(), "temp", "tun-front.json")
}

// reapFrontsIfAny clears a front-end left over from a previous life, and reports
// whether it cleared one.
//
// The pid book is what makes the cheap answer possible: it names the front-end
// this server last raised, so a record that is absent, dead, or this server's own
// child means there is nothing out there holding the adapter.
//
// When the book does name a live front-end, the kill is direct: taskkill on that
// one pid is milliseconds and frees the adapter, where the PowerShell
// enumeration in StopOrphanTunFronts costs ten seconds and more — most of what
// a TUN start after a hard kill spends waiting. The enumeration still runs, but
// as reapStrays behind the start it protects, and only when there is evidence
// it may find something: this function answered true.
func reapFrontsIfAny(cfg string, keep int) bool {
	if cfg == "" {
		return false
	}
	info := FrontPIDOf(dataDir())
	if info == nil || info.PID == keep || !vpn.IsRunning(info.PID) {
		return false
	}
	kill(info.PID)
	// taskkill is synchronous, but teardown lags it by a beat; give the
	// handles a second to close before the core is asked to open the adapter.
	for i := 0; i < 10 && vpn.IsRunning(info.PID); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if vpn.IsRunning(info.PID) {
		// The direct kill did not take. The enumeration sweep is the slow
		// hammer, and here it is earned — synchronous, before the spawn.
		log("stale front-end pid %d survived taskkill, full sweep", info.PID)
		reapFronts(cfg, keep)
		return false
	}
	log("cleared stale front-end (pid %d)", info.PID)
	return true
}

// reapStrays is the net for front-ends the pid book never knew — an orphan from
// a version that wrote no book, say. The enumeration is a PowerShell pass and a
// kill, ten seconds of work that must not sit between the app's toggle and the
// adapter, so it runs behind the start it protects.
//
// The world it assumed can move while it was queued: the keep child may be
// dead and the app may have raised a legacy front-end in its place — one the
// book then names. So the book is read again here, and a front-end it records
// alive is kept whatever keep said: the book is the lease, and its live owner
// is this moment's legitimate front-end, never a stray.
func reapStrays(cfg string, keep int) {
	if info := FrontPIDOf(dataDir()); info != nil && info.PID != keep && vpn.IsRunning(info.PID) {
		keep = info.PID
	}
	n, err := vpn.StopOrphanTunFronts(cfg, keep)
	if err != nil {
		log("front-end sweep failed: %v", err)
		return
	}
	if n > 0 {
		log("cleared %d stray front-end(s)", n)
	}
}

// reapFronts clears every front-end still running on cfg except keep.
//
// The app has its own sweep for this, and it is not enough on its own: it runs
// unelevated, and a front-end is an elevated process — one it started and has
// since lost track of is one it cannot enumerate, let alone kill. What that
// leaves behind is exactly the state this server exists to prevent: an old
// front-end holding the adapter and its addresses, a new one failing to
// configure an interface that is already configured, and TUN reported as "not
// starting" with the evidence split across two logs. This process carries the
// token, so the sweep belongs here as well as there.
func reapFronts(cfg string, keep int) {
	n, err := vpn.StopOrphanTunFronts(cfg, keep)
	if err != nil {
		log("front-end sweep failed: %v", err)
		return
	}
	if n > 0 {
		log("cleared %d stale front-end(s)", n)
	}
}

// teardownAfterAppCrash hands the machine back when the UI that owned a capture has died.
//
// The routes go first and the process second, which is the opposite of the ordinary stop
// path's order only in appearance: there the app releases while its front-end is still
// alive and then takes the process down; here nobody is left to order anything, and the
// window between the two is what the order is for. A capture whose routes are still in
// the table after its front-end dies is a machine whose packets go into an interface no
// core is reading — the "no internet" the reader sees — while the adapter itself is a PnP
// device that survives both. Releasing is by prefix (see tunfast.Link.Release), so a
// capture that is already gone answers no error and this stays safe to call twice.
//
// The pid book is consulted as well as the tracked child because a crash can happen to
// either shape: a front-end this server raised (the child) or one the app raised itself
// and left in the book. Whoever holds the adapter has to go, or the next start meets a
// device that exists and cannot be opened. The adapter device itself is kept, deliberately
// — the same "adapter stays, capture goes" the app's own stop path leaves behind, and the
// seconds it saves the next start.
//
// It answers the pid it took down, zero when there was nothing running to stop.
func teardownAfterAppCrash(dataDir string, child *childProc) int {
	if link, err := tunfast.Find(configbuilder.TunAdapterName()); err == nil {
		if rerr := link.Release(); rerr != nil {
			log("routes: release after app crash: %v", rerr)
		} else {
			log("routes: released after app crash (adapter kept)")
		}
	}
	pid := childPID(child)
	if pid <= 0 {
		if info := FrontPIDOf(dataDir); info != nil && vpn.IsRunning(info.PID) {
			pid = info.PID
		}
	}
	if pid > 0 {
		kill(pid)
		// Teardown lags taskkill by a beat (handles closing), and the difference matters
		// here rather than being tidiness: the caller clears its own child slot on the
		// pid this returns, and a front-end that survived would then be one nobody is
		// watching — the orphan this whole path exists to prevent.
		for i := 0; i < 20 && vpn.IsRunning(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if vpn.IsRunning(pid) {
			// It did not take. The pid book stays, deliberately: that record is the
			// next start's evidence that something may still be holding the adapter,
			// and it is the leftovers sweep's only lead.
			log("front-end survived the kill after app crash (pid %d)", pid)
			return 0
		}
		log("front-end stopped after app crash (pid %d)", pid)
	}
	// The book is the next start's evidence of a front-end that may still hold the
	// adapter. Here that front-end has just been stopped and the adapter is what the
	// leftover sweep would be looking for — nothing is left that the record could
	// truthfully describe.
	ClearFrontPID(dataDir)
	return pid
}

// ensureAdapter keeps the TUN adapter device ready ahead of time.
//
// Nothing is started and nothing is held here: the session belongs to whichever
// core runs on the adapter, and wintun allows exactly one. What this leaves
// behind is the PnP device — which outlives every handle to it, the same fact
// that leaves an adapter behind when a core is killed — so the core the app
// starts later opens an adapter instead of creating one, which is where its
// seconds would have gone.
//
// Silent when the adapter is already there and healthy, which is the normal
// case: a line every ten seconds about nothing would bury the lines that matter.
// A device that exists but cannot be opened is the broken leftover that makes
// every later start fail, so it is cleared and built again.
func ensureAdapter() {
	// The start-up call runs on its own goroutine while the loop can reach this
	// ten seconds later. Two preparations at once would mean two processes racing
	// to create one device — the case the start path waits to avoid — so they are
	// serialised here instead.
	adapterMu.Lock()
	defer adapterMu.Unlock()

	name := configbuilder.TunAdapterName()
	// A device the interface table already carries needs nothing done to it.
	//
	// This used to be Prepare on every pass — OpenAdapter and Close — and that opened
	// and closed the very session a core holds. The cost was not the call itself: it
	// woke the driver out of the state it settles into, and the next core to open the
	// device (the front-end, moments after a start command) paid for that in its own
	// start-up — the "sing-box started (3.11s)" of a config small enough to start in
	// a tenth of a second. Find answers the ordinary case for free; Prepare is kept
	// for the one it cannot answer, a device that is not in the table at all.
	want := adapterWant()
	// Nothing asks for a device: no memo either, so the next request — the reader
	// switching TUN on, or the app arming the prewarm again — is treated as new work
	// rather than as one this process has already done (see ActionIdle).
	if want == "" {
		preparedWant = ""
	}
	if _, err := tunfast.Find(name); err == nil {
		// A row in the interface table is not a working device. A leftover answers Find
		// and then fails to open — measured on this machine: Find ok (luid set),
		// Prepare "exists but cannot be opened: Element not found" — and trusting Find
		// let this function log "adapter ready" in twenty-one milliseconds over an
		// adapter that could not carry a packet, and never clean it.
		//
		// Opening is therefore the test, but only while no core holds the device:
		// opening and closing a live session wakes the driver out of the state the
		// core is using, which is what the Find-only path was written to avoid (see
		// the note above). No front-end, no session — so no risk.
		// A capture is recorded but the pid book names no front-end: either the book was
		// lost or the process holding the device is one this server never raised. Both
		// are the wrong moment to open and close the device — that is what disturbs a
		// live session — so the device is left alone and a start that cannot use it
		// reports it instead (the exit watcher repairs that one).
		if want == "adapter" && !frontHeld() {
			return
		}
		if frontHeld() {
			preparedWant = want
			if lastPrepareErr != "" {
				lastPrepareErr = ""
			}
			if !adapterReady {
				adapterReady = true
				log("adapter ready: %s", name)
			}
			return
		}
		// A device that just failed the open test is not asked again within the
		// minute: the answer cannot have changed, and the question costs fifteen
		// seconds of the loop (see the failed branch below).
		if time.Now().Before(skipAdapterProbeUntil) {
			return
		}
		if perr := tunfast.Prepare(name); perr != nil {
			log("adapter present but unusable (%v); clearing", perr)
			preparedWant = ""
			adapterReady = false
			lastPrepareErr = perr.Error()
			// Straight to the cleanup, never to a second build attempt: on a device
			// that answers "exists but cannot be opened" one Prepare measured 15.26
			// seconds (see the note on prepare_windows.go), so retrying it here —
			// twice per ten-second tick — did not repair anything and held the loop,
			// and with it every command, for a minute at a time.
			//
			// And then left alone for a minute. The cleanup may not have helped (a
			// device that stays broken is still in the table on the next tick), and
			// asking the same question again is another fifteen seconds of a loop that
			// commands have to get through. One minute is the same brake the cleanup
			// itself keeps.
			rebuildBrokenAdapter(name)
			skipAdapterProbeUntil = time.Now().Add(time.Minute)
			return
		} else {
			preparedWant = want
			if lastPrepareErr != "" {
				lastPrepareErr = ""
			}
			if !adapterReady {
				adapterReady = true
				// "warmed", not "ready": nothing holds this device, and it is gone the
				// moment the handle that made it closes — what is ready is the driver,
				// not a capture.
				log("adapter warmed: %s (device not held)", name)
			}
			return
		}
	}

	// Built already for this same request, and nothing has changed it since: the work
	// would be a create whose close takes the device away again (see preparedWant).
	if preparedWant == want && want != "" {
		return
	}
	// Not in the table: never created, or prepared but never configured the way the
	// table would list it. Prepare is CreateAdapter — it answers 183 for a device that
	// is there but not listed — and it is the only thing that can make one.
	err := tunfast.Prepare(name)
	if err == nil {
		preparedWant = want
		// Said when the answer changes, not every ten seconds: this runs forever,
		// and a line about nothing is what buries the lines that matter.
		if lastPrepareErr != "" {
			lastPrepareErr = ""
		}
		if !adapterReady {
			adapterReady = true
			// Warmed rather than ready: this call created the device and closed it
			// again, so what it leaves behind is a driver that has done the work
			// once — the device itself goes with the handle (see the note on prepare).
			log("adapter warmed: %s (device not held)", name)
		}
		// The adapter's resolver is not written here either: it belongs to a node,
		// not to a device (see reconcileAdapterResolver).
		return
	}
	adapterReady = false
	rebuildBrokenAdapter(name)
	if msg := err.Error(); msg != lastPrepareErr {
		lastPrepareErr = msg
		log("adapter prepare failed: %v", err)
	}
}

// rebuildBrokenAdapter clears the leftover devices and builds ours again, at most once
// a minute.
//
// The brake is for repetition, not for the work: a device that stays broken is cleared
// once, and if the rebuild fails too, the next attempt waits. Without it the loop — the
// one place a start command has to get through — would sit in a PowerShell sweep every
// ten seconds. A caller that has just proved the device unusable resets the clock first
// (see the verification branch in ensureAdapter), because that first cleanup is the one
// that has to happen.
func rebuildBrokenAdapter(name string) {
	if time.Since(lastAdapterCleanup) <= time.Minute {
		return
	}
	lastAdapterCleanup = time.Now()
	n, cerr := vpn.CleanStaleTunAdapters(configbuilder.TunAdapterName())
	if cerr != nil || n == 0 {
		return
	}
	log("cleared %d broken adapter(s), rebuilding", n)
	if err := tunfast.Prepare(name); err != nil {
		return
	}
	lastPrepareErr = ""
	adapterReady = true
	preparedWant = adapterWant()
	log("adapter warmed: %s (device not held)", name)
}

// applySelfDNS points the adapter's resolver at itself, where the main core's
// hijack-dns answers with the profile's own (smart-selected) DNS — and the tray's probe
// reads the tunnel as online. A no-op while the answer already matches, and nothing to
// do until the core has configured the address.
func applySelfDNS(name string) {
	if err := tunfast.SelfDNS(name); err != nil {
		if msg := err.Error(); msg != lastDNSNote {
			lastDNSNote = msg
			log("adapter dns failed: %v", err)
		}
	} else {
		lastDNSNote = ""
	}
}

// reconcileAdapterResolver points the adapter's resolver at the adapter while a node is
// serving, and takes it away when there is none.
//
// The resolver belongs to the node, not to the device: the address it names is answered by
// the main core's hijack-dns, so with no core behind it every query the machine sends it
// waits out a timeout. An adapter pointed at a peer that will not answer is a machine
// that cannot resolve a name at all — and the routes are already back on the physical
// adapter by then, so the machine has an internet connection and no way to use it.
//
// The adapter itself stays: it is the expensive half, and the next start reuses it.
func reconcileAdapterResolver(serving bool) {
	name := configbuilder.TunAdapterName()
	if serving {
		applySelfDNS(name)
		return
	}
	if err := tunfast.ClearSelfDNS(name); err != nil {
		if msg := err.Error(); msg != lastDNSNote {
			lastDNSNote = msg
			log("adapter dns release failed: %v", err)
		}
	} else {
		lastDNSNote = ""
	}
}

// frontHeld reports whether a front-end is running on this machine's device.
//
// The pid book names it, and a live process behind that pid is what holds the wintun
// session — the one thing that makes opening the device a bad idea (it wakes the driver
// out of the state the core is using; see ensureAdapter).
func frontHeld() bool {
	info := FrontPIDOf(dataDir())
	return info != nil && vpn.IsRunning(info.PID)
}

// ensureAdapterNow is ensureAdapter for a start that is about to happen: it skips the
// one-a-minute brake the ten-second loop needs, because a start is the one caller that
// cannot wait for it — the core is about to be launched on a device this call already
// knows is broken. Clearing and rebuilding is a few seconds; a core launched on the
// broken device is a failed start the reader has to press again.
func ensureAdapterNow() {

	adapterMu.Lock()
	defer adapterMu.Unlock()
	name := configbuilder.TunAdapterName()
	// A start is the one caller that has to look for itself: the device may have been
	// taken away since the tick last prepared one (see preparedWant).
	preparedWant = ""
	if err := tunfast.Prepare(name); err == nil {
		preparedWant = adapterWant()
		adapterReady = true
		return
	}
	lastAdapterCleanup = time.Now()
	if n, cerr := vpn.CleanStaleTunAdapters(configbuilder.TunAdapterName()); cerr != nil {
		log("adapter cleanup failed: %v", cerr)
	} else {
		log("cleared %d broken adapter(s)", n)
	}
	if err := tunfast.Prepare(name); err != nil {
		log("adapter rebuild failed: %v", err)
		return
	}
	preparedWant = adapterWant()
	lastPrepareErr = ""
	adapterReady = true
	log("adapter ready: %s", name)
}

// spawnChild starts the core the way the app starts it: current directory
// set to the config folder (which is where resolveCore sent sing-box all
// along), the console hidden, and nothing between the child and its log file.
//
// Its stderr, though, is captured rather than inherited. sing-box reports a
// fatal start — "start inbound/tun[tun-in]: configure tun interface: ..." — on
// stderr only, never in the file its own config names, and this server has no
// console to inherit one from: the reason a front-end refused to come up used
// to be discarded, leaving a bare "child exited" and a one-line warning in
// vpn-core.log. The file returned beside the command is where that reason now
// survives.
func spawnChild(core, cfg string) (*exec.Cmd, *os.File, error) {
	cmd := exec.Command(core, "run", "-c", cfg)
	cmd.Dir = filepath.Dir(cfg)
	var errF *os.File
	if f, err := openErrLog(); err == nil {
		errF = f
		cmd.Stderr = f
	}
	util.HideConsole(cmd)
	startAt := time.Now()
	if err := cmd.Start(); err != nil {
		if errF != nil {
			_ = errF.Close()
		}
		return nil, nil, err
	}
	// The other half of a cold start's seconds: process creation here is done by an
	// elevated process under the task scheduler's session, and that is not the same
	// cost as the app starting its own core.
	log("child process created in %v", time.Since(startAt).Round(time.Millisecond))
	return cmd, errF, nil
}

// errLogPath is where a front-end's stderr is kept: the log folder, beside the
// server's own log.
func errLogPath() string {
	return filepath.Join(appPathsOf().logDir(), "tun-front.err.log")
}

// openErrLog truncates the current stderr file and moves the previous run's aside first.
//
// The previous run is the one that failed: it is why the reader is starting the
// front-end again, and truncating in place made its reason unreadable at the exact
// moment someone went looking for it. Renaming can fail while the old process still
// holds the file; then this run appends and the previous text stays in front of it,
// which is still better than losing it.
func openErrLog() (*os.File, error) {
	path := errLogPath()
	_ = os.Rename(path, path+".prev")
	// Read as well as write: the watcher reads this file back after the child exits to
	// see *why* it died (see the exit goroutine), and a write-only handle fails that read
	// with "bad file descriptor" — which is how the repair for a device the core could
	// not configure sat there as dead code while the log line above it reported the byte
	// count it had clearly managed to measure.
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
}

// childProc is the server's tracked front-end: the command and a channel the
// watcher goroutine closes the moment Wait has reaped the process.
//
// asked is set by whichever branch of the loop kills the child. The watcher cannot tell
// a core that refused its config from one this server just stopped — both exit code 1 —
// and the difference decides whether the adapter is worth repairing (see the watcher).
type childProc struct {
	cmd    *exec.Cmd
	exited chan struct{}
	asked  atomic.Bool
}

// childReaped reports whether the watcher has finished reaping the child.
func childReaped(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// childPID answers which process the server owns right now, zero when none.
func childPID(child *childProc) int {
	if child == nil || child.cmd == nil || child.cmd.Process == nil || childReaped(child.exited) {
		return 0
	}
	return child.cmd.Process.Pid
}

// readChildPID reads a pid without trusting it live — the caller decides.
func readChildPID() (int, error) {
	info := FrontPIDOf("")
	if info == nil {
		return 0, fmt.Errorf("no pid record")
	}
	return info.PID, nil
}

// kill terminates one pid the hard way. Unbounded force is the shape every
// other stop in this codebase takes; a hung child that ignored graceful
// stops is the very thing this path exists for.
func kill(pid int) {
	if pid <= 0 {
		return
	}
	// Hidden like every other console child: this runs inside the GUI-subsystem
	// server, and a console tool started without CREATE_NO_WINDOW allocates a
	// fresh, visible console — the cmd window that flashed on every TUN enable
	// once the fast stale-front kill made this run on every start.
	cmd := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/F", "/T")
	util.HideConsole(cmd)
	_, _ = cmd.CombinedOutput()
}

// hashOf stamps the config's content the way both records carry it, empty on
// any read failure — a hash that cannot be computed is treated the same as a
// file that cannot be trusted for reuse.
func hashOf(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// tunserveLogMax is the size at which the server's log is moved aside: this
// file is opened append-only and never rotated, so a server that lives for
// months (it is installed once and beat every two seconds) grew a single file
// with no ceiling — the one file a reader opens when TUN misbehaves, and the
// slowest one to get into.
const tunserveLogMax = 5 << 20 // 5 MiB

// logChecks counts log lines so the size check below runs once every 256 of
// them rather than on each: the check is a stat, and log() is called from the
// command path and the watcher alike.
var logChecks atomic.Uint64

// log appends to the server's own file so its life is readable long after a
// dev console has scrolled it away.
func log(format string, args ...any) {
	path := uploadLogFile()
	if logChecks.Add(1)%256 == 1 {
		if st, err := os.Stat(path); err == nil && st.Size() >= tunserveLogMax {
			// Side, not deleted: the last few months are what a bug report
			// quotes. The swap is rename-then-open — anything racing it either
			// lands in the old file (now .prev) or opens a fresh one, and a
			// failed swap simply keeps appending to the big file.
			_ = os.Remove(path + ".prev")
			_ = os.Rename(path, path+".prev")
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05.000")+" "+format+"\n", args...)
}

func uploadLogFile() string {
	return filepath.Join(appPathsOf().logDir(), "tunserve.log")
}

// dataDir is read once from the same app config the UI reads, so the server
// and the app can never disagree about which folder the files live in.
func dataDir() string { return appPathsOf().DataDir }
