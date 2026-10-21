package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The update swap script, and the command that is supposed to run it.
//
//	cd e:\project\eagle-desk; go test -run TestReview ./

// TestReviewUpdateSwapScriptRunsWhenTempPathHasASpace pins the launch of the
// update swap script to the command that actually runs.
//
// Go quotes an argument that contains a space, so the command line cmd.exe sees
// for a script under "C:\Users\John Smith\AppData\Local\Temp" is
//
//	cmd /c start /min "C:\Users\John Smith\AppData\Local\Temp\eagle-update-1.bat"
//
// and `start` takes its FIRST quoted argument as the window TITLE. With no command
// left, it opens a console whose title is the script path and never runs the script.
//
// The consequence in the app: cmd.Start() returns no error, the app says
// "更新已下载，程序即将重启完成安装…", quits 600 ms later — and nothing replaces the
// binary. The reader gets the old build back and no word about why.
//
// The command is the one the app builds (swapScriptArgs), so this fails again the
// day the empty title is dropped.
func TestReviewUpdateSwapScriptRunsWhenTempPathHasASpace(t *testing.T) {
	root := t.TempDir()
	// A temp path with a space in it, like C:\Users\John Smith\AppData\Local\Temp.
	dir := filepath.Join(root, "eagle start test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	script := filepath.Join(dir, "eagle-update-999.bat")
	marker := filepath.Join(dir, "marker.txt")
	body := "@echo off\r\necho RAN > \"" + marker + "\"\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if !strings.Contains(script, " ") {
		t.Fatalf("the reproduction needs a path with a space, got %q", script)
	}

	// The command app_update.go builds.
	if !runSwapScript(t, marker, swapScriptArgs(script)...) {
		t.Fatalf("the swap script never ran (no marker file after 5s). `start` takes its first " +
			"quoted argument as the window title, so a path with a space in it is swallowed as a " +
			"title and the script is never executed — the empty title `start` needs is missing")
	}
}

// runSwapScript launches a batch file with the given cmd.exe arguments and reports
// whether it ran.
func runSwapScript(t *testing.T, marker string, args ...string) bool {
	t.Helper()
	_ = os.Remove(marker)
	cmd := exec.Command("cmd", args...)
	// A console of its own, so the console `start` opens for the quoted "title"
	// cannot take the test runner's console with it. CREATE_NEW_CONSOLE is 0x10.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000010}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start failed: %v", err)
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
