//go:build windows

// Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
// Adapted from sing-tun (github.com/sagernet/sing-tun) and its WireGuard
// upstream, MIT. The deterministic GUID identity the fast restart rides on
// lives here.

package tunfast

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"unsafe"

	"eagle/internal/tunfast/internal/winipcfg"

	"golang.org/x/sys/windows"
)

// tunnelType is the adapter type string. Keep it identical to sing-tun's:
// the adapter entry a killed karing or sing-box core leaves behind stays
// recognisable, and OpenAdapter below re-opens it instead of failing.
const tunnelType = "sing-tun"

// isAdapterAlreadyExists recognizes the "the adapter is already there" answer
// from WintunCreateAdapter.
//
// The code that arrives is HRESULT_FROM_WIN32(ERROR_ALREADY_EXISTS) —
// 0x800700B7, measured, not inferred — rather than the bare 183, which is why
// neither errors.Is(err, os.ErrExist) nor a comparison against
// ERROR_ALREADY_EXISTS catches it: syscall.Errno.Is compares the raw number, and
// the raw number here is a wrapped one. Both spellings are unwrapped below.
//
// This is the one answer that must never be mistaken for a failure. Mistaken, the
// create-not-open fallback never runs, and a prepared adapter is reported as a
// broken one while it sits there working — which reads as "TUN will not start"
// with the adapter plainly present in 网络连接, and sends anyone looking for it
// into the driver rather than into this comparison.
func isAdapterAlreadyExists(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return errors.Is(err, os.ErrExist)
	}
	code := win32Code(errno)
	return code == windows.ERROR_ALREADY_EXISTS ||
		code == windows.ERROR_FILE_EXISTS ||
		errors.Is(err, os.ErrExist)
}

// win32Code unwraps an HRESULT-wrapped Win32 error — 0x8007xxxx, the shape
// WintunCreateAdapter answers with — back to the Win32 code it carries. Anything
// else is returned unchanged.
func win32Code(errno syscall.Errno) syscall.Errno {
	if v := uint32(errno); v>>16 == 0x8007 {
		return syscall.Errno(v & 0xFFFF)
	}
	return errno
}

// generateGUIDByDeviceName is the deterministic GUID karing leans on:
// md5 over the prefix "wintun" plus the adapter name. The same name always
// maps onto the same GUID, so a restart re-opens the adapter instead of
// re-creating it, and Windows keeps one NLA profile for the identity.
func generateGUIDByDeviceName(name string) *windows.GUID {
	hash := md5.New()
	hash.Write([]byte("wintun"))
	hash.Write([]byte(name))
	sum := hash.Sum(nil)
	return (*windows.GUID)(unsafe.Pointer(&sum[0]))
}

// halve turns a /0 into its two /1 halves, 0.0.0.0/0 into 0.0.0.0/1 plus
// 128.0.0.0/1 and ::/0 into ::/1 plus 8000::/1. The shape is what beats a
// physical default route without touching it (see capturePrefixes).
func halve(p netip.Prefix) []netip.Prefix {
	return []netip.Prefix{
		netip.PrefixFrom(p.Addr(), 1),
		netip.PrefixFrom(otherHalf(p.Addr()), 1),
	}
}

// otherHalf returns the second half of the /1 block p.Addr() opens: the
// same bits with the top bit set. For IPv4 that is 0.0.0.0 -> 128.0.0.0,
// for IPv6 :: -> 8000::; both live on their own byte edges, so (as4 or
// as16) index 0 is the top bit in either family.
func otherHalf(a netip.Addr) netip.Addr {
	if a.Is4() {
		b := a.As4()
		b[0] |= 0x80
		return netip.AddrFrom4(b)
	}
	b := a.As16()
	b[0] |= 0x80
	out, _ := netip.AddrFromSlice(b[:])
	return out
}

// addRouteList writes routes through the IP Helper API. NextHop stays
// zero — on-link through the adapter itself, which is how wintun routes go.
func addRouteList(luid winipcfg.LUID, destinations []netip.Prefix) error {
	for _, destination := range destinations {
		row := winipcfg.MibIPforwardRow2{}
		row.Init()
		row.InterfaceLUID = luid
		row.Metric = 0
		if err := row.DestinationPrefix.SetPrefix(destination); err != nil {
			return fmt.Errorf("tunfast: route %s: %w", destination, err)
		}
		if err := row.Create(); err != nil {
			return fmt.Errorf("tunfast: route %s: %w", destination, err)
		}
	}
	return nil
}
