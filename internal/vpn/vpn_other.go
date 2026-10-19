//go:build !windows

// Package vpn launches the core with elevation for TUN/VPN mode.
package vpn

import (
	"fmt"
	"time"
)

// RunElevated is not implemented on this platform.
func RunElevated(exe string, args []string, dir string) (int, error) {
	return 0, fmt.Errorf("VPN mode is only implemented on Windows")
}

// RunElevatedWait shares the same fate as RunElevated off Windows.
func RunElevatedWait(exe string, args []string, dir string) (int, error) {
	return 0, fmt.Errorf("VPN mode is only implemented on Windows")
}

// StopElevated is not implemented on this platform.
func StopElevated(pid int) error { return nil }

// IsRunning reports whether pid is alive. There is no TUN core to track off
// Windows, so nothing can be running here.
func IsRunning(pid int) bool { return false }

// AliveAfter follows IsRunning: nothing can be running here, so a start is never
// confirmed.
func AliveAfter(pid int, d time.Duration) bool { return false }
