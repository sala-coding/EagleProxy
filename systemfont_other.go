//go:build !windows

package main

// SystemFontScale has nothing to read where there is no Windows text size, so
// it answers 0 and the front end keeps its own 1 (see SystemFontScale in
// systemfont_windows.go for what the number means).
func (a *App) SystemFontScale() float64 { return 0 }
