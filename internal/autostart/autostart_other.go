//go:build !windows

// Package autostart manages the "run at login" registration.
package autostart

import (
	"os"
	"strings"
)

// Enable is not implemented on this platform.
func Enable(name, exePath string) error { return nil }

// Disable is not implemented on this platform.
func Disable(name string) error { return nil }

// Enabled always reports false on this platform.
func Enabled(name string) (bool, error) { return false, nil }

// ExePath returns the current executable path without quotes.
func ExePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return strings.Trim(p, `"`)
}
