//go:build !windows

package vpn

// CleanStaleTunAdapters has nothing to clean off Windows: there is no wintun
// adapter registry to go stale there.
func CleanStaleTunAdapters() (int, error) { return 0, nil }

// RemoveTunAdapter has nothing to remove off Windows: the adapter is a wintun device,
// and there is no wintun outside it.
func RemoveTunAdapter(name string) (removed int, found int, err error) { return 0, 0, nil }
