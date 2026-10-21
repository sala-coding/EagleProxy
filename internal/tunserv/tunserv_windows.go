//go:build windows

// Package tunserv is the resident owner of the TUN front-end: a small process
// the scheduled task starts at highest privileges once, which spawns (and
// supervises) the elevated sing-box front-end on command — so a connect costs
// no UAC prompt and no elevation round-trip. It carries the same promise
// karing keeps with its own resident service: the adapter's owner is not tied
// to the UI process lifetime.
//
// The contract is files under <dataDir>/temp, the folder the app already
// books its TUN state in:
//
//	tunserve.alive     heartbeat, rewritten every 2s — the app reads its
//	                   mtime to learn the server lives (no ports);
//	tunserve.cmd.json  the command slot: the app posts one, the server claims it
//	                   with a rename and runs it (the claim is atomic, so a
//	                   command posted while the server is reading the previous
//	                   one is never deleted unread; posts wait for the slot to
//	                   be free, so the two are executed in the order sent);
//	tun-front.pid      the pid book the app already uses, now owned by the
//	                   server while its child runs. The record carries the
//	                   front config's mtime so adoption can tell a reusable
//	                   front-end from a stale one.
//
// Every message is one-directional and stateless: the app learns only "alive
// / dead / written", and whatever the file system says is the whole truth —
// which is what keeps a UI crash from corrupting the server's state.
package tunserv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eagle/internal/vpn"
)

// File names, all under the data directory's temp folder the app manages.
const (
	heartName = "tunserve.alive"
	cmdName   = "tunserve.cmd.json"
)

// cmdWriteMu serialises the senders in this process, and cmdWriteSeq numbers
// their temp files; between them no two posts can share a temp name or slip
// past each other's wait (see replaceCommandLocked).
var (
	cmdWriteMu  sync.Mutex
	cmdWriteSeq atomic.Int64
)

// Action values for Command.
const (
	ActionStart = "start"
	ActionStop  = "stop"
	ActionExit  = "exit"
)

// FrontPID is the pid file's record. ConfigHash stamps the front config's
// CONTENT (sha256), so a later run tells "same config, reusable" from "config
// moved on" even though writeTunFrontConfig rewrites the file on every TUN
// enable — an mtime stamp would make every rewrite look like a change, and the
// reuse would never fire exactly when it is wanted.
type FrontPID struct {
	PID        int    `json:"pid"`
	Server     bool   `json:"server"`
	Config     string `json:"config"`
	ConfigHash string `json:"config_hash"`
	// Written is when the record was made (server-side), for readers that want
	// freshness rather than identity.
	Written string `json:"written,omitempty"`
}

// Command is one instruction in the command slot; exactly one action per file.
type Command struct {
	Action string `json:"action"`
	Config string `json:"config,omitempty"`
	Core   string `json:"core,omitempty"`
}

func pidPath(dataDir string) string   { return filepath.Join(dataDir, "temp", "tun-front.pid") }
func heartPath(dataDir string) string { return filepath.Join(dataDir, "temp", heartName) }
func cmdPath(dataDir string) string   { return filepath.Join(dataDir, "temp", cmdName) }

// FrontPIDOf reads the pid record, bare-integer legacy files included.
func FrontPIDOf(dataDir string) *FrontPID {
	data, err := os.ReadFile(pidPath(dataDir))
	if err != nil {
		return nil
	}
	var info FrontPID
	if err := json.Unmarshal(data, &info); err == nil && info.PID > 0 {
		return &info
	}
	if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 {
		return &FrontPID{PID: pid}
	}
	return nil
}

// WriteFrontPID is the only writer of the pid record; both the app and the
// server call it, because the file is a lease, not private memory.
//
// Tmp + rename, the same as WriteState: the record is written from one process
// while the other reads it, and a bare WriteFile is a truncate-then-write — a
// reader landing in the gap unmarshals nothing, answers "no front-end", and
// skips the cleanup or adoption it was about to do (FrontPIDOf falls through
// to nil).
func WriteFrontPID(dataDir string, info FrontPID) error {
	info.Written = nowStamp()
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return writeFileAtomicPID(dataDir, data)
}

