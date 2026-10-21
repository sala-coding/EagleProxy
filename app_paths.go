package main

import (
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/util"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type appConfig struct {
	DataDir  string `json:"data_dir"`
	CorePath string `json:"core_path"`
	// Where the logs are. The resident server is this same executable, so it
	// could work the folder out for itself — but only when the program's own
	// folder is writable (see setupLogDir), and the server has to be told which
	// one was chosen.
	LogDir string `json:"log_dir,omitempty"`
}

// appConfigPath is where the backend keeps the two paths the settings page edits: the
// data directory and the core executable. It lived under a nekobox-go folder when the
// program was NekoBox and is an eagle folder now, with the old one carried over once
// (see migrateAppConfig) so a reader who has moved their data somewhere keeps it.
func appConfigPath() string {
	return filepath.Join(appConfigDir(), "eagle", "app.json")
}

func appConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, _ = os.Getwd()
	}
	return dir
}

// migrateAppConfig copies nekobox-go/app.json to eagle/app.json the first time this
// build runs, and only when the new file is not there — the same once-and-only rule
// the settings store follows. The old copy is left where it is: it is two lines of
// paths, and leaving it costs nothing while removing it would strand anyone who
// rolls back to the older build.
func migrateAppConfig() {
	path := appConfigPath()
	if _, err := os.Stat(path); err == nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(appConfigDir(), "nekobox-go", "app.json"))
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// defaultDataDir is the "config" folder beside the process's working directory — where
// this app has always kept its data — written the relative way whenever that is what it
// is, so the field reads as a short portable path (see resolveAppPath) and a copy of the
// folder still finds its own data. The two folders are the same one when the app is
// started from its own; when they differ the short form would point somewhere the data
// is not (wails dev runs the binary out of build/bin while the data sits at the
// repository root), and the default is that same folder written relative to the
// executable instead. A default that moved the data directory is a data directory that
// looks emptied, which is the one thing it must never do.
func defaultDataDir() string {
	// What is on disk decides, rather than where the process was launched from. A
	// Run key or a shortcut starts it with %SystemRoot% (or wherever the shell was
	// standing) as the working directory, and the old reading of "the folder we were
	// launched from" named C:\Windows\config there — a data directory recreated empty
	// on the next autostart, which is the one thing a data directory must never do.
	//
	// The working directory is kept when its own config folder is already there: that
	// is the development shape (wails dev runs the binary out of build/bin while the
	// data sits at the repository root), and following the executable there would
	// present a full library as an empty one. Otherwise the default is the folder
	// beside the executable, which is where an installed copy keeps its data.
	if wd, err := os.Getwd(); err == nil {
		cfg := filepath.Join(wd, "config")
		if st, err := os.Stat(cfg); err == nil && st.IsDir() {
			if rel := relToExec(cfg); rel != "" && !filepath.IsAbs(rel) {
				return rel
			}
			return cfg
		}
	}
	if dir := execDir(); dir != "" {
		return filepath.Join(dir, "config")
	}
	return "config"
}

// execDir is the folder the running executable sits in. The two paths read it as their
// base rather than the process's working directory: the two are the same folder when
// the app is started from its own, and different the moment it is not — and a path the
// reader wrote must not mean "somewhere else depending on how the app was launched".
func execDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// resolveAppPath turns what the reader typed into the absolute path the app runs on.
// An absolute path is itself; a relative one is taken against execDir, so "config" and
// "sing-box.exe" mean the folder beside the executable. What gets stored is the text
// the reader wrote rather than this result (see appConfig and saveAppConfig), which is
// what makes a copy of the folder work wherever it is unpacked. The rule itself lives
// in util.ResolveAppPath now, so the other places that read a reader-typed path — the
// external core paths, for one — can share it.
func resolveAppPath(p string) string {
	return util.ResolveAppPath(p)
}

