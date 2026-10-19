//go:build !windows

package vpn

// CleanStaleTunAdapters has nothing to clean off Windows: there is no wintun
// adapter registry to go stale there.
func CleanStaleTunAdapters() (int, error) { return 0, nil }