// writeFileAtomicPID writes the pid record through a unique sibling and renames
// it over the target. The temp name carries this process's pid so two writers
// (app and server) never share one temp file.
func writeFileAtomicPID(dataDir string, data []byte) error {
	path := pidPath(dataDir)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ClearFrontPID removes the pid record. The server clears it when its child
// is gone; the app clears it when its books also say so.
func ClearFrontPID(dataDir string) { _ = os.Remove(pidPath(dataDir)) }

// ServerAlive reports a server heart within the last 12s. Two missed beats at
// the 2s period still read as alive; three do not.
func ServerAlive(dataDir string) bool {
	info, err := os.Stat(heartPath(dataDir))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 12*time.Second
}

// appHeartName is the app's heartbeat, the mirror of this server's own: the UI process
// stamps it every two seconds for as long as it lives, and the server reads only its age.
// That age is the one piece of evidence that tells a UI between two beats from one that
// has crashed — a crash skips every defer the app owns, so nothing on that side can
// announce it, and this process is the only party left that can notice.
const appHeartName = "tunserve.app.alive"

// appHeartGoneAfter is how stale the app's stamp may be before the server treats the UI
// as gone. Six missed beats at the two-second period; the same budget ServerAlive gives
// the app in the other direction, for the same reason.
const appHeartGoneAfter = 12 * time.Second

func appHeartPath(dataDir string) string { return filepath.Join(dataDir, "temp", appHeartName) }

// WriteAppHeartbeat stamps the app's liveness. Called from the app's own loop; the server
// never writes this file, the same way the app never writes the server's.
func WriteAppHeartbeat(dataDir string) error {
	if dataDir == "" {
		return os.ErrInvalid
	}
	dir := filepath.Join(dataDir, "temp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The pid travels with the stamp so the reader can ask the process itself rather
	// than a clock (see AppHeartStale): a sleeping machine stops the beats without the
	// app being gone.
	return os.WriteFile(appHeartPath(dataDir), []byte(fmt.Sprintf("%d\n%s", os.Getpid(), nowStamp())), 0o644)
}

// ClearAppHeartbeat drops the stamp on the app's way out. An exit that ran leaves nothing
// for the server to misread; an exit that did not is exactly what the stamp is for.
func ClearAppHeartbeat(dataDir string) { _ = os.Remove(appHeartPath(dataDir)) }

// AppHeartStale reports whether the app's stamp has stopped moving, which is the server's
// answer to "did the UI crash".
//
// An absent file answers false, deliberately: a server that has never seen a heartbeat is
// a server with no evidence of a crash, and the safe reading of no evidence is to leave
// the machine's resolvers alone.
func AppHeartStale(dataDir string) bool {
	data, err := os.ReadFile(appHeartPath(dataDir))
	if err != nil {
		return false
	}
	// The pid is the answer no clock can give. A laptop that slept, a process suspended
	// by a debugger or a machine under a long freeze all stop the beats for far longer
	// than the budget while the app is very much alive — and killing its front-end and
	// clearing the resolvers for that is worse than the crash this exists to clean up
	// after. A live pid ends the question here.
	if pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])); err == nil && pid > 0 {
		if vpn.IsRunning(pid) {
			return false
		}
		return true
	}
	// An older stamp without a pid: the clock is all there is.
	info, err := os.Stat(appHeartPath(dataDir))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > appHeartGoneAfter
}

