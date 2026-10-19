//go:build windows

// Package sysproxy configures the Windows system proxy.
//
// Ported from nekoray's QvProxyConfigurator
// (3rdparty/qv2ray/v2/components/proxy/QvProxyConfigurator.cpp): the WinINet
// INTERNET_PER_CONN_OPTION_LIST API, applied to the LAN connection and to every
// RAS connectoid. The difference from writing the registry key directly — which
// is what this package used to do — shows up on machines with dial-up or VPN
// connections: Windows keeps one proxy setting per connection, and
// HKCU\...\Internet Settings only ever describes the default one, so a
// connectoid would keep sending traffic straight out while the app reported the
// system proxy as on.
package sysproxy

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	internetOptionPerConnectionOption = 75
	internetOptionRefresh             = 37
	internetOptionSettingsChange      = 39

	internetPerConnFlags         = 1
	internetPerConnProxyServer   = 2
	internetPerConnProxyBypass   = 3
	internetPerConnAutoConfigURL = 4

	proxyTypeDirect       = 0x00000001
	proxyTypeProxy        = 0x00000002
	proxyTypeAutoProxyURL = 0x00000004

	// The default bypass: loopback and single-label names should never be sent to the
	// local inbound. It is a default rather than the value now — the list is a setting
	// (see sp_bypass), because a reader whose LAN addresses or NAS must answer directly
	// has no other way to say so. Empty in the setting means this.
	defaultBypass = "<local>"
)

var procInternetSetOption = syscall.NewLazyDLL("wininet.dll").NewProc("InternetSetOptionW")

// perConnOption mirrors INTERNET_PER_CONN_OPTIONW. value is the union: a DWORD
// for the flags, an LPWSTR for the server/bypass/PAC fields, which is why it is
// carried as a uintptr rather than as a typed field.
type perConnOption struct {
	dwOption uint32
	value    uintptr
}

// perConnOptionList mirrors INTERNET_PER_CONN_OPTION_LISTW.
type perConnOptionList struct {
	dwSize        uint32
	pszConnection *uint16
	dwOptionCount uint32
	dwOptionError uint32
	pOptions      *perConnOption
}

// Set enables the system proxy. format may contain {ip}, {http_port}, {socks_port}.
// Set points the machine at the local inbound. bypass is the semicolon-separated list
// Windows is told never to send through a proxy; empty means defaultBypass, which is
// what every machine had before the list could be edited.
func Set(format, ip string, httpPort, socksPort int, bypass string) error {
	if format == "" {
		format = "{ip}:{http_port}"
	}
	server := strings.NewReplacer(
		"{ip}", ip,
		"{http_port}", strconv.Itoa(httpPort),
		"{socks_port}", strconv.Itoa(socksPort),
	).Replace(format)
	if strings.TrimSpace(bypass) == "" {
		bypass = defaultBypass
	}

	serverPtr, err := windows.UTF16PtrFromString(server)
	if err != nil {
		return err
	}
	bypassPtr, err := windows.UTF16PtrFromString(bypass)
	if err != nil {
		return err
	}
	opts := []perConnOption{
		{dwOption: internetPerConnFlags, value: uintptr(proxyTypeDirect | proxyTypeProxy)},
		{dwOption: internetPerConnProxyServer, value: uintptr(unsafe.Pointer(serverPtr))},
		{dwOption: internetPerConnProxyBypass, value: uintptr(unsafe.Pointer(bypassPtr))},
	}
	if err := applyToAllConnections(opts); err != nil {
		return err
	}
	// the pointers above are referred to only through uintptr values, which the
	// garbage collector cannot see — keep them reachable until the calls are done
	runtime.KeepAlive(serverPtr)
	runtime.KeepAlive(bypassPtr)
	// The setting is written; notify only asks running programs to re-read it. Reporting
	// a failure here made the caller roll back a switch whose registry entry was already
	// in place — the window said off while the machine was proxied, and the next press
	// went the wrong way round.
	_ = notify()
	return nil
}

// SetPAC points the system at a proxy auto-config script instead of a fixed
// server. NekoRay carries the same branch (isPAC); nothing in this app uses it
// yet, but the capability belongs to the port.
func SetPAC(url string) error {
	if url == "" {
		return Clear()
	}
	pacPtr, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	opts := []perConnOption{
		{dwOption: internetPerConnFlags, value: uintptr(proxyTypeDirect | proxyTypeAutoProxyURL)},
		{dwOption: internetPerConnAutoConfigURL, value: uintptr(unsafe.Pointer(pacPtr))},
	}
	if err := applyToAllConnections(opts); err != nil {
		return err
	}
	runtime.KeepAlive(pacPtr)
	// Written, not merely announced: see the note in Set.
	_ = notify()
	return nil
}

// Clear disables the system proxy.
func Clear() error {
	opts := []perConnOption{
		{dwOption: internetPerConnFlags, value: uintptr(proxyTypeDirect)},
	}
	if err := applyToAllConnections(opts); err != nil {
		return err
	}
	// Written, not merely announced: see the note in Set.
	_ = notify()
	return nil
}

