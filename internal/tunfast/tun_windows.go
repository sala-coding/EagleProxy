//go:build windows

// Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
// Adapted from sing-tun (github.com/sagernet/sing-tun) and its WireGuard
// upstream, MIT. The fast bring-up karing benefits from lives here.

package tunfast

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"eagle/internal/tunfast/internal/winipcfg"
	"eagle/internal/tunfast/internal/winsys"
	"eagle/internal/tunfast/internal/wintun"

	"golang.org/x/sys/windows"
)

// ipv6AllRoutersMulticast is ff02::15:2, the group the three NDP messages
// below are sent to; when IPv6 has no tunnel address, the strict-route
// policy has to let them through or the physical network stops talking.
var ipv6AllRoutersMulticast = winsys.FWP_BYTE_ARRAY16{ByteArray16: [16]uint8{0xff, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}}

// tunnelType is the adapter type string. Keep it identical to sing-tun's:
// the adapter entry a killed karing or sing-box core leaves behind stays
// recognisable, and OpenAdapter below re-opens it instead of failing.
const tunnelType = "sing-tun"

// ringCapacity is handed to WintunStartSession, 8 MiB like karing: enough
// to absorb hundreds of frames of burst, small enough that allocation and
// teardown never show up in the bring-up path.
const ringCapacity = 0x800000

type nativeTun struct {
	adapter  *wintun.Adapter
	options  Options
	session  wintun.Session
	readWait windows.Handle

	// running counts every call sitting inside Read or Write. Close must
	// wait for them to end the session: afterwards the ring buffer does
	// not exist, and a late ReleaseReceivePacket would touch freed memory.
	running sync.WaitGroup

	closeOnce sync.Once
	closing   atomic.Bool
	fwpm      uintptr
}

var (
	// kernel32 is only asked for SetEvent: the read-wait event wintun
	// hands back is a real event handle, and nothing else wakes a blocked
	// reader — without this, Close on a quiet link would hang until a
	// packet arrived.
	modkernel32  = windows.NewLazySystemDLL("kernel32.dll")
	procSetEvent = modkernel32.NewProc("SetEvent")
	moddnsapi    = windows.NewLazySystemDLL("dnsapi.dll")
	procFlushDNS = moddnsapi.NewProc("DnsFlushResolverCache")
)

func init() {
	DeleteDriver = func() error {
		return wintun.Uninstall()
	}
}

// Open brings an adapter up with the karing fast path and returns the
// handle packets flow through. It must run elevated, which TUN mode
// already guarantees by the time the core launches.
func Open(options Options) (Interface, error) {
	if options.Name == "" {
		return nil, errors.New("tunfast: adapter name is required")
	}
	if options.MTU == 0 {
		options.MTU = 1500
	}
	// The identity is the whole trick: md5("wintun"+name) is the same GUID
	// the previous session used, so CreateAdapter answers
	// ERROR_OBJECT_ALREADY_EXISTS for the adapter that is already there,
	// and OpenAdapter picks it up untouched. Creating an adapter is the
	// only step that can invoke the driver service machinery; skipping it
	// is where the seconds go.
	guid := generateGUIDByDeviceName(options.Name)
	adapter, err := wintun.CreateAdapter(options.Name, tunnelType, guid)
	if err != nil {
		if !isAdapterAlreadyExists(err) {
			return nil, fmt.Errorf("tunfast: create adapter: %w", err)
		}
		createErr := err
		adapter, err = wintun.OpenAdapter(options.Name)
		if err != nil {
			return nil, fmt.Errorf("tunfast: create adapter: %w; open existing adapter: %w", createErr, err)
		}
	}
	tun := &nativeTun{
		adapter: adapter,
		options: options,
	}
	session, err := adapter.StartSession(ringCapacity)
	if err != nil {
		adapter.Close()
		return nil, fmt.Errorf("tunfast: start session: %w", err)
	}
	tun.session = session
	tun.readWait = session.ReadWaitEvent()
	if err = tun.configure(); err != nil {
		session.End()
		adapter.Close()
		return nil, err
	}
	if options.AutoRoute {
		if err = tun.startRoutes(); err != nil {
			// startRoutes may have put its /1 halves down before failing (the
			// strict-route filters go last), and Close does not take them back:
			// it ends the session and closes the handle, while the wintun device
			// — and the routes pointing the machine's whole IPv4/IPv6 traffic at
			// it — stay. Longest-prefix match then sends every packet into an
			// adapter nobody reads: the machine loses the internet outright
			// until the routes are cleaned by hand. The metric configure()
			// pinned at 0 goes back with them for the same reason an idle
			// adapter sits at 9999 (setInterfaceMetric): 0 without the capture
			// wins Windows' own per-interface picks — the NCSI probe above all.
			luid := winipcfg.LUID(tun.adapter.LUID())
			_ = clearCaptureRoutes(luid, options.Name)
			_ = setInterfaceMetric(luid, options.Name,
				len(options.Inet4Address) > 0, len(options.Inet6Address) > 0, linkMetricIdle)
			tun.Close()
			return nil, err
		}
	}
	return tun, nil
}

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

