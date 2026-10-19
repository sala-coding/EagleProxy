//go:build !windows

package util

import (
	"os/exec"
	"runtime"
)

// OpenPath hands a path to the desktop's file manager: `open` on macOS, `xdg-open`
// everywhere else. Both are the platform's own opener, so a machine without one
// reports the failure rather than silently doing nothing.
func OpenPath(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, path).Start()
}
