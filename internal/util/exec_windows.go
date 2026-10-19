//go:build windows

package util

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// createNoWindow is CREATE_NO_WINDOW: the child is created without a console.
//
// Unlike DETACHED_PROCESS it leaves the inherited stdin/stdout/stderr handles
// intact, so it can be used on commands whose output is piped back into the
// log panel.
const createNoWindow = 0x08000000

// HideConsole marks cmd so Windows starts it without a console window.
//
// Every binary the app shells out to — sing-box, external cores, powershell,
// taskkill — is built for the console subsystem, so Windows allocates a console
// for each one and a terminal flashes on the desktop every time a profile is
// started, TUN is switched on or a speed test runs. The app itself is a GUI
// subsystem binary and has no console to inherit, so the allocation is always
// a fresh window. Clearing the flag keeps those children fully in the
// background; their stdout/stderr still reach the pipes we attached.
func HideConsole(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// DetachConsole drops the console this process was started with.
//
// The resident TUN server is this same executable launched by its scheduled
// task, and a console-subsystem build (the dev build above all) is handed a
// fresh, visible console for its whole lifetime — a black window that sits on
// the desktop for as long as the server lives. Detaching is the fix, and it
// closes the window rather than hiding it: this process is the only one
// attached to that console, and a console with no attached process goes away.
// Nothing readable is lost — the server writes its logs to files, and every
// child it raises is Hidden or redirected anyway.
//
// On a GUI-subsystem build there is no console to drop and the call answers
// false; on other platforms it does not exist (see exec_other.go).
var (
	modkernel32     = windows.NewLazySystemDLL("kernel32.dll")
	procFreeConsole = modkernel32.NewProc("FreeConsole")
)

func DetachConsole() bool {
	// FreeConsole is a plain BOOL in kernel32 and x/sys/windows does not wrap
	// it, so it goes through the lazy proc like every other unwrapped call in
	// this codebase.
	r, _, _ := procFreeConsole.Call()
	return r != 0
}