// configure assigns addresses, DNS and interface flags through the IP
// Helper APIs. No child process is spawned anywhere here — every call
// lands in the kernel in microseconds, and a failure returns a typed
// errno instead of a string to parse.
func (t *nativeTun) configure() error {
	luid := winipcfg.LUID(t.adapter.LUID())
	if len(t.options.Inet4Address) > 0 {
		if err := luid.SetIPAddressesForFamily(windows.AF_INET, t.options.Inet4Address); err != nil {
			return fmt.Errorf("tunfast: set ipv4 address: %w", err)
		}
		// The setting is applied even when the list is empty: that clears
		// what a previous session left on the adapter, without needing to
		// know what that session wrote.
		if err := luid.SetDNS(windows.AF_INET, dnsAddrs(t.options.DNSServers, 4), nil); err != nil {
			return fmt.Errorf("tunfast: set ipv4 dns: %w", err)
		}
	}
	if len(t.options.Inet6Address) > 0 {
		if err := luid.SetIPAddressesForFamily(windows.AF_INET6, t.options.Inet6Address); err != nil {
			return fmt.Errorf("tunfast: set ipv6 address: %w", err)
		}
		if err := luid.SetDNS(windows.AF_INET6, dnsAddrs(t.options.DNSServers, 6), nil); err != nil {
			return fmt.Errorf("tunfast: set ipv6 dns: %w", err)
		}
	}
	if len(t.options.Inet4Address) > 0 || len(t.options.Inet6Address) > 0 {
		_ = luid.DisableDNSRegistration()
	}
	if len(t.options.Inet4Address) > 0 {
		inetIf, err := luid.IPInterface(windows.AF_INET)
		if err != nil {
			return fmt.Errorf("tunfast: read ipv4 interface: %w", err)
		}
		inetIf.ForwardingEnabled = true
		inetIf.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
		inetIf.DadTransmits = 0
		inetIf.ManagedAddressConfigurationSupported = false
		inetIf.OtherStatefulConfigurationSupported = false
		inetIf.NLMTU = t.options.MTU
		if t.options.AutoRoute {
			// Metric 0 lets the route written in startRoutes win over the
			// physical adapter's default without deleting that route,
			// which is what keeps the stop - start cycle cheap.
			inetIf.UseAutomaticMetric = false
			inetIf.Metric = 0
		}
		if err := inetIf.Set(); err != nil {
			return fmt.Errorf("tunfast: set ipv4 options: %w", err)
		}
	}
	if len(t.options.Inet6Address) > 0 {
		inet6If, err := luid.IPInterface(windows.AF_INET6)
		if err != nil {
			return fmt.Errorf("tunfast: read ipv6 interface: %w", err)
		}
		inet6If.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
		inet6If.DadTransmits = 0
		inet6If.ManagedAddressConfigurationSupported = false
		inet6If.OtherStatefulConfigurationSupported = false
		inet6If.NLMTU = t.options.MTU
		if t.options.AutoRoute {
			inet6If.UseAutomaticMetric = false
			inet6If.Metric = 0
		}
		if err := inet6If.Set(); err != nil {
			return fmt.Errorf("tunfast: set ipv6 options: %w", err)
		}
	}
	return nil
}

