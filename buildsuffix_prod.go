//go:build !dev

package main

// devIDSuffix is empty in a packaged build: every copy of the shipped program —
// the exe the user double-clicks, a renamed copy of it, and the elevated copy
// the TUN handover starts — shares one lock, so a second launch is always
// intercepted.
func devIDSuffix() string { return "" }
