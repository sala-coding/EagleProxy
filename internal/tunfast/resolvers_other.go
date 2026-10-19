//go:build !windows

package tunfast

import (
	"net/netip"
)

// AdapterResolvers is one interface's resolver list as the machine has it. Declared here so
// the app's own bookkeeping compiles everywhere it builds; nothing fills it in off Windows.
type AdapterResolvers struct {
	Name string   `json:"name"`
	LUID uint64   `json:"luid"`
	V4   []string `json:"v4,omitempty"`
	V6   []string `json:"v6,omitempty"`
}

// SystemResolvers reports ErrUnsupported: there is no interface table to read here, and the
// caller's answer to that is to leave the machine's resolvers alone.
func SystemResolvers(tunnelName string) ([]AdapterResolvers, error) { return nil, ErrUnsupported }

// PrependResolvers reports ErrUnsupported.
func PrependResolvers(snapshot []AdapterResolvers, v4, v6 netip.Addr) error { return ErrUnsupported }

// RestoreResolvers reports ErrUnsupported. Nothing was ever changed, so nothing is lost.
func RestoreResolvers(snapshot []AdapterResolvers) error { return ErrUnsupported }