// ServerLogStamp is when the server last wrote a line of its own log, and the zero time
// when it has no log at all. The folder is the one the app decided on (see
// appPaths.logDir).
//
// Why the app asks: a /Run that starts nothing answers exactly like one that starts a
// slow server — success from schtasks, silence from the heartbeat — and a task can point
// at an executable that no longer exists (a moved folder, a replaced build) for months
// without saying so. The server's first log line is written the moment its process exists,
// before its first beat, so it is the earliest evidence a cold start produces and the one
// that separates "still starting" from "never started" (see ensureTunServer).
func ServerLogStamp(logDir string) time.Time {
	info, err := os.Stat(filepath.Join(logDir, "tunserve.log"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// SendCommand posts one instruction, and waits for the slot to be free first.
//
// Write to a temp name, then rename: a reader never sees a truncated command, and
// a crash on the writer side never resurrects an old one. The wait is what makes
// the slot a queue rather than a place where the newest command wins: the server
// drains it on a quarter-second tick, and a second command posted inside that
// window used to overwrite the first before anybody had read it. A stop lost under
// a start left the previous front-end holding the adapter; a start lost under a
// stop left the app waiting out a twenty-second timeout for a child that was never
// going to be raised, and then raising one itself. Both shapes are what the reader
// meets as "TUN sometimes just does not come up".
//
// The wait is bounded on purpose: a wedged (or dead) server must not hold a caller
// forever, and by then whatever was in the slot is a command nothing is coming to
// read, so the newer one is written over it. Use ReplaceCommand where the older
// command is deliberately being cancelled — that is a different intent, and only
// one call site has it (the fallback that gives up on the server and starts the
// front-end itself).
func SendCommand(dataDir string, cmd Command) error {
	// The wait is inside the sender lock, not before it: two senders both
	// passing waitSlotFree and then writing in turn still loses the first
	// command — the second overwrites a slot the server has not read yet, which
	// is the exact "a stop lost under a start" this wait exists to prevent.
	cmdWriteMu.Lock()
	defer cmdWriteMu.Unlock()
	waitSlotFree(cmdPath(dataDir))
	return replaceCommandLocked(dataDir, cmd)
}

// ReplaceCommand posts an instruction over whatever is in the slot, without
// waiting for the server to have read it.
//
// This is the cancelling form. It exists for the one caller that has already
// decided the command in the slot should not run: the direct-launch fallback,
// where a start that timed out is still sitting there and a server reaching it
// later would raise a second front-end behind the one about to be started by
// hand.
func ReplaceCommand(dataDir string, cmd Command) error {
	cmdWriteMu.Lock()
	defer cmdWriteMu.Unlock()
	return replaceCommandLocked(dataDir, cmd)
}

// replaceCommandLocked is the write itself: marshal, write to a name no other
// writer can be using, rename over the slot. The temp name is unique per write
// (process pid + a sequence) rather than the fixed ".new" it replaced: two
// senders — the shutdown path and the stop/watchdog path, in this same process
// — both waiting for a free slot then writing the same fixed name truncated
// each other's half-written JSON, and the server's ConsumeCommand reads a torn
// payload as nothing and drops the command with no trace.
func replaceCommandLocked(dataDir string, cmd Command) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.new.%d.%d", cmdPath(dataDir), os.Getpid(), cmdWriteSeq.Add(1))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("tunserv: write command: %w", err)
	}
	if err := os.Rename(tmp, cmdPath(dataDir)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("tunserv: swap command: %w", err)
	}
	return nil
}

// commandSlotWait bounds how long a post waits for the reader's turn. The server
// takes what is in the slot within its 250ms tick, so this is slack for a slow
// machine, not a working timeout.
const commandSlotWait = 2 * time.Second

// waitSlotFree blocks until the slot is empty — the server has claimed whatever
// was in it — or the bound runs out.
func waitSlotFree(path string) {
	deadline := time.Now().Add(commandSlotWait)
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ConsumeCommand is the server-side read: take the current command (nil when the
// slot is empty) and clear the slot, so a slow loop can never act on it twice.
//
// The claim is a rename, not a read followed by a delete. Read-then-delete had a
// window in the middle of it: a command posted between the two was deleted
// unread, and its sender went on waiting for an effect that could no longer
// arrive. A rename takes the file away in one step, so whatever the writer put
// down after it is left where it is, for the next tick.
func ConsumeCommand(dataDir string) *Command {
	path := cmdPath(dataDir)
	taken := path + ".taken"
	if err := os.Rename(path, taken); err != nil {
		return nil
	}
	data, err := os.ReadFile(taken)
	_ = os.Remove(taken)
	if err != nil {
		return nil
	}
	var cmd Command
	if json.Unmarshal(data, &cmd) != nil || cmd.Action == "" {
		// A torn or foreign payload used to vanish here with nothing said: the
		// sender's post was simply gone, and both sides blamed their own
		// timeout. The length is the only thing worth quoting — the bytes may
		// be anything — and the file is already unreadable, so it goes.
		log("dropped an unreadable command from the slot (%d bytes)", len(data))
		return nil
	}
	// Start is remembered across one crash-restart window inside the server;
	// the caller decides which actions to re-fire after a child dies.
	return &cmd
}

// Heartbeat rewrites the heart file with a current timestamp inside, plus the
// timestamp of the binary this server runs from.
//
// The column is human-readable so a manual reader can tell "watched file" from
// "stale file" without a decoder; the second line is what tells the app whether
// this server is the build it expects. A server outlives the app that started
// it, so after an update the old one keeps running its old code — no adapter
// preparation, no front-end sweep — while the new app assumes both. Comparing
// the executable's own timestamp is enough to tell them apart, and it costs a
// stat on a file this process is already reading its own path from.
func Heartbeat(dataDir string) error {
	return os.WriteFile(heartPath(dataDir),
		[]byte(nowStamp()+"\n"+exeStamp()), 0o644)
}

// exeStamp is the modification time of this process's own executable, read
// once at the first call and never again.
//
// Read per call it lies in the one case it exists for: the file on disk is
// REPLACED while this old server is still running (a rebuild), its mtime
// changes, and every heartbeat then stamps the new build's time — the next
// app reads "same build" and the old server, wrong code and all, is never
// restarted. Stamped at first use, the column carries the binary this
// process actually runs, which is the identity the comparison is after.
// Empty when it cannot be read — in which case the app cannot tell one build
// from another and leaves the server alone.
var (
	exeStampOnce   sync.Once
	exeStampCached string
)

func exeStamp() string {
	exeStampOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		info, err := os.Stat(exe)
		if err != nil {
			return
		}
		exeStampCached = info.ModTime().Format(time.RFC3339Nano)
	})
	return exeStampCached
}

// ServerRunsSameBuild reports whether the live server was started from the same
// binary this process is running. False covers both "no usable heartbeat" and
// "a server from another build", and both callers want the same answer: a server
// that cannot answer for the current code is not one to hand work to.
func ServerRunsSameBuild(dataDir string) bool {
	data, err := os.ReadFile(heartPath(dataDir))
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return false
	}
	return strings.TrimSpace(lines[1]) == exeStamp()
}

