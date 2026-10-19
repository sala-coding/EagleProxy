//go:build windows

package util

import "os/exec"

// OpenPath hands a path to the desktop's file manager.
//
// explorer is used rather than ShellExecute because it needs no cgo and no syscall
// dance, and because it accepts a directory path as it stands — the one case this is
// for (the settings directory) is a directory that always exists.
func OpenPath(path string) error {
	return exec.Command("explorer", path).Start()
}
