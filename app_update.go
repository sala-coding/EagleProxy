package main

import (
	"eagle/internal/tunserv"
	"eagle/internal/update"
	"eagle/internal/util"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CheckUpdate queries GitHub for the newest release, or nil when this build is it.
func (a *App) CheckUpdate() (*update.Release, error) {
	if a.curSetting() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	return update.Check(updateRepo, a.curSetting().CheckIncludePre, a.buildTime())
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
	rel, err := update.Check(updateRepo, a.curSetting().CheckIncludePre, a.buildTime())
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
	// The script retries rather than firing one move and starting the build again
	// regardless: a move that fails while this process is still shutting down (or
	// while anything else holds the image) used to be ignored — errorlevel was never
	// read — and the reader, who had already been told the update was installing, was
	// handed the old build with nothing said. Thirty tries at roughly a second each
	// covers a slow shutdown; the failure path still starts the old build and leaves
	// a line beside it.
	body := fmt.Sprintf("@echo off\r\n"+
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
		"del \"%%~f0\"\r\n"+
		"exit /b\r\n"+
		":done\r\n"+
		"start \"\" \"%%SELF%%\"\r\n"+
		"del \"%%~f0\"\r\n",
		tmp, self, time.Now().Format("2006-01-02 15:04:05"))
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
