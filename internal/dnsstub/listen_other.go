//go:build !windows

package dnsstub

import "syscall"

// reuseAddr is a no-op off Windows: the port is released with the socket there, and a
// listener that failed to bind is one the caller already handles (see Start).
func reuseAddr(network, address string, c syscall.RawConn) error { return nil }
