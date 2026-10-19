//go:build windows

package main

import (
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// SystemFontScale returns the system's text size, as the fraction of this app's
// own default a fresh install should open at: the display's applied DPI over
// the 96 it claims as its 100%. The app's own slider is what turns the number
// into a preference: it seeds the first value and keeps whatever the reader
// chose after. 0 means "cannot be read for this user"; the front end treats
// that as 1.
func (a *App) SystemFontScale() float64 {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Control Panel\Desktop\WindowMetrics`, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	dpi := appliedDPI(k)
	if dpi == 0 {
		return 0
	}
	return float64(dpi) / 96.0
}

// appliedDPI reads the display's text scale. Windows keeps it as REG_SZ here, and
// registry.GetIntegerValue accepts nothing but DWORD and QWORD — so the integer read
// failed on every machine, this function answered 0 for all of them, and a fresh
// install never opened at the size the display actually runs. Both shapes are read:
// the number, and the number written as text.
func appliedDPI(k registry.Key) uint64 {
	if v, _, err := k.GetIntegerValue("AppliedDPI"); err == nil {
		return v
	}
	s, _, err := k.GetStringValue("AppliedDPI")
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
