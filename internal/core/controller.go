// Package core launches and supervises a sing-box process using a config
// generated from a profile.
package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/db"
	"eagle/internal/logstore"
	"eagle/internal/setting"
	"eagle/internal/util"
)

// StoppedID is the sentinel "not running" profile id (matches nekoray).
const StoppedID = -1919

// Controller runs a single sing-box instance at a time.
type Controller struct {
	mu      sync.Mutex
	mgr     *db.Manager
	setting *setting.Setting

	corePath  string
	configDir string

	cmd       *exec.Cmd
	extCmds   []*exec.Cmd
	runningID int

	// hotNodes maps each profile the running config can be switched to without being built
	// again, to the outbound tag that profile answers to inside it (see
	// configbuilder.proxyGroup, and Result.HotNodes). Empty when the config carries no
	// selector — an external tun run, or a group of one — and then every switch is a
	// restart, exactly as it was before any of this existed.
	hotNodes map[int]string

	// runningConfig is the config the running core was started with.
	//
	// It is kept so a caller can ask whether the core it has is the one the
	// current settings and mode would start: a mode switch that changes nothing
	// in the config has no reason to stop a core that is already right, and the
	// only honest way to know that is to compare the config that is running with
	// the one that would be built — not to compare the mode with itself.
	runningConfig map[string]interface{}

	// stopRequested marks that this core is being stopped on purpose. The
	// supervisor reads it when the process is reaped: Windows reports a killed
	// process as an error exit, and "core exited: exit status 1" reads like a
	// crash of a core that was working a moment ago.
	stopRequested atomic.Bool

	// inboundReady says whether the core's own inbound is answering right now.
	//
	// A running process is not the same thing: sing-box reads its config, opens its
	// inbounds and only then prints "started", and everything the app arms on the
	// strength of a start goes through that inbound — the machine's resolvers point
	// at this app, and the TUN front-end hands every packet to it. Armed a second
	// early, those two things answered nothing until the core was really up: every
	// name on the machine failed, and the traffic the capture had taken went
	// nowhere.
	inboundReady bool

	logs []string

	// OnLog receives a line of core output.
	OnLog func(string)
	// OnExit receives the profile id the core was serving when it left, and the pid
	// of the process that left. The pid is what lets the supervisor tell the exit it
	// asked for from a crash: a core is stopped by asking it to die, and a mark that
	// is only a boolean cannot name which process it was about — so it was consumed
	// by whatever exited next (a stop that never produced an exit swallowed the next
	// real crash) or left standing (a stop that failed made the death it eventually
	// produced look like a crash, restarting the node the reader had just stopped).
	OnExit func(id, pid int)
}

// New creates a controller. configDir is where the generated config is written.
func New(mgr *db.Manager, s *setting.Setting, configDir string) *Controller {
	return &Controller{
		mgr:       mgr,
		setting:   s,
		configDir: configDir,
		runningID: StoppedID,
	}
}

// SetCorePath sets the sing-box executable path.
func (c *Controller) SetCorePath(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.corePath = p
}

// CorePath returns the configured core path.
func (c *Controller) CorePath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.corePath
}

// SetSetting swaps the settings this controller builds configs from.
func (c *Controller) SetSetting(s *setting.Setting) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setting = s
}

// SetManager swaps the profile store. A refresh rebuilds the manager, so a
// controller that survives the refresh has to follow the new one.
func (c *Controller) SetManager(mgr *db.Manager) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mgr = mgr
}

// RunningID returns the running profile id, or StoppedID.
func (c *Controller) RunningID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runningID
}

// PID returns the pid of the running core, or 0 when none is up. The supervisor reads
// it when it marks a stop as expected: the mark names the process it is about, so the
// exit it answers can be told from a crash of anything else.
func (c *Controller) PID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// SwitchedTo records that the running config is now being used for another profile, even
// though the core was not restarted: the hot-switch path moves a selector inside it, and
// the process — together with the config it read once — stays exactly as it was (see
// HotNodes).
//
// runningID is all that changes here. There is no pid file to rewrite, no wait goroutine
// to move and no start to log, because no process started. What it fixes is what every
// caller reads: RunningID is how this app answers "which node is up", and a switch that
// left it alone would have the window reporting the old node for as long as the core ran.
func (c *Controller) SwitchedTo(id int) {
	c.mu.Lock()
	c.runningID = id
	c.mu.Unlock()
}

