//go:build !windows

package vpn

// StopOrphanTunFronts has nothing to stop off Windows: there is no elevated
// front-end holding an adapter there.
func StopOrphanTunFronts(frontCfg string, keep int) (int, error) { return 0, nil }
