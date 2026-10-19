package main

import (
	"eagle/internal/tunserv"
	"eagle/internal/update"
	"eagle/internal/util"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CheckUpdate queries GitHub for the newest release, or nil when this build is it.
func (a *App) CheckUpdate() (*update.Release, error) {
	if a.curSetting() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	return a.newerRelease()
}

// newerRelease answers "is there something newer than what is running", and it
// answers it with the version number whenever there is one.
//
// The version number is the answer because it is the only one that can be right
// in every case: it says "1.0.1 is newer than 1.0.0" and "1.0.1 is not newer
// than 1.0.1" — including the case that a time cannot get right, a release
// published minutes AFTER the build that made it, which a time orders as
// newer. Every build this project makes carries a comparable number: a release
// its own ("v1.0.2"), a working copy a number below every release
// ("0.0.0-dev+9988017", see build/release.ps1).
//
// The build time is only the last resort, for a build that carries no number
// to compare — one made by a bare "wails build", which stamps nothing.
func (a *App) newerRelease() (*update.Release, error) {
	rel, err := update.Latest(updateRepo, a.curSetting().CheckIncludePre)
	if err != nil || rel == nil {
		return rel, err
	}
	if appVersion != "" {
		if c, ok := update.Compare(appVersion, rel.TagName); ok {
			if c >= 0 {
				// This build is the newest one there is.
				return nil, nil
			}
			return rel, nil
		}
	}
	// No number to compare (a build nobody stamped, or a tag this build cannot
	// read): the build time is what is left.
	if !update.PublishedAfter(*rel, a.buildTime()) {
		return nil, nil
	}
	return rel, nil
}

// buildTime is when the running executable was written, which is the only version
// stamp this build carries — the About card shows a commit hash, and no release tag
// can be ordered against one. A stat that fails answers the zero time, which makes
// the comparison in update.Check skip itself rather than hide updates.
func (a *App) buildTime() time.Time {
	self, err := os.Executable()
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(self)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// updateSwapScript is the batch file that swaps the new build over the running
// one.
//
// It retries rather than firing one move and starting the build again
// regardless: a move that fails while this process is still shutting down (or
// while anything else holds the image) used to be ignored — errorlevel was never
// read — and the reader, who had already been told the update is installing, was
// handed the old build with nothing said. Thirty tries at roughly a second each
// covers a slow shutdown; the failure path still starts the old build and leaves
// a line beside it.
//
// The script does not delete itself. A batch file is read line by line, and
// the line after "del %~f0" cannot be read any more: the shell answers "the
// batch file cannot be found" and puts that complaint in a window on the
// reader's screen — over a swap that has already worked. The file is a few
// hundred bytes in the temp folder, and the next update takes it away (see
// cleanupUpdateTemps).
func updateSwapScript(newPath, self string) string {
	return fmt.Sprintf("@echo off\r\n"+
		"setlocal\r\n"+
		"set \"NEW=%s\"\r\n"+
		"set \"SELF=%s\"\r\n"+
		"set /a TRIES=0\r\n"+
		":retry\r\n"+
		"ping -n 2 127.0.0.1 > nul\r\n"+
		"move /y \"%%NEW%%\" \"%%SELF%%\" > nul 2>&1\r\n"+
		"if not errorlevel 1 goto :done\r\n"+
		"set /a TRIES+=1\r\n"+
		"if %%TRIES%% LSS 30 goto :retry\r\n"+
		"echo %s update failed: could not replace the running build (tried %%TRIES%% times) >> \"%%SELF%%.update.log\"\r\n"+
		"start \"\" \"%%SELF%%\"\r\n"+
		"exit /b\r\n"+
		":done\r\n"+
		"start \"\" \"%%SELF%%\"\r\n"+
		"exit /b\r\n",
		newPath, self, time.Now().Format("2006-01-02 15:04:05"))
}

// canReplace reports whether this build's own folder can be written to.
//
// The swap moves the new build over the running one, and a folder the reader
// cannot write to — Program Files, or anything else the system owns — would
// fail every try of the script. The reader would be told an update was
// installing and end up with the old build and no word about why. The
// question is worth asking before the download rather than thirty times
// after it.
func canReplace(dir string) bool {
	f, err := os.CreateTemp(dir, "eagle-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// cleanupUpdateTemps removes what an earlier update left in the temp folder: the
// swap script of that run and the build it had downloaded.
//
// The script of the run that is starting is written after this, so it is never
// caught by it — and it is this that takes away the script a finished update
// left behind, since a batch file cannot delete itself without the shell
// complaining about it (see the script below).
func cleanupUpdateTemps() {
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "eagle-update-") {
			continue
		}
		if strings.HasSuffix(name, ".bat") || strings.HasSuffix(name, ".exe") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// DownloadUpdate fetches the newest release's Windows build, hands the swap to a small
// script and quits.
//
// The running image cannot be overwritten, so the last step has to happen after this
// process is gone: the script waits for it, moves the new build over the old one and
// starts it. That is the same shape nekoray uses — it fetches the release and leaves an
// updater behind — and it is the only shape that works on Windows, where an executing
// file is locked.
func (a *App) DownloadUpdate() error {
	if a.curSetting() == nil {
		return fmt.Errorf("backend not initialised")
	}
	// One download at a time. Two of them shared the temp path and the swap script
	// (both named by this process's pid, so two calls in one process are the same
	// names), and the second `os.Create` truncated the first one's half-written file
	// while both went on to quit the app and swap in whatever was left.
	if !a.updateBusy.CompareAndSwap(false, true) {
		return fmt.Errorf("更新正在处理中，请稍候")
	}
	defer a.updateBusy.Store(false)
	rel, err := a.newerRelease()
	if err != nil {
		return err
	}
	if rel == nil {
		// Not a failure: the comparison above answered "this build is the newest
		// one there is", and saying so is the whole point of having it.
		a.notifyMessage("当前已是最新版本，无需更新")
		return nil
	}
	asset, err := update.WindowsAsset(rel)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// Both names carry the pid: two instances updating at once would otherwise
	// overwrite each other's temp build (and even swap script) mid-flight.
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("eagle-update-%d%s", os.Getpid(), filepath.Ext(self)))
	// Whether the new build can land on the old one at all. Asked before the
	// download: a folder the reader cannot write to is a fact worth saying out
	// loud, and saying it after a twenty-megabyte download leaves the reader
	// with the old build and a message about an update that was never going to
	// happen.
	dir := filepath.Dir(self)
	if !canReplace(dir) {
		return fmt.Errorf("程序所在目录不可写（%s），无法自动更新；请以管理员身份运行，或把程序放到可写的目录", dir)
	}
	// What an earlier update left in the temp folder goes first: the swap
	// script of that run and the build it had downloaded. This run's own files
	// are written after this, so they are never caught by it.
	cleanupUpdateTemps()
	if err := update.Download(asset.URL, tmp); err != nil {
		return err
	}
	// Everything that runs from this same executable has to be gone before the swap.
	// Windows will not let a running image be replaced, and both the TUN front-end and
	// the resident server are this binary with different arguments: the bat below
	// would retry a move that cannot succeed until the machine is rebooted, and the
	// reader would be told the install was on its way.
	a.updating.Store(true)
	if pid, _ := a.vpnState(); pid > 0 {
		if err := a.stopTunFront(); err != nil {
			a.logf("[update] TUN 前端未能停止（替换程序前需要它退出）: " + err.Error())
		}
	}
	if tunserv.ServerAlive(a.dataDir) {
		_ = tunserv.SendCommand(a.dataDir, tunserv.Command{Action: tunserv.ActionExit})
		for i := 0; i < 15; i++ {
			if !tunserv.ServerAlive(a.dataDir) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if tunserv.ServerAlive(a.dataDir) {
			a.logf("[update] 常驻服务未退出，替换程序可能失败（脚本会重试）")
		}
	}
	script := filepath.Join(os.TempDir(), fmt.Sprintf("eagle-update-%d.bat", os.Getpid()))
	body := updateSwapScript(tmp, self)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		// The update never started, so the exit that follows is an ordinary one and the
		// front-end may be kept: leaving the flag set told every later exit to tear it
		// down, for a build that was never being replaced.
		a.updating.Store(false)
		return err
	}
	cmd := exec.Command("cmd", "/c", "start", "/min", script)
	util.HideConsole(cmd)
	if err := cmd.Start(); err != nil {
		a.updating.Store(false)
		return err
	}
	// The script is detach-started and outlives this cmd; the handle must still
	// be released, or the updater cmd sits as a zombie until the app exits.
	go func() { _ = cmd.Wait() }()
	// Said before the window goes, and the wait is short because the script is already
	// counting: quitting first is what lets the move succeed at all.
	a.notifyMessage("更新已下载，程序即将重启完成安装…")
	go func() {
		time.Sleep(600 * time.Millisecond)
		a.QuitApp()
	}()
	return nil
}
