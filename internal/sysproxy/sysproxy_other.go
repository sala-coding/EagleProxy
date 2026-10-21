//go:build !windows

// Package sysproxy configures the system proxy.
package sysproxy

import "fmt"

// Set is not implemented on this platform.
func Set(format, ip string, httpPort, socksPort int, bypass string) error {
	return fmt.Errorf("system proxy is only implemented on Windows")
}

// SetPAC is not implemented on this platform.
func SetPAC(url string) error {
	return fmt.Errorf("system proxy is only implemented on Windows")
}

// Clear is not implemented on this platform.
func Clear() error { return nil }

// Current has no registry to read here: there is no system proxy for this app to leave
// behind, so the answer is always "nothing set". The PAC field is part of the
// signature on Windows (see sysproxy_windows.go) and stays empty here.
func Current() (bool, string, string, error) { return false, "", "", nil }

// CurrentBypass has no registry to read here.
func CurrentBypass() (string, error) { return "", nil }

// SetUndoFile / HasUndo are the Windows-side undo record (what the machine's
// proxy was before a takeover). There is no takeover off Windows, so nothing is
// ever recorded and nothing is ever waiting to be restored. Defined at all so
// the app compiles on the platforms it is only ever built for in CI.
func SetUndoFile(path string) {}

// HasUndo reports no recorded takeover.
func HasUndo() bool { return false }
