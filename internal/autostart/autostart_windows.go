//go:build windows

// Package autostart manages the "run at login" registration.
package autostart

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Enable registers the app to start at login.
//
// The value is the quoted path alone, with no working directory and no shell wrapper.
// A Run key cannot carry a working directory, so the process starts with whatever
// explorer had — %SystemRoot%, in the ordinary case — and that used to matter: the data
// directory was read as "the config folder beside the process's working directory", and
// an autostart therefore looked at C:\Windows\config, created it empty, and presented a
// full library as an empty one. The fix lives where the question is asked
// (defaultDataDir prefers the folder beside the executable, and only honours the working
// directory when a config folder is already sitting there), so this entry can stay the
// plain path it always was: a shell wrapper here would only be a second place for the
// same rule, and a place for quoting mistakes.
func Enable(name, exePath string) error {
	if exePath == "" {
		return errors.New("empty executable path")
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, `"`+exePath+`"`)
}

// Disable removes the login entry.
func Disable(name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// Enabled reports whether the login entry exists.
func Enabled(name string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(name); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ExePath returns the current executable path without quotes.
func ExePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return strings.Trim(p, `"`)
}