// startRoutes installs the default routes and, under StrictRoute, the WFP
// filters. One CreateIpForwardEntry2 per prefix block; the filters go
// into a dynamic WFP session the kernel clears when this process ends, so
// there is nothing to undo before an instant restart.
func (t *nativeTun) startRoutes() error {
	luid := winipcfg.LUID(t.adapter.LUID())
	if err := addRouteList(luid, t.routeRanges()); err != nil {
		return err
	}
	flushResolverCache()
	if t.options.StrictRoute {
		if err := t.applyStrictRoute(luid); err != nil {
			return err
		}
	}
	return nil
}

// routeRanges is the set of prefixes run through addRouteList. Each family
// with an address gets its /0 split into two /1 halves: Windows matches
// the most specific prefix first and then the lowest metric, so halves at
// metric 0 beat the physical default route while the physical route
// itself stays intact — stopping the adapter restores the network without
// rewriting anything. Excluding a family drops that family's halves: a
// remaining /0 would swallow the excluded prefixes again, since honoring
// the exclusion is the caller's longer-prefix business, not ours.
func (t *nativeTun) routeRanges() []netip.Prefix {
	excluded4, excluded6 := false, false
	for _, p := range t.options.ExcludedRoutes {
		if p.Addr().Is4() {
			excluded4 = true
		} else {
			excluded6 = true
		}
	}
	var ranges []netip.Prefix
	if len(t.options.Inet4Address) > 0 && !excluded4 {
		ranges = append(ranges, halve(netip.MustParsePrefix("0.0.0.0/0"))...)
	}
	if len(t.options.Inet6Address) > 0 && !excluded6 {
		ranges = append(ranges, halve(netip.MustParsePrefix("::/0"))...)
	}
	return ranges
}

// halve turns a /0 into its two /1 halves, 0.0.0.0/0 into 0.0.0.0/1 plus
// 128.0.0.0/1 and ::/0 into ::/1 plus 8000::/1.
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

// addRouteList writes the routes at metric 0. NextHop stays zero —
// on-link through the adapter itself, which is how wintun routes go.
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

// dnsAddrs splits the resolver list by family, keeping the user's order.
func dnsAddrs(all []netip.Addr, family int) []netip.Addr {
	var out []netip.Addr
	for _, a := range all {
		if family == 4 && a.Is4() {
			out = append(out, a)
		}
		if family == 6 && a.Is6() && !a.Is4In6() {
			out = append(out, a)
		}
	}
	return out
}

// flushResolverCache throws away Windows' cached answers right after the
// DNS setting changes, via DnsFlushResolverCache. Best effort: the call
// shortens how long stale answers live, it never decides whether traffic
// works.
func flushResolverCache() {
	if err := procFlushDNS.Find(); err != nil {
		return
	}
	procFlushDNS.Call()
}

// Read returns one packet. The wait-side story is what makes this fast
// and idle-free at once: the loop asks the ring once, and when it is
// empty it sleeps on the kernel event wintun signals, so there is no
// busy CPU and no poll interval between a packet and the reader.
func (t *nativeTun) Read(p []byte) (n int, err error) {
	t.running.Add(1)
	defer t.running.Done()
	for {
		if t.closing.Load() {
			return 0, os.ErrClosed
		}
		packet, errno := t.session.ReceivePacket()
		switch errno {
		case 0:
			if len(packet) > len(p) {
				t.session.ReleaseReceivePacket(packet)
				continue // oversized frame; the next one fits
			}
			n = copy(p, packet)
			t.session.ReleaseReceivePacket(packet)
			return
		case windows.ERROR_NO_MORE_ITEMS:
			windows.WaitForSingleObject(t.readWait, windows.INFINITE)
			continue
		case windows.ERROR_HANDLE_EOF:
			return 0, os.ErrClosed
		case windows.ERROR_INVALID_DATA:
			return 0, errors.New("tunfast: send ring corrupt")
		default:
			return 0, fmt.Errorf("tunfast: read packet: %w", errno)
		}
	}
}