// Logs returns a copy of the recent core output.
func (c *Controller) Logs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.logs))
	copy(out, c.logs)
	return out
}

// ClearLogs empties the log buffer.
func (c *Controller) ClearLogs() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = nil
}

// Start builds a config for the profile and launches the core.
// HotNodes reports what the running config can be switched to without being rebuilt: a map
// of profile id to the outbound tag that profile answers to inside it. Empty is a
// legitimate answer rather than a failure — a config with no selector has nothing to
// switch between, and the caller then starts the core again as it always did.
func (c *Controller) HotNodes() map[int]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hotNodes == nil {
		return nil
	}
	// A copy: the caller uses this while the core may be starting or stopping, and a map
	// handed out by reference would be mutated under it.
	out := make(map[int]string, len(c.hotNodes))
	for k, v := range c.hotNodes {
		out[k] = v
	}
	return out
}

// RunningConfig returns the config the running core was started with, or nil when
// no core is running.
//
// It is the map itself, not a copy: the config is not written to after the launch,
// and the caller only reads it — a deep copy of a whole sing-box config would be
// paid for nothing.
func (c *Controller) RunningConfig() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runningID == StoppedID {
		return nil
	}
	return c.runningConfig
}

func (c *Controller) Start(id int) error {
	// One short locked read for the state the build needs — SetManager /
	// SetSetting swap them from another goroutine (a reload that keeps the
	// core), so wide reads outside the lock would race the swap.
	c.mu.Lock()
	mgr, set := c.mgr, c.setting
	c.mu.Unlock()
	if mgr == nil || set == nil {
		return fmt.Errorf("controller not initialised")
	}
	// The config is built before the supervisor lock: building walks the profile
	// store and can take real time, and holding the lock through it queues every
	// Stop / RunningID / Logs behind it for that whole stretch.
	// A snapshot rather than the live entity: BuildConfig walks the bean map and
	// the group it belongs to, and both are mutated under the manager's lock by
	// anything else on this machine (a speed test writing a result, an edit
	// saving) while this build is under way — an unlocked walk against a locked
	// writer is a concurrent map access, which kills the process with no log line
	// to explain it. The id is still the live one; only the copy is walked.
	ent := mgr.SnapshotProfile(id)
	if ent == nil {
		return fmt.Errorf("profile %d not found", id)
	}
	res, err := configbuilder.BuildConfig(ent, mgr, set, false)
	if err != nil {
		c.appendLog(fmt.Sprintf("failed to build config for [%s]: %v", ent.DisplayName(), err))
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.runningID != StoppedID {
		return fmt.Errorf("a profile is already running")
	}
	return c.startLocked(id, res, ent)
}

// StartWithConfig launches the core with a config the caller built. VPN mode
// needs this: the inbound type is decided by the config (tun vs mixed), and
// BuildConfig only ever produces the port one.
func (c *Controller) StartWithConfig(id int, res *configbuilder.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runningID != StoppedID {
		return fmt.Errorf("a profile is already running")
	}
	return c.startLocked(id, res, c.mgr.SnapshotProfile(id))
}

// startLocked runs the shared launch path. The caller holds c.mu.
func (c *Controller) startLocked(id int, res *configbuilder.Result, ent *db.ProxyEntity) (err error) {
	cfgPath, err := c.writeConfig(id, res.Config)
	if err != nil {
		c.appendLogLocked("failed to write core config: " + err.Error())
		return err
	}
	// What this config can be switched between, kept for the caller that may want to move
	// between two of them without restarting anything: see HotNodes. Recorded before the
	// launch rather than after, and that is safe because a failed launch leaves runningID
	// at StoppedID — which is what every caller checks first.
	c.hotNodes = res.HotNodes
	// The config itself, for the same reason and with the same safety: a caller that
	// wants to know whether the core it has is the one the current settings and mode
	// would start has nothing to compare against without it.
	c.runningConfig = res.Config
	// Not so safe after all for HotNodes' other reader: runningID never moves on
	// a failed launch, but tryHotSwitch compares the hot snapshot before it asks
	// about the running id — a stale table after a failed start let that path
	// name a node of a config nothing is running and PUT the selection to the
	// clash API port, which another process may well be holding (the exact
	// hazard wait() clears its own copy for). Any error below takes the two
	// fields back with it, the same bookkeeping wait() does.
	launched := false
	defer func() {
		if err != nil && !launched {
			c.hotNodes = nil
			c.runningConfig = nil
		}
	}()

	exe, err := c.resolveCore()
	if err != nil {
		c.appendLogLocked("failed to start core: " + err.Error())
		return err
	}

	// tun mode needs the wintun driver next to the core; sing-box would fail
	// with a message about a missing DLL that says nothing about where to put it
	if res.NeedVPN && !WintunReady(exe, c.configDir) {
		err := fmt.Errorf("未找到 wintun.dll：TUN 模式需要它，请把 wintun.dll 放到 %s（或核心目录 %s）后重试",
			c.configDir, filepath.Dir(exe))
		c.appendLogLocked(err.Error())
		return err
	}

	// Reap this app's own leftovers, but only when the port is actually taken.
	// The single-instance lock is scoped to the executable name, so the dev build
	// and the packaged build can run side by side — and they share this pid file.
	// Killing its contents unconditionally would shoot down the other copy's
	// running core for no reason at all whenever the two use different ports.
	if portInUse(c.setting.CoreBoxClashAPI) {
		if stale := readPIDFile(c.pidPath()); clearStaleCore(stale, exe) {
			c.appendLogLocked(fmt.Sprintf("<<< 已结束上次残留的核心进程 (pid %d)", stale))
			c.waitPortFree(c.setting.CoreBoxClashAPI)
		}
	}

	if err := c.checkClashAPI(exe); err != nil {
		c.appendLogLocked(err.Error())
		return err
	}

	cmd := exec.Command(exe, "run", "-c", cfgPath)
	cmd.Dir = c.configDir
	w := &lineWriter{fn: c.appendLog}
	cmd.Stdout = w
	cmd.Stderr = w
	// The console the child would otherwise put up (a console program started by
	// a GUI one gets a window of its own). The setting — the TUN card's "隐藏核
	// 心控制台" — is what the reader toggles it with: hidden by default, shown
	// when they uncheck it to watch the core's own window while debugging.
	if c.setting.VPNHideConsole {
		util.HideConsole(cmd)
	}

	if err := cmd.Start(); err != nil {
		c.appendLogLocked("failed to start core: " + err.Error())
		return fmt.Errorf("failed to start core: %w", err)
	}
	launched = true

	c.cmd = cmd
	c.runningID = id
	// A new core is not a stopped one: the marker is about this process.
	c.stopRequested.Store(false)
	name := ""
	if ent != nil {
		name = fmt.Sprintf(" for [%s] %s", ent.DisplayType(), ent.DisplayName())
	}
	c.appendLogLocked(fmt.Sprintf(">>> started core (pid %d)%s", cmd.Process.Pid, name))
	// the OS takes it from here on a crash; the pid file covers a previous run
	if err := trackCoreProcess(cmd.Process.Pid); err != nil {
		c.appendLogLocked("failed to bind core to job object: " + err.Error())
	}
	c.writePIDFile(cmd.Process.Pid)

	// The core is not up when the process is: it reads its config, opens its
	// inbounds and only then prints "started". The supervisor is started first so
	// that a core which dies during the wait is noticed at once, and the waiter
	// below records when the inbound answers — in the background, because the
	// start itself must not wait for the core's boot: the reader is not held for
	// a second and a half, and what the app arms on the strength of a start asks
	// the flag instead.
	exited := make(chan struct{})
	go c.wait(cmd, id, exited)
	go c.watchInbound(cmd, mixedInboundPort(res.Config), exited)

	c.startExternals(res.Externals)

	return nil
}

// mixedInboundPort returns the port the core's mixed inbound listens on, or 0
// when the config carries none.
//
// The port is read out of the config that was written rather than out of the
// setting: the config is what the core was told to do, and the setting is what
// the reader asked for. They agree today; the config is the one that is true.
func mixedInboundPort(cfg map[string]interface{}) int {
	inbounds, _ := cfg["inbounds"].([]interface{})
	for _, in := range inbounds {
		m, ok := in.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); t != "mixed" {
			continue
		}
		switch p := m["listen_port"].(type) {
		case int:
			return p
		case float64:
			return int(p)
		}
	}
	return 0
}

