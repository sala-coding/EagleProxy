//go:build !windows

// Package winframe touches the window's own chrome — the parts of a Windows window that
// Wails does not expose as options.
package winframe

// ClearTitleBarIcon is a no-op away from Windows: the title bar's icon there comes from
// the window manager's own rules, not from something the app asks for.
func ClearTitleBarIcon() {}