// Write queues one packet. A full ring (ERROR_BUFFER_OVERFLOW) drops the
// frame without an error: the ring back-pressures by design, losing one
// frame is cheaper than stalling the writer, and TCP recovers while a
// blocked stream does not.
func (t *nativeTun) Write(p []byte) (n int, err error) {
	t.running.Add(1)
	defer t.running.Done()
	if t.closing.Load() {
		return 0, os.ErrClosed
	}
	packet, errno := t.session.AllocateSendPacket(len(p))
	switch errno {
	case 0:
		copy(packet, p)
		t.session.SendPacket(packet)
		return len(p), nil
	case windows.ERROR_HANDLE_EOF:
		return 0, os.ErrClosed
	case windows.ERROR_BUFFER_OVERFLOW:
		return 0, nil
	default:
		return 0, fmt.Errorf("tunfast: send packet: %w", errno)
	}
}

// Close stops everything in the only order that cannot leak: wake the
// readers, wait for them out of the session, end the session, close the
// adapter. Under StrictRoute the filters belong to a dynamic WFP session,
// which vanishes with the engine handle right here — and on a crash with
// the process itself, so a bad exit never leaves filters behind to slow
// the next start.
func (t *nativeTun) Close() error {
	t.closeOnce.Do(func() {
		t.closing.Store(true)
		procSetEvent.Call(uintptr(t.readWait))
		t.running.Wait()
		if t.session != (wintun.Session{}) {
			t.session.End()
		}
		t.adapter.Close()
		if t.fwpm != 0 {
			winsys.FwpmEngineClose0(t.fwpm)
		}
		if t.options.AutoRoute {
			flushResolverCache()
		}
	})
	return nil
}

// LUID implements Interface.
func (t *nativeTun) LUID() uint64 {
	return t.adapter.LUID()
}

