// Package tunfast replicates the fast bring-up path karing takes on Windows:
// the wintun driver DLL embedded in the binary and loaded straight from
// memory, a deterministic adapter GUID so an adapter left over from an
// earlier session is opened instead of re-created, and every bit of adapter
// configuration done through Win32 APIs — never by spawning netsh.
//
// What each choice buys, in the order it matters at start-up:
//
//   - memmod loading means the driver is never written to disk and never
//     searched for: the CreateAdapter call the adapter lives or dies on is
//     reached without touching the filesystem. A fetch from PATH or
//     System32 costs milliseconds at best and a missing file at worst,
//     usually surfacing as "wintun: Failed to setup adapter".
//   - md5("wintun"+name) as the adapter GUID keeps the identity stable
//     across restarts. wintun then fails CreateAdapter with
//     ERROR_OBJECT_ALREADY_EXISTS on an adapter that is already there, and
//     the fast path is OpenAdapter instead: creating an adapter is the
//     only step that can invoke the driver service, and skipping it is the
//     single largest constant in a restart. A stable GUID also keeps the
//     network-profile entry (NLA) the same, so Windows does not spend the
//     start re-classifying a "new" network.
//   - API-only configuration removes the child processes. One netsh run
//     costs hundreds of milliseconds before it answers, and a bring-up
//     that needs several serialises that into seconds. Win32 calls are
//     nanoseconds to microseconds and fail with a real errno instead of a
//     string to parse.
//   - OpenSession uses an 8 MiB ring so a burst in either direction never
//     drops, and the read side sleeps on the kernel event rather than
//     polling, so idle CPU is zero and the first packet after sleep is not
//     late by a poll interval.
//
// Usage:
//
//	iface, err := tunfast.Open(tunfast.Options{
//	    Name:         "EagleTun",
//	    Inet4Address: []netip.Prefix{netip.MustParsePrefix("172.19.0.2/30")},
//	    DNSServers:   []netip.Addr{netip.MustParseAddr("172.19.0.1")},
//	    MTU:          1500,
//	    AutoRoute:    true,
//	})
//	if err != nil { return err }
//	defer iface.Close()
//	n, err := iface.Read(buf)   // blocking, wakes on kernel event
//	_ = iface.Write(packet)     // ENOBUFS is dropped like wintun does
//
// Bringing an adapter up requires the administrator token, exactly as TUN
// mode already holds it when the core launches. StrictRoute additionally
// installs WFP filters under a dynamic session, which Windows tears down
// on process exit by itself — no cleanup pass is needed before the next
// start, which is the other half of why karing restarts instantly.
package tunfast

import (
	"io"
	"net/netip"
)

// Options is what Open needs to bring an adapter up. It mirrors the fields
// sing-box would put in its tun inbound, so a config that already feeds the
// core can feed this package with the same values.
type Options struct {
	// Name is the adapter's friendly name, e.g. "EagleTun". The GUID is
	// derived from this name, so a restart opens the same adapter.
	Name string

	// Inet4Address / Inet6Address are the addresses assigned to the
	// adapter, with the prefix length, e.g. 172.19.0.2/30.
	Inet4Address []netip.Prefix
	Inet6Address []netip.Prefix

	// DNSServers are published on the adapter when AutoRoute is on, so
	// resolver traffic actually enters the tunnel. They may be in either
	// family; each is applied to the family it belongs to.
	DNSServers []netip.Addr

	// MTU of the adapter; 0 means 1500.
	MTU uint32

	// AutoRoute installs a default route through the adapter with metric
	// 0, which beats the physical adapter's default route without
	// touching it. Routes in ExcludedRoutes are skipped so their traffic
	// keeps leaving through the physical adapter.
	AutoRoute bool

	// ExcludedRoutes are prefixes that must NOT be routed into the tunnel
	// when AutoRoute is on. The caller is responsible for keeping an OS
	// route for them outside the adapter; sing-box's auto_route machinery
	// does this today and its routes remain in place.
	ExcludedRoutes []netip.Prefix

	// HijackDNS blocks UDP 53 leaving the machine while the tunnel is up,
	// so resolver traffic cannot bypass the adapter's DNS setting.
	HijackDNS bool

	// StrictRoute additionally installs WFP filters that drop everything
	// not permitted through the tunnel, preventing leaks by stale routes
	// of the physical adapter. The filters live in a dynamic WFP session
	// that the kernel removes when this process exits, so a crash leaves
	// nothing behind and the next start never pays for cleanup.
	StrictRoute bool
}

// Interface is the packet handle Open returns. Read blocks until a packet
// arrives or the interface closes; errors after Close are os.ErrClosed.
type Interface interface {
	io.ReadWriteCloser

	// LUID is the Windows layer-2 interface id, needed to recognise this
	// adapter among the machine's interfaces without polling.
	LUID() uint64
}

// DeleteDriver removes the wintun driver service when no adapter is using
// it. This is what karing does when CreateAdapter fails with
// "wintun: Failed to setup adapter" and what the app's stale-adapter
// cleanup calls for: a driver left half-installed by a killed core makes
// every later open fail, and deleting the service is the only way back.
// Needs the administrator token like Open itself.
//
// It removes the shared wintun driver, so it must wait until it belongs to
// no VPN — this process's adapter closed, others may still be using it.
var DeleteDriver func() error
