//go:build windows

package dnsx

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows"
)

// The two socket options that pin a socket's egress, spelled out because
// x/sys/windows does not carry them: both are 31, at their own protocol level
// (winSock2.h: IP_UNICAST_IF and IPV6_UNICAST_IF).
const (
	ipUnicastIF   = 31
	ipv6UnicastIF = 31
)

// bindInterface makes the socket leave by one interface whatever the route table says.
//
// Windows chooses the interface from the destination, and with a TUN up that means the
// tunnel: a query the app's forwarder must not send through it (see internal/dnsstub)
// would be captured there and answered by the core it is standing in for, a circle. The
// unicast-interface options pin the egress instead.
//
// The byte orders are not the same for the two families, and both were measured rather
// than looked up (a wrong order is invisible: the option is either refused outright or
// read as some other interface, and the socket goes on using the route table):
//
//   - IP_UNICAST_IF takes the index in NETWORK byte order. Handed the host-order value of
//     a real interface it answers WSAEADDRNOTAVAIL — "the requested address is not valid
//     in its context" — which is how this was settled.
//   - IPV6_UNICAST_IF takes it in host order.
//
// A socket of one family fails the other family's option with WSAENOPROTOOPT, which is
// expected and not a failure; an error is only reported when BOTH are refused, because
// that is a socket that did not get pinned at all — and a dial that believed it was
// pinned would quietly go back through the tunnel.
func bindInterface(index uint32) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var both error
		if err := c.Control(func(fd uintptr) {
			h := windows.Handle(fd)
			v4 := windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIF, int(networkOrder(index)))
			v6 := windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIF, int(index))
			if v4 != nil && v6 != nil {
				both = fmt.Errorf("pin interface %d: v4=%v v6=%v", index, v4, v6)
			}
		}); err != nil {
			return err
		}
		return both
	}
}

// networkOrder is the host-order interface index as the option wants it: big endian, which
// a little-endian host reaches by reversing the bytes.
func networkOrder(index uint32) uint32 {
	return index<<24 | (index&0xff00)<<8 | (index&0xff0000)>>8 | index>>24
}
