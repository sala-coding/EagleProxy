//go:build !windows

package dnsx

import "syscall"

// bindInterface has nothing to do off Windows: there is no tunnel here for a query to be
// captured by, and the caller's reason for asking (see dnsx.Options.InterfaceIndex) does
// not exist. Returning nil keeps every call site one shape.
func bindInterface(index uint32) func(network, address string, c syscall.RawConn) error {
	return nil
}