// relToExec writes an absolute path the way the about box reports it: relative to the
// folder the executable sits in, which is the text a reader would have written there
// anyway (see resolveAppPath) and short enough to read at a glance. A path with no
// relative form — another volume, or one above the executable's folder — is reported as
// it is: a chain of ".." says less than the path itself.
func relToExec(abs string) string {
	if abs == "" {
		return ""
	}
	base := execDir()
	if base == "" {
		return abs
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

func (a *App) loadAppConfig() {
	dataDirRaw := defaultDataDir()
	dataDir := resolveAppPath(dataDirRaw)
	corePathRaw, corePath := "", ""
	migrateAppConfig()
	if data, err := os.ReadFile(appConfigPath()); err == nil {
		var c appConfig
		if json.Unmarshal(data, &c) == nil {
			if c.DataDir != "" {
				dataDirRaw = c.DataDir
				dataDir = resolveAppPath(c.DataDir)
			}
			corePathRaw = c.CorePath
			corePath = resolveAppPath(c.CorePath)
		}
	}
	a.setPaths(dataDir, dataDirRaw, corePath, corePathRaw)
}

func (a *App) saveAppConfig() {
	// As written, not as resolved: a path saved as an absolute one stops meaning
	// anything the moment the folder moves (see resolveAppPath).
	c := appConfig{DataDir: a.curDataDirRaw(), CorePath: a.curCorePathRaw(), LogDir: a.curLogDirPath()}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	path := appConfigPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}

// logDirName is the folder every log this app writes goes into.
const logDirName = "log"

// logDir is where the logs go: a "log" folder beside the executable.
//
// One folder for all of them — the categorised text logs, the core's own log, the
// crash record, the resident server's log and the front-end's stderr — because a
// reader looking for "the log" should find one place, not five to guess between.
//
// It is beside the executable rather than inside the data directory because that
// is where the reader will look: it is the one folder the program is in, and the
// one a copy of the program folder carries with it.
func (a *App) logDir() string {
	if p := a.curLogDirPath(); p != "" {
		return p
	}
	if dir := execDir(); dir != "" {
		return filepath.Join(dir, logDirName)
	}
	return ""
}

// GetLogDir returns the directory holding the logs.
func (a *App) GetLogDir() string { return a.logDir() }

// setupLogDir decides where the logs go, creates that folder and brings the logs
// this app used to write elsewhere into it.
//
// The folder beside the executable is the one the reader asked for, and the one a
// copy of the program folder carries with it. It is not always writable — an
// installed copy under Program Files, a folder on a read-only share — and a log
// that cannot be written is the same as no log at all, so the data directory
// stands in when the program's own folder refuses. Which one was chosen is
// written into the log itself, because from the outside the two look alike.
//
// Called once, at startup, before anything writes: the log store, the core's own
// log and the crash record all name this folder, and a folder that appears later
// is a folder the first lines went missing from.
func (a *App) setupLogDir() string {
	a.setLogDirPath(resolveLogDir(execDir(), a.curDataDir()))
	p := a.curLogDirPath()
	if p == "" {
		return ""
	}
	// A panic recorded after this point belongs with the rest of the logs, in the
	// folder the reader was told about.
	crashlog.SetDir(p)
	migrateOldLogs(p, a.curDataDir())
	return p
}

// resolveLogDir picks the folder the logs go in and creates it.
//
// A function and not a method so a test can hand it two folders and ask what it
// would have done: the answer depends on nothing but whether the folder can be
// created, and that is exactly what a test has to be able to arrange.
func resolveLogDir(exeDir, dataDir string) string {
	if exeDir != "" {
		dir := filepath.Join(exeDir, logDirName)
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	// The program's own folder would not take it. The data directory is the other
	// place this app is allowed to write, and a log folder there is still one
	// folder — the reader is told which one in the log's own first line.
	if dataDir != "" {
		dir := filepath.Join(dataDir, logDirName)
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	return ""
}

// migrateOldLogs brings the logs this app used to write in other places into the
// one folder it writes them in now.
//
// The old places were three: a "text-logs" folder beside the data directory, the
// core's own log beside the data directory, and the resident server's two files
// in the data directory's temp folder. They are moved, not copied: a log in two
// places is a log nobody knows to look at.
//
// A file that cannot be moved is left where it is. The server may be holding its
// log open, and a file this app cannot move is not a reason to lose it.
func migrateOldLogs(logDir, dataDir string) {
	if logDir == "" || dataDir == "" {
		return
	}
	type old struct {
		from string
		name string
	}
	olds := []old{
		{filepath.Join(dataDir, "vpn-core.log"), "vpn-core.log"},
		{filepath.Join(dataDir, "vpn-core.log.prev"), "vpn-core.log.prev"},
		{filepath.Join(dataDir, "temp", "tunserve.log"), "tunserve.log"},
		{filepath.Join(dataDir, "temp", "tun-front.err.log"), "tun-front.err.log"},
		{filepath.Join(dataDir, "temp", "tun-front.err.log.prev"), "tun-front.err.log.prev"},
	}
	// The categorised logs are a folder of dated files, each of which keeps its
	// name: the log panel reads them by category and day, and a renamed file
	// would be a file the panel does not show.
	if dir := filepath.Join(filepath.Dir(dataDir), "text-logs"); dir != "" {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				olds = append(olds, old{filepath.Join(dir, e.Name()), e.Name()})
			}
		}
	}
	for _, o := range olds {
		if _, err := os.Stat(o.from); err != nil {
			continue
		}
		to := filepath.Join(logDir, o.name)
		if _, err := os.Stat(to); err == nil {
			// Something with that name is already there: the one in the new folder
			// is this run's, and the old one is history that is already counted.
			continue
		}
		if err := os.Rename(o.from, to); err == nil {
			continue
		}
		// Another volume, or a file somebody holds open: copy it across and leave
		// the original where it is rather than losing it.
		if data, err := os.ReadFile(o.from); err == nil {
			if f, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.Write(data)
				_ = f.Close()
			}
		}
	}
}

// GetDataDir returns the data directory as the reader wrote it — a relative path stays
// relative, which is what the settings field shows back (see resolveAppPath). The
// resolved path is what the app runs on, and what the about box reports.
func (a *App) GetDataDir() string { return a.curDataDirRaw() }

// SetDataDir switches the data directory and reloads everything.
//
// A directory that has not moved is not a switch. The settings dialog sends this on
// every save — the field sits in the same form as every other one — and a reload for
// an unchanged path takes the whole data root apart to put the same one back: the
// controller is rebuilt, which stops a live core, loses the node the window was
// reporting and drops the runtime mode flags. Saving a setting has to cost the
// setting and nothing else, so the no-op is caught here rather than left to the
// caller to remember.
func (a *App) SetDataDir(dir string) error {
	raw := strings.TrimSpace(dir)
	if raw == "" {
		return fmt.Errorf("empty directory")
	}
	abs := resolveAppPath(raw)
	if samePath(abs, a.curDataDir()) {
		// The folder did not move, but the way it is written may have: the field is
		// saved on every settle, and a directory written relatively has to stay
		// written that way rather than being turned into this machine's spelling of
		// it (see appConfig).
		if raw != a.curDataDirRaw() {
			a.setDataDirRaw(raw)
			a.saveAppConfig()
		}
		return nil
	}
	// Everything the running state is recorded in lives under the directory about to be
	// abandoned: the pid book, the tun front-end's record, the resolver state file. Hand
	// the machine back while those are still readable, or the next directory inherits a
	// machine pointed at a proxy and a capture nothing in it can name (see
	// quiesceForDataDirChange).
	oldDir, oldRaw := a.curDataDir(), a.curDataDirRaw()
	a.quiesceForDataDirChange()
	a.setDataDir(abs, raw)
	// The same lock Refresh takes: switching the store under a subscription walk
	// leaves that walk writing to the manager the app has just stopped using.
	a.subMu.Lock()
	err := a.reload(false)
	a.subMu.Unlock()
	if err != nil {
		// The switch did not take. Without the rollback the session sat in the
		// state this path used to produce: memory on the new directory, nothing
		// running, the config file still naming the old one — and every later
		// read answering from a store the reader did not ask for. Point
		// everything back at the directory it came from and rebuild on it; the
		// node stays down (the quiesce saw to that), which is honest — the
		// reader pressed a switch that failed.
		a.setDataDir(oldDir, oldRaw)
		a.subMu.Lock()
		if err2 := a.reload(false); err2 != nil {
			a.logf("[datadir] 回退到原目录失败: " + err2.Error())
		}
		a.subMu.Unlock()
		a.saveAppConfig()
		return fmt.Errorf("切换数据目录失败（已回退到原目录）: %w", err)
	}
	a.saveAppConfig()
	return nil
}

// quiesceForDataDirChange hands the machine back before the data directory — and with it
// every book the running state is recorded in — is replaced.
//
// One function because the pieces cannot be separated by the caller: the tun front-end is
// named by a pid file under the old directory, the resident server reads a state file
// under it, and the Windows proxy entry belongs to neither. Stopping the core alone (what
// the reload used to do) left the capture installed and the proxy written, so the machine
// went on pointing at ports nothing would serve again — a new directory with no node in
// it, and no internet on the reader's side.
func (a *App) quiesceForDataDirChange() {
	if a.curSetting() == nil {
		return
	}
	if a.runningID() != core.StoppedID {
		_ = a.StopProfile()
	}
	// The front-end outlives the core by design, so the stop above does not cover it.
	if pid, _ := a.vpnState(); pid > 0 {
		_ = a.stopTunFront()
	}
	a.releaseTunRoutes()
	if a.curSetting().SPModeSystemProxy() {
		_ = a.applySystemProxy(false)
		a.curSetting().SetSPModeSystemProxy(false)
	}
	a.curSetting().SetVPNMode(false)
	// The server reads this file; an empty state is the only way to tell it to put the
	// machine's resolvers back, and it is written while this process still knows the path.
	a.releaseSystemDNS()
	a.emitRunningState()
}

// samePath compares two directories the way the file system does: Windows paths are
// case-insensitive, and a trailing separator or a "." inside one is the same
// directory. Reading them as different is the expensive mistake — it turns a no-op
// into a reload — so the comparison is deliberately loose.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// GetCorePath returns the core path as the reader wrote it (see GetDataDir).
func (a *App) GetCorePath() string { return a.curCorePathRaw() }

// SetCorePath sets the sing-box executable path.
func (a *App) SetCorePath(path string) {
	raw := strings.TrimSpace(path)
	// Empty stays empty: that is what asks for the core to be found automatically.
	abs := resolveAppPath(raw)
	a.setCorePath(abs, raw)
	if a.curController() != nil {
		a.curController().SetCorePath(abs)
	}
	a.saveAppConfig()
}

// PickCoreFile opens the native file picker and returns the chosen executable.
// An empty string means the user cancelled.
func (a *App) PickCoreFile() (string, error) {
	ctx := a.curCtx()
	if ctx == nil {
		return "", fmt.Errorf("backend not initialised")
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "选择 sing-box 核心程序",
		Filters: []runtime.FileFilter{
			{DisplayName: "可执行文件 (*.exe)", Pattern: "*.exe"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

// Refresh re-reads profiles and groups from disk. It is a read-only operation:
// a running core, the status bar and the runtime proxy toggles all survive it.
func (a *App) Refresh() error {
	// The lock lives here rather than at each call site, so that no caller can forget
	// it: a re-read rebuilds the manager's maps while a subscription walk may be
	// standing in the old ones, and the walk's commits would then land on a manager
	// nothing else is reading (the whole update silently lost).
	a.subMu.Lock()
	defer a.subMu.Unlock()
	return a.reload(true)
}

// OpenDataDir opens the settings directory in the file manager. It is where the
// routing profiles, the rule sets and the logs live, and the one place a reader has to
// reach by hand now and then — to drop a .srs in, or to read yesterday's log.
func (a *App) OpenDataDir() error {
	dir := a.curDataDir()
	if dir == "" {
		return fmt.Errorf("data directory is not set")
	}
	return util.OpenPath(dir)
}
