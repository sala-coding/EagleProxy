//go:build !windows

package main

import "time"

// waitForInstanceLockRelease has nothing to wait for off Windows: the UAC
// handover it sequences is a Windows-only path, so the caller is always free to
// take the lock.
func waitForInstanceLockRelease(string, time.Duration) bool { return true }
