//go:build windows

package tunserv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/tunfast"
	"eagle/internal/util"
	"eagle/internal/vpn"
)

// adapterCheckTicks is how often the main loop asks whether the adapter is still
// there, in 250 ms ticks — ten seconds. Frequent enough that a device taken away
// is back before the next TUN start, rare enough that the table read it costs is
// invisible.
const adapterCheckTicks = 40

// sysDNSIntervalTicks is how often the resolver takeover is reconciled, in 250 ms ticks —
// one second. Faster than the adapter check because it is the one thing here the reader is
// waiting on: the takeover should land as the node starts, not up to ten seconds later.
const sysDNSIntervalTicks = 4

// adapterMu serialises adapter preparation between the start-up goroutine and
// the loop's periodic check.
var adapterMu sync.Mutex

// adapterReady and lastPrepareErr remember the previous answer, so the log says
// something when the answer changes rather than every ten seconds.
var (
	adapterReady       bool
	lastPrepareErr     string
	lastAdapterCleanup time.Time
	lastDNSNote        string
)

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
	adapterReady := make(chan struct{})
	go func() {
		// Only the adapter is prepared here. A front-end left over from a previous
		// life is not this moment's business: the app may be about to adopt it, and
		// a sweep here would kill a front-end that is doing its job. The sweep
		// belongs where the intent is known — right before a new front-end is
		// spawned, see ActionStart.
		defer close(adapterReady) // also on a panic: the main loop waits on this forever otherwise
		defer func() {
			if r := recover(); r != nil {
				log("adapter preparation panic: %v", r)
			}
		}()
		// Only when TUN is what the reader is asking for. This process now also serves
		// system-proxy mode (the machine's resolvers, see sysdns_windows.go), and an adapter
		// prepared for a mode nobody is in is a network device that appears on the reader's
		// list for nothing — with the app's own idle reclaim taking it apart again, on a
		// ten-second cycle, for as long as the app stays in the other mode.
		if ReadState(dataDir()).Adapter {
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
	ticks := 0
	sysTicks := 0
	for {
		// The heart is written by its own goroutine (see above), deliberately
		// not here: this loop can stall for seconds inside one pass.

		// The machine's resolvers are reconciled on a faster beat than the adapter, and
		// this is the one piece of the elevated side the reader waits on directly: a node
		// starting means their names should stop being answered by whichever resolver on
		// the wire replies first, and a ten-second window of forged answers is exactly the
		// thing being fixed. Cheap to ask — one small file, read only when its stamp moved
		// is not worth arranging for a file this size.
		sysTicks++
		if sysTicks >= sysDNSIntervalTicks {
			sysTicks = 0
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
		ticks++
		if ticks >= adapterCheckTicks {
			ticks = 0
			if ReadState(dataDir()).Adapter {
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
				<-adapterReady
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
				// The device is normally here already — this server built it
				// ahead of time — but the idle reclaim takes it apart when no
				// node uses it, and the re-preparation runs on a ten-second
				// tick this start does not wait for.
				//
				// Prepare, not Find. Find only answers "an interface of that name
				// is in the table", and the state that makes a start fail is the
				// opposite one: the name is taken and the device cannot be opened
				// (the leftover a killed core leaves behind — "exists but cannot be
				// opened: Element not found"). A broken adapter is still in the
				// table, so the Find check passed it through to a spawn that could
				// only fail, while the ten-second tick was the only thing that
				// noticed. Prepare is the check the core itself will make, and it
				// is microseconds when the device is healthy.
				checkAt := time.Now()
				if err := tunfast.Prepare(configbuilder.TunAdapterName()); err != nil {
					log("adapter unusable before start (%v); clearing and rebuilding", err)
					ensureAdapterNow()
				}
				// Timed because this is one of the two places a cold TUN start can spend
				// its seconds, and the log is where that argument gets settled.
				if d := time.Since(checkAt); d > 20*time.Millisecond {
					log("adapter check before start: %v", d.Round(time.Millisecond))
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
				go func(p *exec.Cmd, f *os.File, done chan struct{}) {
					_ = p.Wait()
					code := 0
					if p.ProcessState != nil {
						code = p.ProcessState.ExitCode()
					}
					var size int64
					if f != nil {
						if st, err := f.Stat(); err == nil {
							size = st.Size()
						}
						_ = f.Close()
					}
					close(done)
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
				}(c, errF, child.exited)

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
					kill(pid)
					log("child stopped on command (pid %d)", pid)
				}
				child = nil

			case ActionExit:
				if pid := childPID(child); pid > 0 {
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
		time.Sleep(250 * time.Millisecond)
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
	if _, err := tunfast.Find(name); err == nil {
		if lastPrepareErr != "" {
			lastPrepareErr = ""
		}
		if !adapterReady {
			adapterReady = true
			log("adapter ready: %s", name)
		}
		// The adapter's resolver is not written here: it belongs to a node, not to a
		// device, and this function knows nothing about nodes (see
		// reconcileAdapterResolver).
		return
	}
	// Not in the table: never created, or prepared but never configured the way the
	// table would list it. Prepare is CreateAdapter — it answers 183 for a device that
	// is there but not listed — and it is the only thing that can make one.
	err := tunfast.Prepare(name)
	if err == nil {
		// Said when the answer changes, not every ten seconds: this runs forever,
		// and a line about nothing is what buries the lines that matter.
		if lastPrepareErr != "" {
			lastPrepareErr = ""
		}
		if !adapterReady {
			adapterReady = true
			log("adapter ready: %s", name)
		}
		// The adapter's resolver is not written here either: it belongs to a node,
		// not to a device (see reconcileAdapterResolver).
		return
	}
	adapterReady = false
	// A device that exists but cannot be opened is the broken leftover that makes
	// every later start fail, so it is cleared and built again — at most once a
	// minute. The cleanup is a PowerShell pass and the loop reaches this every ten
	// seconds: without the brake, a device that stays broken keeps the main loop
	// busy in the one place it must not be, the path a start command takes.
	if time.Since(lastAdapterCleanup) > time.Minute {
		lastAdapterCleanup = time.Now()
		if n, cerr := vpn.CleanStaleTunAdapters(); cerr == nil && n > 0 {
			log("cleared %d broken adapter(s), rebuilding", n)
			if err = tunfast.Prepare(name); err == nil {
				lastPrepareErr = ""
				adapterReady = true
				log("adapter ready: %s", name)
				return
			}
		}
	}
	if msg := err.Error(); msg != lastPrepareErr {
		lastPrepareErr = msg
		log("adapter prepare failed: %v", err)
	}
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

// ensureAdapterNow is ensureAdapter for a start that is about to happen: it skips the
// one-a-minute brake the ten-second loop needs, because a start is the one caller that
// cannot wait for it — the core is about to be launched on a device this call already
// knows is broken. Clearing and rebuilding is a few seconds; a core launched on the
// broken device is a failed start the reader has to press again.
func ensureAdapterNow() {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	name := configbuilder.TunAdapterName()
	if err := tunfast.Prepare(name); err == nil {
		adapterReady = true
		return
	}
	lastAdapterCleanup = time.Now()
	if n, cerr := vpn.CleanStaleTunAdapters(); cerr != nil {
		log("adapter cleanup failed: %v", cerr)
	} else {
		log("cleared %d broken adapter(s)", n)
	}
	if err := tunfast.Prepare(name); err != nil {
		log("adapter rebuild failed: %v", err)
		return
	}
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
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
}

// childProc is the server's tracked front-end: the command and a channel the
// watcher goroutine closes the moment Wait has reaped the process.
type childProc struct {
	cmd    *exec.Cmd
	exited chan struct{}
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
