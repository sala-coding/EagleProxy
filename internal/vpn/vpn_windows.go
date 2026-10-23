//go:build windows

// Package vpn launches the core with elevation for TUN/VPN mode.
package vpn

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"eagle/internal/util"
)

// RunElevated starts exe elevated (UAC) and returns its process id.
//
// The launcher is ShellExecuteExW with the "runas" verb, called directly.
// What closes is not abstraction but a process: the elevation path used to
// cost a PowerShell start before any real work began — roughly a second of
// idling, often the single largest slice of a TUN start that otherwise
// spends nothing. The console flash PowerShell carried was why
// -WindowStyle Hidden had to exist; ShellExecuteEx owns window creation,
// and nShow SW_HIDE covers the elevated child's console too.
func RunElevated(exe string, args []string, dir string) (int, error) {
	if exe == "" {
		return 0, fmt.Errorf("core executable not found")
	}
	handle, err := shellRunAs(exe, commandLine(args), dir)
	if err != nil {
		return 0, fmt.Errorf("elevation failed: %w", err)
	}
	pid, perr := windows.GetProcessId(handle)
	_ = windows.CloseHandle(handle)
	if perr != nil || pid <= 0 {
		return 0, fmt.Errorf("failed to obtain vpn process id")
	}
	return int(pid), nil
}

// StopElevated terminates a process started by RunElevated.
//
// The core runs with the administrator token from the UAC prompt, and a plain
// taskkill issued by an unelevated app only gets "Access is denied" back — the
// TUN core then survived every attempt to switch it off. So a failed plain
// kill falls back to the same elevation used to start it. That costs one more
// UAC prompt, but only when the process is still there to kill.
func StopElevated(pid int) error {
	if pid <= 0 {
		return nil
	}
	return StopElevatedMany([]int{pid})
}

// StopElevatedMany terminates several processes that RunElevated started, with at
// most one elevation prompt.
//
// The unelevated attempt runs first and settles the ordinary case in one shot; only
// what is still alive afterwards is retried elevated, and always in a single
// taskkill, so sweeping up a handful of leftovers does not ask the user once per
// process.
func StopElevatedMany(pids []int) error {
	live := make([]int, 0, len(pids))
	for _, p := range pids {
		if p > 0 {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		return nil
	}
	if err := taskkill(live, false); err == nil {
		return nil
	}
	var alive []int
	for _, p := range live {
		if IsRunning(p) {
			alive = append(alive, p)
		}
	}
	if len(alive) == 0 {
		return nil // already gone; taskkill failed only because it found nothing
	}
	return taskkill(alive, true)
}

// taskkill runs taskkill /F /T on every pid, optionally asking for elevation.
func taskkill(pids []int, elevated bool) error {
	args := make([]string, 0, len(pids)*2+2)
	for _, p := range pids {
		args = append(args, "/PID", strconv.Itoa(p))
	}
	args = append(args, "/F", "/T")
	if elevated {
		// The elevated kill reads the taskkill exit code itself, the way
		// the $null guard used to make a refused UAC a real failure: a
		// process that never ran reports a failure, while a kill that
		// landed reports zero and the caller moves on.
		handle, err := shellRunAs("taskkill.exe", commandLine(args), "")
		if err != nil {
			return err
		}
		// Both waits are unbounded by design: /F is fast even against
		// many pids, and a hang beyond that is a kernel matter no timeout
		// here could repair. The exit code is read while the handle is
		// still open — after CloseHandle the query would be refused.
		_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
		var code uint32
		if err := windows.GetExitCodeProcess(handle, &code); err != nil {
			_ = windows.CloseHandle(handle)
			return err
		}
		_ = windows.CloseHandle(handle)
		if code != 0 {
			return fmt.Errorf("taskkill exited %d", code)
		}
		return nil
	}
	cmd := exec.Command("taskkill", args...)
	util.HideConsole(cmd)
	return cmd.Run()
}

// RunElevatedWait starts exe elevated and waits for it to exit, answering its
// code. It is what callers that must KNOW the outcome use: a registration
// whose success depended on its own process teaches "started" from "succeeded"
// apart — the gap that let a rejected schtasks install read as installed.
func RunElevatedWait(exe string, args []string, dir string) (int, error) {
	if exe == "" {
		return 0, fmt.Errorf("core executable not found")
	}
	handle, err := shellRunAs(exe, commandLine(args), dir)
	if err != nil {
		return 0, err
	}
	// The wait is unbounded by design: /Create is seconds work; anything hung
	// past that is a system matter a timeout could not repair here.
	_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, err
	}
	_ = windows.CloseHandle(handle)
	if code != 0 {
		return 0, fmt.Errorf("%s exited %d", exe, code)
	}
	return 0, nil
}

