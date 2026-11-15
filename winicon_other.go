//go:build !windows

package main

// applyBigWindowIcon has nothing to do off Windows: the taskbar icon is a
// Windows-side gap (see winicon_windows.go), and the other platforms take the
// window icon from what the framework already sets.
func applyBigWindowIcon() {}