// applyStrictRoute installs the WFP filters. Weight decides who wins: the
// filters are additive, each layer below the permit layers blocks a wider
// set, and everything lives in one dynamic session closed in Close. The
//
// layers, in the order karing's sing-tun installs them:
//
//	permit 13  — this process's own outbound, so neither the tunnel's
//	             server connection nor the SOCKS hop to the core is eaten
//	             by the block below;
//	permit 13  — the three NDP discovery messages when IPv6 carries no
//	             tunnel address, so the physical network keeps functioning;
//	block  12  — IPv6 as a whole when IPv6 has no tunnel address;
//	permit 11  — everything arriving on the adapter itself, TCP and UDP:
//	             the packets coming out of the tunnel;
//	block  10  — UDP port 53 when DNS is hijacked, so resolver traffic
//	             cannot sneak out past the adapter's DNS setting.
func (t *nativeTun) applyStrictRoute(luid winipcfg.LUID) error {
	engine, err := openFwpmSession()
	if err != nil {
		return err
	}
	t.fwpm = engine

	subLayer := winsys.FWPM_SUBLAYER0{}
	subLayerKey, err := windows.GenerateGUID()
	if err != nil {
		return fmt.Errorf("tunfast: strict route: %w", err)
	}
	subLayer.SubLayerKey = subLayerKey
	subLayer.DisplayData = winsys.CreateDisplayData(tunnelType, "auto-route rules")
	subLayer.Weight = 65535
	if err := winsys.FwpmSubLayerAdd0(engine, &subLayer, 0); err != nil {
		return fmt.Errorf("tunfast: strict route sublayer: %w", err)
	}

	// Permit this process so the tunnel's own connections survive the
	// block filters below.
	processAppID, err := winsys.GetCurrentProcessAppID()
	if err != nil {
		return fmt.Errorf("tunfast: strict route: %w", err)
	}
	defer winsys.FwpmFreeMemory0(unsafe.Pointer(&processAppID))

	permitConditions := []winsys.FWPM_FILTER_CONDITION0{{
		FieldKey:  winsys.FWPM_CONDITION_ALE_APP_ID,
		MatchType: winsys.FWP_MATCH_EQUAL,
		ConditionValue: winsys.FWP_CONDITION_VALUE0{
			Type:  winsys.FWP_BYTE_BLOB_TYPE,
			Value: uintptr(unsafe.Pointer(processAppID)),
		},
	}}
	for _, layer := range []windows.GUID{winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V4, winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V6} {
		filter := winsys.FWPM_FILTER0{}
		filter.FilterCondition = &permitConditions[0]
		filter.NumFilterConditions = 1
		filter.DisplayData = winsys.CreateDisplayData(tunnelType, "protect self")
		filter.SubLayerKey = subLayerKey
		filter.LayerKey = layer
		filter.Action.Type = winsys.FWP_ACTION_PERMIT
		filter.Weight.Type = winsys.FWP_UINT8
		filter.Weight.Value = uintptr(13)
		filter.Flags = winsys.FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT
		var id uint64
		if err := winsys.FwpmFilterAdd0(engine, &filter, 0, &id); err != nil {
			return fmt.Errorf("tunfast: strict route permit: %w", err)
		}
	}

	if len(t.options.Inet6Address) == 0 {
		// No IPv6 through the tunnel: keep NDP working, block the rest of
		// the family so v6 traffic cannot leak around a v4-only tunnel.
		for _, ndp := range []struct {
			name     string
			icmpType uint16
			conds    int
		}{
			{"router solicitation", 133, 4},
			{"neighbor solicitation", 135, 3},
			{"neighbor advertisement", 136, 3},
		} {
			conditions := make([]winsys.FWPM_FILTER_CONDITION0, 4)
			conditions[0] = winsys.FWPM_FILTER_CONDITION0{
				FieldKey:  winsys.FWPM_CONDITION_IP_PROTOCOL,
				MatchType: winsys.FWP_MATCH_EQUAL,
				ConditionValue: winsys.FWP_CONDITION_VALUE0{
					Type:  winsys.FWP_UINT8,
					Value: uintptr(winsys.IPPROTO_ICMPV6),
				},
			}
			conditions[1] = winsys.FWPM_FILTER_CONDITION0{
				FieldKey:  winsys.FWPM_CONDITION_ICMP_TYPE,
				MatchType: winsys.FWP_MATCH_EQUAL,
				ConditionValue: winsys.FWP_CONDITION_VALUE0{
					Type:  winsys.FWP_UINT16,
					Value: uintptr(ndp.icmpType),
				},
			}
			conditions[2] = winsys.FWPM_FILTER_CONDITION0{
				FieldKey:  winsys.FWPM_CONDITION_ICMP_CODE,
				MatchType: winsys.FWP_MATCH_EQUAL,
				ConditionValue: winsys.FWP_CONDITION_VALUE0{
					Type:  winsys.FWP_UINT16,
					Value: 0,
				},
			}
			conditions[3] = winsys.FWPM_FILTER_CONDITION0{
				FieldKey:  winsys.FWPM_CONDITION_IP_REMOTE_ADDRESS,
				MatchType: winsys.FWP_MATCH_EQUAL,
				ConditionValue: winsys.FWP_CONDITION_VALUE0{
					Type:  winsys.FWP_BYTE_ARRAY16_TYPE,
					Value: uintptr(unsafe.Pointer(&ipv6AllRoutersMulticast)),
				},
			}
			filter := winsys.FWPM_FILTER0{}
			filter.FilterCondition = &conditions[0]
			filter.NumFilterConditions = uint32(ndp.conds)
			filter.DisplayData = winsys.CreateDisplayData(tunnelType, "allow ipv6 "+ndp.name)
			filter.SubLayerKey = subLayerKey
			filter.LayerKey = winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V6
			filter.Action.Type = winsys.FWP_ACTION_PERMIT
			filter.Weight.Type = winsys.FWP_UINT8
			filter.Weight.Value = uintptr(13)
			var id uint64
			if err := winsys.FwpmFilterAdd0(engine, &filter, 0, &id); err != nil {
				return fmt.Errorf("tunfast: strict route %s: %w", ndp.name, err)
			}
		}
		blockFilter := winsys.FWPM_FILTER0{}
		blockFilter.DisplayData = winsys.CreateDisplayData(tunnelType, "block ipv6")
		blockFilter.SubLayerKey = subLayerKey
		blockFilter.LayerKey = winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V6
		blockFilter.Action.Type = winsys.FWP_ACTION_BLOCK
		blockFilter.Weight.Type = winsys.FWP_UINT8
		blockFilter.Weight.Value = uintptr(12)
		var id uint64
		if err := winsys.FwpmFilterAdd0(engine, &blockFilter, 0, &id); err != nil {
			return fmt.Errorf("tunfast: strict route block ipv6: %w", err)
		}
	}

	// Allow everything that arrives on the adapter itself: the packets
	// coming out of the tunnel own this path.
	netInterface, err := net.InterfaceByName(t.options.Name)
	if err != nil {
		return fmt.Errorf("tunfast: strict route: %w", err)
	}
	tunConditions := []winsys.FWPM_FILTER_CONDITION0{{
		FieldKey:  winsys.FWPM_CONDITION_LOCAL_INTERFACE_INDEX,
		MatchType: winsys.FWP_MATCH_EQUAL,
		ConditionValue: winsys.FWP_CONDITION_VALUE0{
			Type:  winsys.FWP_UINT32,
			Value: uintptr(uint32(netInterface.Index)),
		},
	}}
	for _, layer := range []windows.GUID{winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V4, winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V6} {
		filter := winsys.FWPM_FILTER0{}
		filter.FilterCondition = &tunConditions[0]
		filter.NumFilterConditions = 1
		filter.DisplayData = winsys.CreateDisplayData(tunnelType, "allow tun interface")
		filter.SubLayerKey = subLayerKey
		filter.LayerKey = layer
		filter.Action.Type = winsys.FWP_ACTION_PERMIT
		filter.Weight.Type = winsys.FWP_UINT8
		filter.Weight.Value = uintptr(11)
		var id uint64
		if err := winsys.FwpmFilterAdd0(engine, &filter, 0, &id); err != nil {
			return fmt.Errorf("tunfast: strict route allow tun: %w", err)
		}
	}

	if t.options.HijackDNS {
		// DNS leaving the machine on any interface is dropped: resolvers
		// must answer through the adapter, whose DNS setting decides.
		portConditions := []winsys.FWPM_FILTER_CONDITION0{{
			FieldKey:  winsys.FWPM_CONDITION_IP_REMOTE_PORT,
			MatchType: winsys.FWP_MATCH_EQUAL,
			ConditionValue: winsys.FWP_CONDITION_VALUE0{
				Type:  winsys.FWP_UINT16,
				Value: uintptr(uint16(53)),
			},
		}}
		for _, layer := range []windows.GUID{winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V4, winsys.FWPM_LAYER_ALE_AUTH_CONNECT_V6} {
			filter := winsys.FWPM_FILTER0{}
			filter.FilterCondition = &portConditions[0]
			filter.NumFilterConditions = 1
			filter.DisplayData = winsys.CreateDisplayData(tunnelType, "block dns")
			filter.SubLayerKey = subLayerKey
			filter.LayerKey = layer
			filter.Action.Type = winsys.FWP_ACTION_BLOCK
			filter.Weight.Type = winsys.FWP_UINT8
			filter.Weight.Value = uintptr(10)
			var id uint64
			if err := winsys.FwpmFilterAdd0(engine, &filter, 0, &id); err != nil {
				return fmt.Errorf("tunfast: strict route block dns: %w", err)
			}
		}
	}
	return nil
}

// openFwpmSession opens the dynamic session the filters live under.
// DYNAMIC means the kernel owns the lifetime: when the handle closes — or
// the process ends, cleanly or not — every filter in the session
// disappears, which is what banner, restart-without-leak cleanup rides on.
func openFwpmSession() (uintptr, error) {
	session := &winsys.FWPM_SESSION0{Flags: winsys.FWPM_SESSION_FLAG_DYNAMIC}
	var engine uintptr
	if err := winsys.FwpmEngineOpen0(nil, winsys.RPC_C_AUTHN_DEFAULT, nil, session, unsafe.Pointer(&engine)); err != nil {
		return 0, fmt.Errorf("tunfast: strict route engine: %w", err)
	}
	return engine, nil
}