// shellRunAs launches file with ShellExecuteExW and the "runas" verb — the
// elevation prompt, nothing more. NOCLOSEPROCESS makes the call hand back
// the child's process handle: the pid the caller records for RunElevated,
// and the object the rare waiting caller reads an exit status from.
//
// The COM init is not decoration: ShellExecuteEx runs on whatever OS thread
// the goroutine happens to be parked on, and that thread has no COM
// apartment unless this call makes one. The PowerShell-era path ran
// elevated work inside a child whose runtime owned its apartment, so the
// difference never showed there. An apartment that is already up (Wails
// threads the dialog work happens on) answers S_FALSE and stays put;
// a different-mode one answers RPC_E_CHANGED_MODE and — since the
// apartment itself is fine — is left alone.
//
// The whole call is pinned to one OS thread, because the apartment is a fact
// about a THREAD and a goroutine is not tied to one: preempted between the
// init and the execute, the execute could run on a thread with no apartment
// — the occasional elevation failure with nothing in any log — and the
// deferred uninitialize could run on a third thread still, leaving an
// apartment leaked and the pool poisoned for the next caller.
func shellRunAs(file, params, dir string) (handle windows.Handle, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if hr == nil || hr == syscall.Errno(windows.S_FALSE) {
		// Both answers mean an apartment is open for this thread and the
		// call must be balanced; a different-mode refusal means one is
		// already open and remains the caller's.
		defer windows.CoUninitialize()
	}
	filePtr, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0, err
	}
	paramsPtr, _ := windows.UTF16PtrFromString(params)
	dirPtr, _ := windows.UTF16PtrFromString(dir)
	verbPtr, _ := windows.UTF16PtrFromString("runas")

	sei := &shellExecuteInfoW{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verbPtr,
		lpFile:       filePtr,
		lpParameters: paramsPtr,
		lpDirectory:  dirPtr,
		nShow:        swHide,
	}
	ok, _, lastErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(sei)))
	if ok == 0 {
		if lastErr == windows.ERROR_CANCELLED {
			return 0, fmt.Errorf("elevation declined")
		}
		return 0, lastErr
	}
	if sei.hProcess == 0 {
		return 0, fmt.Errorf("elevation returned no process handle")
	}
	return sei.hProcess, nil
}

// shellExecuteInfoW mirrors SHELLEXECUTEINFOW. The icon/monitor union is a
// single field here — the members never live at the same time — and the
// explicit pad keeps every pointer on the 8-byte edge the Windows ABI puts
// them on.
type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.HWND
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       unsafe.Pointer
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	_              [4]byte
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
)



// stillActive is the pseudo exit code Windows reports for a process that has
// not exited yet.
const stillActive = 259

// IsRunning reports whether pid still names a running process.
//
// It asks for limited information only, which an unelevated process is allowed
// to query even for an elevated target, so it works for the very process that
// plain taskkill cannot kill. Callers use it to decide whether a failed stop
// means "already gone" or "still there".
func IsRunning(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// AliveAfter reports whether pid is still running once d has passed.
//
// RunElevated hands back a pid whether or not the core lives: a front-end started
// while another one still holds the adapter dies within a fraction of a second, and
// the pid it leaves behind is indistinguishable from a live one at a single glance.
// So "it started" is something to confirm across a short window rather than infer
// from the pid's existence at one instant.
func AliveAfter(pid int, d time.Duration) bool {
	if pid <= 0 {
		return false
	}
	deadline := time.Now().Add(d)
	for {
		if !IsRunning(pid) {
			return false
		}
		if !time.Now().Before(deadline) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// commandLine builds the single-parameter string ShellExecuteExW hands to
// the child, under the CommandLineToArgvW rules the child then parses by.
// syscall.EscapeArg is the quoting rule set's battle-tested form — the same
// one RelaunchElevated already relies on — and it covers "Program Files"
// paths the old PowerShell path quoted through psArray for.
func commandLine(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, syscall.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}

func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func psArray(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, "'"+psEscape(a)+"'")
	}
	return "@(" + strings.Join(parts, ",") + ")"
}
