//go:build !windows

package util

import "os/exec"

// HideConsole is a no-op off Windows: a child process never gets a console
// window of its own there.
func HideConsole(cmd *exec.Cmd) {}

// DetachConsole is a no-op off Windows: there is no console to detach from.
func DetachConsole() bool { return true }
