//go:build dev

package main

// devIDSuffix keeps `wails dev` (which builds with the dev tag) on a lock of
// its own: the dev copy and a packaged copy are two different programs as far
// as the user is concerned, and a running packaged instance must not be able to
// keep the development window from starting.
func devIDSuffix() string { return "-dev" }
