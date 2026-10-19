//go:build windows

package main

import (
	"time"

	"golang.org/x/sys/windows"
)

// wailsSingleInstanceMutex mirrors the kernel object Wails uses for the single
// instance lock. Wails derives it from the UniqueId it is handed: see
// SetupSingleInstance in
//
//	wails/v2/internal/frontend/desktop/windows/single_instance.go
//
// where the mutex is created as "wails-app-" + uniqueId + "sim".
//
// The name is mirrored rather than invented because there is nothing else to
// wait on: the handover between this app and its elevated copy needs "the
// previous instance is gone", and the moment the lock is released is exactly
// that. If Wails ever changes the scheme this wait simply times out and the
// caller falls back to a lock of its own, which is the behaviour the app had
// before — the worst case is a temporary extra window, not a broken start.
func wailsSingleInstanceMutex(uniqueID string) string {
	return "wails-app-" + uniqueID + "sim"
}

// waitForInstanceLockRelease blocks until the single instance lock is free —
// i.e. until whichever process holds it has exited — or until timeout elapses,
// and reports whether it is free.
//
// The probe is a throwaway CreateMutex on the very name Wails will use. While
// another process owns it the call fails with ERROR_ALREADY_EXISTS; once it
// succeeds the handle is closed again immediately, letting go so Wails can take
// the lock for real a moment later.
//
// Any other failure (access denied, for instance) is reported as "not free", so
// the caller keeps its fallback rather than racing a lock it cannot see.
func waitForInstanceLockRelease(uniqueID string, timeout time.Duration) bool {
	name, err := windows.UTF16PtrFromString(wailsSingleInstanceMutex(uniqueID))
	if err != nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		h, err := windows.CreateMutex(nil, false, name)
		if err == nil {
			_ = windows.CloseHandle(h)
			return true
		}
		if err != windows.ERROR_ALREADY_EXISTS {
			// The handle is valid even in this case, but the lock belongs to
			// someone else: release our reference to it on the way out.
			if h != 0 {
				_ = windows.CloseHandle(h)
			}
			return false
		}
		_ = windows.CloseHandle(h)
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
