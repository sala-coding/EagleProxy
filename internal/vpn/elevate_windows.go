//go:build windows

package vpn

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	procIsUserAnAdmin  = shell32.NewProc("IsUserAnAdmin")
	procShellExecuteW  = shell32.NewProc("ShellExecuteW")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// IsAdmin reports whether this process holds the administrator token.
//
// Internal tun needs it: the tun inbound lives in the main core, so the whole
// app has to be elevated, not just a helper process. IsUserAnAdmin is the same
// probe NekoRay uses (sys/windows/guihelper.cpp).
func IsAdmin() bool {
	ret, _, _ := procIsUserAnAdmin.Call()
	return ret != 0
}

// RelaunchElevated starts this executable again through a UAC prompt, passing
// extraArgs in addition to the current command line, and returns once the new
// instance has been launched. The caller is expected to quit immediately after:
// the elevated copy takes over from here.
//
// ShellExecuteW with the "runas" verb is the same mechanism as NekoRay's
// WinCommander::runProcessElevated — the new process has a new token, so its
// children (the core, and through it the tun adapter) get the rights the tun
// driver demands.
func RelaunchElevated(extraArgs []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Only the caller's own arguments are forwarded. Inheriting os.Args would
	// copy whatever the parent was started with — the dev server flags under
	// `wails dev`, for one — into a process that must come up the same way the
	// user would have launched it.
	args := append([]string{}, extraArgs...)
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, syscall.EscapeArg(a))
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(joinArgs(quoted))
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return err
	}

	const swShownormal = 1
	ret, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(swShownormal))

	// ShellExecuteW reports success as a value greater than 32; anything else is
	// an error code, and 5 in particular is the user dismissing the UAC prompt —
	// in which case this instance must stay alive and say so.
	if ret <= 32 {
		return fmt.Errorf("请求管理员权限失败（错误码 %d，5 表示用户取消了 UAC）", ret)
	}
	runtime.KeepAlive(verb)
	runtime.KeepAlive(file)
	runtime.KeepAlive(params)
	runtime.KeepAlive(dir)
	return nil
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