// inboundReadyTimeout is how long the waiter gives the core to open its inbound.
const inboundReadyTimeout = 30 * time.Second

// watchInbound waits for the core's mixed inbound to answer and records that it
// does.
//
// The wait ends when the process is reaped: a core that is gone will never
// answer, and waiting out the timeout for it would only delay the report.
func (c *Controller) watchInbound(cmd *exec.Cmd, port int, exited <-chan struct{}) {
	if port <= 0 {
		return
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(inboundReadyTimeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			c.mu.Lock()
			// Only the core that is still up may claim the readiness: a stop and a
			// start in quick succession leave two waiters, and the older one must
			// not say "ready" for a process that has been replaced.
			if c.cmd == cmd {
				c.inboundReady = true
			}
			c.mu.Unlock()
			return
		}
		select {
		case <-exited:
			return
		default:
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// InboundReady reports whether the core's mixed inbound is answering right now.
//
// It is a flag and not a dial on purpose: the app asks it on a one-second tick
// (the DNS takeover is decided there), and a dial would be a connection the core
// logs and the clash API counts as one more reader's connection.
func (c *Controller) InboundReady() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inboundReady
}

// Stop terminates the running core and any external cores.
//
// It does not return until the supervisor goroutine has observed the exit.
// Kill only asks the OS to terminate the process, while runningID is cleared
// later by wait(); returning straight away made a stop-then-start sequence
// (switching profiles) race that window and fail with "a profile is already
// running", or leave the dying process holding the inbound port.
func (c *Controller) Stop() error {
	c.mu.Lock()
	cmd := c.cmd
	exts := c.extCmds
	c.extCmds = nil
	// The config is going away with the process: there is nothing left to switch between.
	c.hotNodes = nil
	// And the config the process was running goes with it: there is no core left
	// to compare anything against.
	c.runningConfig = nil
	// And nothing can be handed to it any more.
	c.inboundReady = false
	c.mu.Unlock()
	// Set before the kill and read by the supervisor when the process is reaped:
	// it is what tells a stop from a crash.
	c.stopRequested.Store(true)

	for _, e := range exts {
		if e.Process != nil {
			_ = e.Process.Kill()
			// Reaped here because nothing else ever does. startExternals only Starts;
			// an exec.Cmd that is never Waited keeps its process handle and the
			// goroutines copying its output alive for the life of the app — a handful
			// per core restart, and a day of switching nodes is thousands of handles.
			_ = e.Wait()
		}
	}
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	c.appendLog("<<< stopping core")
	// The kill's own error is kept, but it is not the end of the story. On
	// Windows, terminating a process that has already ended — but not yet been
	// reaped — fails, and returning that error straight away skipped the wait
	// below. The caller then read a stopped core as a failed stop, and the
	// system proxy was left pointing at a port nothing was listening on: a
	// machine with no internet until the app was restarted.
	killErr := cmd.Process.Kill()
	c.clearPIDFileIf(cmd.Process.Pid)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// One locked read per iteration: wait() clears both under the same
		// lock, and reading them across two lock-free calls raced its write.
		c.mu.Lock()
		gone := c.cmd == nil && c.runningID == StoppedID
		c.mu.Unlock()
		if gone {
			// The process is gone. "already finished" is not a failure — the
			// node really did stop.
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if killErr != nil {
		return killErr
	}
	// The kill did not take (or the supervisor is wedged): the state stays
	// "running", which is what a caller's next Start must still be told — the
	// silent nil used to read as success and the following start failed with a
	// bare "already running" instead of the actual reason.
	c.appendLog("core did not exit within 3s")
	return fmt.Errorf("核心进程 3 秒内未退出，请稍后重试或在任务管理器中结束 sing-box")
}

func (c *Controller) startExternals(specs []*configbuilder.ExternalSpec) {
	for _, spec := range specs {
		if spec.Program == "" {
			continue
		}
		// The path was written by the reader and may be relative. It is taken
		// against the folder the executable sits in — the same rule the data
		// directory and the core path follow — never against the working
		// directory, so a copy of the folder keeps finding its sidecar cores
		// however it is started.
		cmd := exec.Command(util.ResolveAppPath(spec.Program), spec.Args...)
		cmd.Dir = c.configDir
		if len(spec.Env) > 0 {
			cmd.Env = append(os.Environ(), spec.Env...)
		}
		util.HideConsole(cmd)
		tag := spec.Tag
		w := &lineWriter{fn: func(line string) { c.appendLog("[" + tag + "] " + line) }}
		cmd.Stdout = w
		cmd.Stderr = w
		if err := cmd.Start(); err != nil {
			c.appendLogLocked("failed to start external core " + tag + ": " + err.Error())
			continue
		}
		_ = trackCoreProcess(cmd.Process.Pid)
		c.appendLogLocked("started external core " + tag + " (pid " + strconv.Itoa(cmd.Process.Pid) + ")")
		c.extCmds = append(c.extCmds, cmd)
	}
}

func (c *Controller) wait(cmd *exec.Cmd, id int, exited chan struct{}) {
	err := cmd.Wait()
	// Closed before the lock: the waiter for the core's inbound asks it whether
	// the core is gone, and it must be able to hear that without waiting for a
	// mutex this goroutine is about to take.
	close(exited)

	c.mu.Lock()
	c.cmd = nil
	// The core is gone, so nothing can be handed to it.
	c.inboundReady = false
	// The config went with the process: there is nothing left to switch
	// between. A stale table let a hot switch name a node of a config that is
	// no longer running — and the switch was a PUT to the clash API port,
	// which another process may well have been holding.
	c.hotNodes = nil
	// The config went with the process: there is nothing left to compare a mode
	// against, and a stale one would say the core is serving a config it is not.
	c.runningConfig = nil
	// The node that was up when the process died, which is not necessarily the
	// one this launch was given: a hot switch moves the selector inside the
	// running core and SwitchedTo moves runningID with it, while this goroutine
	// still holds the id startLocked captured. Reporting the stale id had the
	// supervisor restart the node the reader had already switched away from.
	lastID := c.runningID
	c.runningID = StoppedID
	exts := c.extCmds
	c.extCmds = nil
	// the bookkeeping runs out with the process; a stale file would point the
	// next launch at whatever pid happens to be reused for
	c.clearPIDFileIf(cmd.Process.Pid)
	c.mu.Unlock()

	for _, e := range exts {
		if e.Process != nil {
			_ = e.Process.Kill()
			// Same reason as Stop's loop: this is the only place they are reaped.
			_ = e.Wait()
		}
	}

	if err != nil {
		if c.stopRequested.Load() {
			// The stop asked for it. Windows reports a killed process as an error
			// exit, and "core exited: exit status 1" reads like a crash of a core
			// that was working a moment ago.
			c.appendLog("<<< core stopped")
		} else {
			c.appendLog(fmt.Sprintf("core exited: %v", err))
			c.hintIfBindFailed()
		}
	} else {
		c.appendLog("core exited")
	}
	if lastID != id {
		// Said out loud because it is the one case where the two ids disagree,
		// and the difference is what the supervisor's restart is about: the
		// reader switched nodes inside the running core, then the core died.
		c.appendLog(fmt.Sprintf("core exited after a hot switch (started for %d, was serving %d)", id, lastID))
	}
	if c.OnExit != nil {
		c.OnExit(lastID, cmd.Process.Pid)
	}
}

// bindErrorHints are the fragments a core prints when it cannot take its
// listening port. The probe in Start can pass while this still fails — a
// TIME_WAIT socket or a reserved port range refuses this app's plain bind just
// as it refuses the core's — and the raw line then reads like a random startup
// failure rather than a port problem.
var bindErrorHints = []string{
	"bind:",
	"address already in use",
	"Only one usage of each socket address",
}

// hintIfBindFailed adds one readable line after a core dies on its port, so the
// user is told what to do instead of being left with a bare bind error.
func (c *Controller) hintIfBindFailed() {
	// The port is a field SetSetting may swap from another goroutine, so it is
	// read under the lock like the log tail below it; the blocking port probe
	// itself stays lock-free, between the two sections.
	c.mu.Lock()
	port := c.setting.CoreBoxClashAPI
	c.mu.Unlock()
	if !portInUse(port) {
		return
	}
	c.mu.Lock()
	start := len(c.logs) - 12
	if start < 0 {
		start = 0
	}
	tail := strings.Join(c.logs[start:], "\n")
	c.mu.Unlock()

	for _, h := range bindErrorHints {
		if strings.Contains(tail, h) {
			c.appendLog(fmt.Sprintf("提示: Clash API 端口 %d 无法绑定 —— 常见原因是核心刚退出、端口还在系统回收期（约 30 秒后自动可用），或该端口段被 Hyper-V/WSL/Docker 保留。稍等重试，或在设置里换一个 Clash API 端口。", port))
			return
		}
	}
}

func (c *Controller) writeConfig(id int, cfg map[string]interface{}) (string, error) {
	dir := filepath.Join(c.configDir, "temp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("core-config-%d.json", id))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	// The others go. One file per profile id, written on every start and removed by
	// nobody, was a folder that only ever grew — and they are not inert: a generated
	// config carries the node's address, its uuid and its password in clear text, and
	// the leftovers sat there for every node that had ever run. Nothing reads one back
	// (each start writes its own before launching), so the sweep costs a few stats and
	// takes the whole history with it.
	if old, err := filepath.Glob(filepath.Join(dir, "core-config-*.json")); err == nil {
		for _, p := range old {
			if p != path {
				_ = os.Remove(p)
			}
		}
	}
	return path, nil
}

// ResolveCore returns the configured sing-box path or auto-detects a binary.
func ResolveCore(corePath, baseDir string) (string, error) {
	if corePath != "" {
		if _, err := os.Stat(corePath); err != nil {
			return "", fmt.Errorf("core not found at %s", corePath)
		}
		return corePath, nil
	}
	name := "sing-box"
	if runtime.GOOS == "windows" {
		name = "sing-box.exe"
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(d, name),
			filepath.Join(d, "..", name),
		)
		// the repo keeps the core in a sibling "sing-box/" folder, and the dev
		// binary lives in build/bin, so walk a couple of levels up as well
		candidates = append(candidates,
			filepath.Join(d, "sing-box", name),
			filepath.Join(d, "..", "sing-box", name),
			filepath.Join(d, "..", "..", "sing-box", name),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, name),
			filepath.Join(wd, "sing-box", name),
		)
	}
	if baseDir != "" {
		// the data dir is <root>/config, so the core is often <root>/sing-box
		root := filepath.Dir(baseDir)
		candidates = append(candidates,
			filepath.Join(baseDir, name),
			filepath.Join(baseDir, "..", name),
			filepath.Join(root, "sing-box", name),
		)
	}
	for _, cand := range candidates {
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("sing-box core not found, tried: %s", strings.Join(candidates, ", "))
}

// resolveCore returns the configured core path or auto-detects a sing-box binary.
func (c *Controller) resolveCore() (string, error) {
	return ResolveCore(c.corePath, c.configDir)
}

// checkClashAPI verifies the external controller port is free before the core is
// launched. A core left behind by a crashed run keeps holding the port, and
// sing-box then aborts at startup with a bare "bind: Only one usage of each
// socket address", which reads like a random failure: the process dies right
// after spawn, so the status bar flashes and falls back to "stopped".
//
// A restart does not always clear it — the squatter may be a core this app never
// started, or one belonging to another copy running alongside this one — so the
// port is resolved back to the process holding it. If it is a copy of the core
// this app is configured to run, it is ended here; anything else is named in the
// error instead of being guessed at.
func (c *Controller) checkClashAPI(coreExe string) error {
	port := c.setting.CoreBoxClashAPI
	if !portInUse(port) {
		return nil
	}
	pid, who := c.displaceListener(port, coreExe)
	if pid > 0 {
		msg := fmt.Sprintf("Clash API 端口 %d 被 %s (pid %d) 占用，无法自动结束该进程", port, who, pid)
		c.appendLogLocked(msg + "；请手动结束它，或在设置里换一个 Clash API 端口")
		return errors.New(msg)
	}
	if !portInUse(port) {
		return nil
	}
	// The port cannot be bound, yet nothing is listening on it. That is not a
	// stray process anyone could end, and refusing to start on it only produced
	// a message about a process that does not exist:
	//   - the socket may be in the system's TIME_WAIT recycle, because the
	//     listener that just died was serving live connections when it was
	//     killed (the Clash API gets polled, so almost always);
	//   - or the port sits in a range reserved by Hyper-V/WSL/Docker, where bind
	//     fails with no listener anywhere.
	// Either way the core is given its chance: sing-box may bind where this
	// plain probe cannot. If it cannot, the log now says which one it was.
	c.appendLogLocked(fmt.Sprintf("Clash API 端口 %d 暂时无法绑定（无进程在监听，可能处于系统回收期或被系统保留），继续尝试启动核心", port))
	if lines := portLines(port); len(lines) > 0 {
		for _, l := range lines {
			c.appendLogLocked("[port] " + l)
		}
	} else {
		c.appendLogLocked("[port] netstat 未列出该端口：通常意味着端口段被 Hyper-V/WSL/Docker 保留了（此时没有任何进程可结束，换个端口即可）")
	}
	return nil
}

// displaceListener ends the process listening on port when it is a copy of this
// app's core, and waits for the port to come free. It returns the pid that still
// holds the port together with a name for it, or zero once the port is clear.
func (c *Controller) displaceListener(port int, coreExe string) (int, string) {
	for _, pid := range listenersOnPort(port) {
		if !clearStaleCore(pid, coreExe) {
			continue
		}
		c.appendLogLocked(fmt.Sprintf("<<< 已结束占用端口 %d 的核心进程 (pid %d)", port, pid))
		if c.waitPortFree(port) {
			return 0, ""
		}
	}
	// still held: report who, so the message stops being a guess. Anything we
	// dared not touch lands here too, and the user decides.
	for _, pid := range listenersOnPort(port) {
		if image := processExe(pid); image != "" {
			return pid, filepath.Base(image)
		}
		return pid, "未知进程"
	}
	return 0, "未知进程"
}

// waitPortFree polls the port briefly after a kill: the listening socket is
// released asynchronously, so probing straight away would race the cleanup.
func (c *Controller) waitPortFree(port int) bool {
	for i := 0; i < 20; i++ {
		if !portInUse(port) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !portInUse(port)
}

// portInUse reports whether something is already listening on the local port.
func portInUse(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		// the bind failed, so something (or a recycle window) holds the port
		return true
	}
	// The bind succeeded: nothing is listening. `Close() == nil` read as
	// "in use" instead — plain binds nearly never fail to close, so every
	// probe answered "taken", waitPortFree burned its full second and the
	// start path paid the netstat walk every time.
	_ = ln.Close()
	return false
}

// pidPath is where the running core's pid is kept between runs. That memory is
// what lets the next launch recognise the core it left behind and end it, and
// nothing else in this file can find that process on its own: Windows offers no
// portable way from a listening port back to its owner.
func (c *Controller) pidPath() string {
	return filepath.Join(c.configDir, "temp", "core.pid")
}

func (c *Controller) writePIDFile(pid int) {
	_ = os.WriteFile(c.pidPath(), []byte(strconv.Itoa(pid)), 0o644)
}

// clearPIDFileIf drops the recorded pid, but only when the file still holds
// this very pid: two copies of the app (dev and packaged) share the path, and
// whichever exits last must not delete the record of the copy still running.
func (c *Controller) clearPIDFileIf(pid int) {
	if readPIDFile(c.pidPath()) != pid {
		return
	}
	_ = os.Remove(c.pidPath())
}

func readPIDFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func (c *Controller) appendLog(line string) {
	c.mu.Lock()
	c.appendLogLocked(line)
	c.mu.Unlock()
}

// appendLogLocked appends a line while the caller already holds c.mu.
//
// Start keeps the lock for its whole body, so it must not call appendLog:
// sync.Mutex is not reentrant, so that self-deadlocks. The deadlock fired right
// after the core process had already been spawned, which left Start hanging
// forever — no "started core" line, no return to the frontend, and RunningID
// blocked behind the same mutex, so the status bar stayed empty.
func (c *Controller) appendLogLocked(line string) {
	// the core may colour its output; the escapes are meaningless in a log file
	// and in the UI, and they survive a clipboard copy as stray control bytes
	line = util.StripANSI(line)
	// The ignore list is the reader's noise filter (setting 的 log_ignore):
	// a line carrying any entry as a substring never enters the buffer, the
	// live stream or the file — the three readers of this one path. Read live
	// from the setting, so a save takes effect on the next line without a
	// restart.
	if c.logIgnored(line) {
		return
	}
	// Stamped here, once, for every reader of this line: this buffer is what the
	// panel reloads from, and OnLog carries the same string to the file and to the
	// live stream. Stamping the buffer instead of its readers is what keeps a line
	// timed the same before and after a reload — it used to be timed only in the
	// file, so the panel showed the same line with and without a timestamp
	// depending on whether it had just streamed in or been read back.
	stamped := logstore.Stamp(line)
	c.logs = append(c.logs, stamped)
	if n := c.logCap(); len(c.logs) > n {
		c.logs = c.logs[len(c.logs)-n:]
	}
	if c.OnLog != nil {
		c.OnLog(stamped)
	}
}

// logCap is the in-memory log buffer's size: MaxLogLine from the settings (the
// reader's "内存日志行数"), with a floor — a cap that small would forget what
// the panel is showing before the reader has read it — and a fallback for a
// settings file that carries nothing sensible in the field.
func (c *Controller) logCap() int {
	if n := c.setting.MaxLogLine; n > 0 {
		if n < 50 {
			return 50
		}
		return n
	}
	return 500
}

// logIgnored reports whether a line matches the ignore list. Match is a
// case-insensitive substring: the reader filters log noise ("dns", "TLS
// handshake") rather than whole formats, and a case-sensitive match would send
// them back to the settings twice for one line of noise.
func (c *Controller) logIgnored(line string) bool {
	list := c.setting.LogIgnore
	if len(list) == 0 {
		return false
	}
	low := strings.ToLower(line)
	for _, pat := range list {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if strings.Contains(low, strings.ToLower(pat)) {
			return true
		}
	}
	return false
}

// lineWriter splits written bytes into lines and forwards them. os/exec runs a
// separate copier goroutine for stdout and stderr, both writing here at the
// same time, so the buffer is guarded.
type lineWriter struct {
	mu  sync.Mutex
	fn  func(string)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	var lines []string
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, string(bytes.TrimRight(w.buf[:i], "\r")))
		w.buf = w.buf[i+1:]
	}
	w.mu.Unlock()

	// forward outside the lock: the callback writes logs to disk.
	for _, line := range lines {
		w.fn(line)
	}
	return len(p), nil
}
