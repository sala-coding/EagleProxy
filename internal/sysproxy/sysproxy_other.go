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
// behind, so the answer is always "nothing set".
func Current() (bool, string, error) { return false, "", nil }
