package main

import (
	"eagle/internal/configbuilder"
	"eagle/internal/util"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The two answers below are asked once a second by the traffic loop, and the
// routing table does not change that often: a dial and an interface walk per
// second is work with nothing behind it. Cached for a while, and only the
// successful answers are kept — an address that could not be read is retried
// on the next ask rather than remembered for half a minute.
const lanCacheTTL = 30 * time.Second

var (
	lanCacheMu sync.Mutex
	lanIPValue string
	lanIPAt    time.Time
	lanAddrSet map[string]bool
	lanAddrAt  time.Time
)

// virtualIfaceHints are the adapter names that answer for a network the reader's
// phone is not on: the app's own tunnel, the hypervisors and container runtimes
// that leave adapters behind, and the capture drivers (TAP) that never carry a
// LAN. Matched case-insensitively against the interface name.
var virtualIfaceHints = []string{"vethernet", "vmware", "virtualbox", "hyper-v", "wsl",
	"docker", "tailscale", "zerotier", "npcap", "tap-", "tun", "loopback"}

// LANIP returns the machine's own address on the local network — the one that
// answers "what does my router see me as" — for the status bar's corner.
//
// The routing table's own answer comes first: a UDP dial that sends nothing
// (UDP has no handshake) asks which interface a default-route hop would leave
// through, so the answer follows the live route rather than the interface order.
// That answer is skipped when it belongs to the app's own tunnel or to a virtual
// adapter — with TUN up, the default route is the tunnel, and its address names
// a network the phone cannot reach — and the physical adapters are walked
// instead. Loopback and a failed dial leave an empty string, which the bar reads
// as "nothing to say here".
func (a *App) LANIP() string {
	lanCacheMu.Lock()
	defer lanCacheMu.Unlock()
	if lanIPValue != "" && time.Since(lanIPAt) < lanCacheTTL {
		return lanIPValue
	}
	lanIPValue = detectLANIP()
	if lanIPValue != "" {
		lanIPAt = time.Now()
	}
	return lanIPValue
}

func detectLANIP() string {
	if ip := routeIP(); ip != "" && !unusableLANIP(ip) {
		return ip
	}
	for _, ip := range physicalIPs() {
		return ip
	}
	return ""
}

// routeIP is the address a default-route hop would leave from.
func routeIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// unusableLANIP reports an address that is this machine's but is not the one a
// phone reaches it on: the app's own TUN adapter, and the virtual adapters the
// hypervisors and container runtimes leave behind.
func unusableLANIP(ip string) bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifi := range ifaces {
		if ifaceHasAddr(ifi, ip) {
			return isVirtualIface(ifi.Name)
		}
	}
	return false
}

// physicalIPs lists the addresses a phone on the same network could dial, on
// adapters that are up, are not loopback and do not carry a virtualIfaceHints
// name. The private IPv4s come first — that is the network a phone is on in
// almost every case — and the IPv6s after them, for a LAN that carries nothing
// else: global and unique-local only, since a link-local address needs a scope
// id the reader cannot type into a phone.
func physicalIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var v4s, v6s []string
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || isVirtualIface(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			ipnet, ok := ad.(*net.IPNet)
			if !ok {
				continue
			}
			// Private only: a public address on an adapter is not what a phone on
			// the same Wi-Fi dials, and a link-local one is not routable at all.
			if v4 := ipnet.IP.To4(); v4 != nil {
				if v4.IsPrivate() {
					v4s = append(v4s, v4.String())
				}
				continue
			}
			if v6 := ipnet.IP.To16(); v6 != nil && !v6.IsLoopback() && !v6.IsLinkLocalUnicast() && !v6.IsUnspecified() {
				v6s = append(v6s, v6.String())
			}
		}
	}
	return append(v4s, v6s...)
}

func ifaceHasAddr(ifi net.Interface, ip string) bool {
	want := net.ParseIP(ip)
	if want == nil {
		return false
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	for _, ad := range addrs {
		ipnet, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		if ipnet.IP.Equal(want) {
			return true
		}
	}
	return false
}

func isVirtualIface(name string) bool {
	l := strings.ToLower(name)
	if strings.EqualFold(name, configbuilder.TunAdapterName()) {
		return true
	}
	for _, h := range virtualIfaceHints {
		if strings.Contains(l, h) {
			return true
		}
	}
	return false
}

// localAddrs is every IPv4 address this machine holds, cached the same way as
// LANIP and for the same reason. The device count reads it as "not someone
// else" — a connection whose source is one of these came from this machine,
// whichever adapter it left through, so a phone is never counted as two and this
// machine is never counted as one.
func localAddrs() map[string]bool {
	lanCacheMu.Lock()
	defer lanCacheMu.Unlock()
	if lanAddrSet != nil && time.Since(lanAddrAt) < lanCacheTTL {
		return lanAddrSet
	}
	set := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, ifi := range ifaces {
			addrs, err := ifi.Addrs()
			if err != nil {
				continue
			}
			for _, ad := range addrs {
				// Both families: the shared inbound listens dual-stack, so a
				// connection from this machine can arrive looking like either.
				if ipnet, ok := ad.(*net.IPNet); ok && ipnet.IP != nil {
					set[ipnet.IP.String()] = true
				}
			}
		}
	}
	lanAddrSet = set
	lanAddrAt = time.Now()
	return set
}

// LANEndpoint is what a phone on the same Wi-Fi dials while LAN sharing is on: the
// machine's LAN address and the inbound port, "ip:port" — the two fields the phone's
// proxy settings ask for, joined so the bar can show them as the one thing to copy.
// Empty when sharing is off or the LAN address cannot be read, which the bar reads
// as "nothing to say here".
func (a *App) LANEndpoint() string {
	s := a.curSetting()
	if s == nil || !s.LANShare || !util.IsValidPort(s.InboundSocksPort) {
		return ""
	}
	ip := a.LANIP()
	if ip == "" {
		return ""
	}
	return net.JoinHostPort(ip, strconv.Itoa(s.InboundSocksPort))
}
