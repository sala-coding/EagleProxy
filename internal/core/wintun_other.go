//go:build !windows

package core

// WintunReady always reports true off Windows: the tun driver there is part of
// the kernel (utun on macOS, /dev/net/tun on Linux) and needs no DLL.
func WintunReady(coreExe, configDir string) bool { return true }
