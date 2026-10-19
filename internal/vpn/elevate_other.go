//go:build !windows

package vpn

import (
	"fmt"
	"os"
)

// IsAdmin reports whether the process runs as root. On Unix the tun device
// needs CAP_NET_ADMIN / root, which NekoRay grants per binary rather than by
// relaunching the UI.
func IsAdmin() bool {
	return os.Geteuid() == 0
}

// RelaunchElevated is not implemented off Windows; there the app is expected to
// carry the capability it needs (or be started with sudo) instead of relaunching
// itself.
func RelaunchElevated(extraArgs []string) error {
	return fmt.Errorf("relaunching with elevation is only implemented on Windows")
}