// Current reports what Windows is configured to send requests through: whether the proxy
// is enabled, and the server list it carries (empty when only a PAC script is set).
//
// Read from the registry rather than through WinINet because the question is about the
// stored setting — the one that survives a crash — not about what a running browser
// currently holds.
func Current() (enabled bool, server string, err error) {
	path, err := windows.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if err != nil {
		return false, "", err
	}
	var key windows.Handle
	if err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, path, 0, windows.KEY_READ, &key); err != nil {
		return false, "", err
	}
	defer func() { _ = windows.RegCloseKey(key) }()

	name, _ := windows.UTF16PtrFromString("ProxyEnable")
	var typ, size uint32 = 0, 4
	var on uint32
	if err := windows.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&on)), &size); err != nil {
		return false, "", err
	}
	enabled = on != 0

	name, _ = windows.UTF16PtrFromString("ProxyServer")
	typ, size = 0, 0
	if err := windows.RegQueryValueEx(key, name, nil, &typ, nil, &size); err != nil {
		// Enabled with no server is a state nothing can act on.
		return enabled, "", nil
	}
	buf := make([]uint16, size/2+1)
	if err := windows.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size); err != nil {
		return enabled, "", err
	}
	return enabled, windows.UTF16ToString(buf), nil
}

// applyToAllConnections writes the options to the LAN connection first and then
// to every RAS connectoid, mirroring QvProxyConfigurator::__SetProxyOptions.
//
// The LAN write is the one that matters on an ordinary machine, so its failure is
// reported rather than hidden behind a connectoid that happened to accept the same
// options: a machine with any dial-up or VPN entry would otherwise answer "written" for a
// default connection that never moved, and the app would show its switch on over traffic
// that still went direct. The connectoids are best-effort in the other direction — one
// that refuses is not a reason to fail a setting the default connection took.
func applyToAllConnections(opts []perConnOption) error {
	lanErr := applyToConnection(nil, opts)

	for _, name := range rasEntries() {
		conn, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		_ = applyToConnection(conn, opts)
	}
	return lanErr
}

func applyToConnection(conn *uint16, opts []perConnOption) error {
	if len(opts) == 0 {
		return nil
	}
	list := perConnOptionList{
		pszConnection: conn,
		dwOptionCount: uint32(len(opts)),
		pOptions:      &opts[0],
	}
	list.dwSize = uint32(unsafe.Sizeof(list))

	ret, _, callErr := procInternetSetOption.Call(0,
		internetOptionPerConnectionOption,
		uintptr(unsafe.Pointer(&list)),
		uintptr(list.dwSize))
	if ret == 0 {
		return fmt.Errorf("InternetSetOption(PER_CONNECTION_OPTION) failed: %v", callErr)
	}
	return nil
}

// notify tells already-running WinINet/WinHTTP clients that the settings
// changed; without it browsers keep the old proxy until they are restarted.
func notify() error {
	r1, _, e1 := procInternetSetOption.Call(0, internetOptionSettingsChange, 0, 0)
	r2, _, _ := procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
	if r1 == 0 || r2 == 0 {
		return fmt.Errorf("InternetSetOption failed: %v", e1)
	}
	return nil
}

const (
	rasMaxEntryName = 256
	rasMaxPath      = 260
)

// rasEntryName mirrors RASENTRYNAMEW. The field widths are fixed by the API:
// RasEnumEntries rejects any entry whose dwSize is smaller than the structure
// it knows about, so these constants cannot be trimmed.
type rasEntryName struct {
	dwSize          uint32
	szEntryName     [rasMaxEntryName + 1]uint16
	dwFlags         uint32
	szPhonebookPath [rasMaxPath + 1]uint16
}

// rasEntries lists the dial-up / VPN connectoids defined on this machine. An
// empty result is normal (no RAS on the box) and is not an error.
func rasEntries() []string {
	proc := syscall.NewLazyDLL("rasapi32.dll").NewProc("RasEnumEntriesW")
	if err := proc.Find(); err != nil {
		return nil
	}
	entrySize := uint32(unsafe.Sizeof(rasEntryName{}))

	var cb uint32
	var count uint32
	// A first call with a NULL buffer reports the size that is needed.
	_, _, _ = proc.Call(0, 0, 0, uintptr(unsafe.Pointer(&cb)), uintptr(unsafe.Pointer(&count)))
	if cb == 0 {
		return nil
	}

	buf := make([]rasEntryName, int(cb/entrySize)+1)
	for i := range buf {
		buf[i].dwSize = entrySize
	}
	cb = uint32(len(buf)) * entrySize
	ret, _, _ := proc.Call(0, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&cb)),
		uintptr(unsafe.Pointer(&count)))
	if ret != 0 {
		return nil
	}

	names := make([]string, 0, count)
	for i := 0; i < int(count) && i < len(buf); i++ {
		if name := windows.UTF16ToString(buf[i].szEntryName[:]); name != "" {
			names = append(names, name)
		}
	}
	return names
}
