//go:build windows

package dnsstub

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// reuseAddr lets the listener take its port back while the operating system still counts
// the last connections on it.
//
// Windows refuses a bind on a port that any socket still holds — a connection the previous
// process never got to close, or one in TIME_WAIT after it was killed — and the reader
// meets that as a forwarder that quietly did not come up: the log says the side fell back
// to the core's own resolver, and the smart pick stops taking effect without anything
// obviously broken. A restart is exactly when that happens, which is exactly when nobody is
// watching.
//
// The trade is that another local process could bind the same port first and read what the
// core sends; both ports are loopback-only helpers of this app, and the alternative is a
// feature that disappears after a crash.
func reuseAddr(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		_ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	})
}