// ClearHeartbeat drops the heart file when the server exits.
func ClearHeartbeat(dataDir string) { _ = os.Remove(heartPath(dataDir)) }

// TaskName derives the scheduled task's name. Dev builds get one of their own,
// the same way the single-instance lock does, so both life states can coexist.
// The suffix must match what the app's own devIDSuffix produces, but this
// package cannot import main — "dev" in the executable name is the same sign.
func TaskName(exePath string) string {
	name := "EagleTunFront"
	if filepath.Base(exePath) == "Eagle-dev.exe" {
		name += "-dev"
	}
	return name
}

// InstallTask registers the scheduled task that runs the server. Creating the
// task is the step that needs the administrator token — the ONE prompt — and
// runElevatedWait carries the "schtasks /Create" INSTEAD of spawning a shell
// for it, waiting for schtasks to exit so its nonzero answer is a real error
// rather than a success that was never checked.
//
// Exact wording matters here: /SC is what schtasks guards as mandatory, and a
// create without it answers "Invalid syntax" and never registers anything.
// ONCE with a long-gone date means the task is started only by explicit /Run —
// which is exactly how the app drives it; nothing runs on its own schedule.
// HIGHEST means every /Run starts with the token the adapter demands, and no
// prompt appears ever again.
func InstallTask(exePath string, runElevatedWait func(exe string, args []string, dir string) (int, error)) error {
	tr := strconv.Quote(exePath) + " --tun-serve"
	pid, err := runElevatedWait("schtasks.exe", []string{
		"/Create", "/F", "/SC", "ONCE", "/SD", "2000-01-01", "/ST", "00:00",
		"/TN", TaskName(exePath), "/TR", tr, "/RL", "HIGHEST",
	}, "")
	if err != nil {
		return err
	}
	_ = pid
	return nil
}

// RunTask asks Windows to start the server task. The registering user's tasks
// start on their say-so unelevated — this is the call that must never cost a
// prompt, or the whole design is void.
func RunTask(exePath string) error {
	return execute("schtasks.exe", []string{"/Run", "/TN", TaskName(exePath)})
}

// TaskInstalled answers whether the task with the current name exists. The
// _exist check costs one schtasks /Query; cheap enough to sit before every
// server-recovery path.
func TaskInstalled(exePath string) (bool, error) {
	out, err := captureQuery(exePath)
	if err != nil {
		return false, err
	}
	return containsTask(out, TaskName(exePath)), nil
}

func nowStamp() string { return time.Now().Format("2006-01-02 15:04:05.000") }

// stampLayout is how nowStamp writes Written, and the only shape FreshFrom reads.
const stampLayout = "2006-01-02 15:04:05.000"

// FreshFrom reports whether the record was written at or after t — the receipt
// test for a caller that sent a start command and wants the child raised by
// THAT command.
//
// Without it the book lies by omission: it names whichever front-end was last
// raised, and a start sent while an adopted one is still running reads the old
// record, then watches ITS death — killed by the very start it was waiting on —
// and reports the start a failure while the real child is still on its way.
func (f *FrontPID) FreshFrom(t time.Time) bool {
	if f == nil || f.Written == "" {
		// A bare-integer legacy record has no stamp at all: it cannot prove
		// freshness, so it never passes.
		return false
	}
	w, err := time.ParseInLocation(stampLayout, f.Written, time.Local)
	return err == nil && !w.Before(t)
}
