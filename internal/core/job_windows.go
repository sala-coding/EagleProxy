//go:build windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows does not tear a child process down with its parent: whether the app
// exits normally, crashes, or is killed from the task manager, a core started
// with exec.Command survives and keeps holding the Clash API port. The next
// launch then fails its port probe with a message asking the user to end that
// process by hand.
//
// The fix is the OS's own answer for exactly this: put every core process into
// a job object flagged with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. The job's last
// handle — including the implicit close the kernel performs when this process
// dies — terminates its members. Nothing else has to run on the way out, so a
// crash cleans up as reliably as a clean exit.
var (
	jobOnce struct {
		sync.Mutex
		h windows.Handle
	}
)

// trackCoreProcess binds a freshly started core (or external helper) to this
// process's job object so the OS kills it when this process dies.
func trackCoreProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	jobOnce.Lock()
	defer jobOnce.Unlock()

	if jobOnce.h == 0 {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return err
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			_ = windows.CloseHandle(h)
			return err
		}
		jobOnce.h = h
	}

	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(p) }()
	return windows.AssignProcessToJobObject(jobOnce.h, p)
}

// clearStaleCore terminates a core process recorded by an earlier run and
// reports whether it did. Two guards keep this from being a loaded gun: the pid
// must still resolve, and the executable behind it has to be the very core this
// app is configured to run. A port squatted by some other proxy app is left
// alone — that is a message for the user, not a process for us to kill.
func clearStaleCore(pid int, coreExe string) bool {
	if pid <= 0 {
		return false
	}
	exe := processExe(pid)
	if exe == "" {
		return false // gone already, or privileged beyond our reach: hands off
	}
	if !strings.EqualFold(filepath.Base(exe), filepath.Base(coreExe)) {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Kill() == nil
}

// processExe returns the executable path backing pid, or "" when the process is
// gone or is too privileged to be queried. Windows allows an unelevated caller
// to query a limited handle even for an elevated target, which is what makes
// checking the image of a stray core possible.
func processExe(pid int) string {
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer func() { _ = windows.CloseHandle(p) }()

	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(p, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}
