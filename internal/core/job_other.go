//go:build !windows

package core

import (
	"os/exec"
	"strconv"
	"strings"
)

// trackCoreProcess is a no-op off Windows. POSIX has no job objects; the sanity
// net is the pidFile below plus the same "is this really our core" check, which
// recognises a leftover from a previous run whatever killed it.
func trackCoreProcess(pid int) error { return nil }

// clearStaleCore always declines off Windows. ps reports only a command name,
// so "same name" cannot safely mean "our core" — another user's process must
// never be killed on that evidence.
func clearStaleCore(pid int, coreExe string) bool { return false }

// listenersOnPort is unimplemented off Windows; the port conflict there keeps
// its generic wording rather than naming a process it cannot reliably identify.
func listenersOnPort(port int) []int { return nil }

// portLines is likewise empty off Windows.
func portLines(port int) []string { return nil }

// processExe returns the executable backing pid, reported by ps. macOS refuses
// it for processes owned by another user, in which case the caller must not act
// on the answer.
func processExe(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
