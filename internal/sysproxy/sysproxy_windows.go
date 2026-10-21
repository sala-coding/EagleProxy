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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

// The undo record: what the machine's own proxy was before this app took it
// over, kept on disk so the restore survives a crash — the same bargain
// tunserv's resolver undo makes (sysdns_windows.go). Without it the original
// was gone the moment Set ran: the exit path wrote the DIRECT flag and nothing
// else, so a reader whose machine pointed at a corporate proxy went home on
// direct after one session with Eagle. Written once per takeover (the file
// being present means the takeover already recorded its starting point) and
// removed by the restore, so a Set→Clear→Set cycle remembers each takeover's
// own origin rather than the very first one forever.
//
// SetUndoFile names the record; an empty path disables the whole mechanism and
// leaves Set/Clear exactly as they were (Set overwrites, Clear writes DIRECT).
// The caller decides the path because the app's data directory — where temp/
// lives — is its own, and it can move under a running session.
type undoRecord struct {
	Enable   bool   `json:"enable"`
	Server   string `json:"server"`
	Override string `json:"override"`
	PAC      string `json:"pac"`
}

var (
	undoMu   sync.Mutex
	undoPath string
)

// SetUndoFile points the package at its undo record. Callers set it before the
// first Set/Clear of a session and again after the data directory moves; an
// empty value turns undo off.
func SetUndoFile(path string) {
	undoMu.Lock()
	undoPath = path
	undoMu.Unlock()
}

// HasUndo reports whether a recorded takeover is waiting to be restored.
func HasUndo() bool {
	undoMu.Lock()
	defer undoMu.Unlock()
	if undoPath == "" {
		return false
	}
	_, err := os.Stat(undoPath)
	return err == nil
}

// captureUndo records the machine's current setting, if no takeover is
// already recorded. Read-before-write: the snapshot has to be taken before the
// new values land, and the file's presence is what makes a second Set (the
// mode re-entered while the record is still out) keep the first origin rather
// than record the app's own server as the "original".
func captureUndo() {
	undoMu.Lock()
	path := undoPath
	undoMu.Unlock()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return // a takeover is already recorded; its origin is the one to restore
	}
	enable, server, pac, err := Current()
	if err != nil {
		return // unreadable settings: nothing worth recording, and Set must go on
	}
	rec := undoRecord{Enable: enable, Server: server, PAC: pac}
	// The bypass list too: Set writes its own, and the reader's LAN/NAS rules
	// are exactly the sort of thing that must not be lost to one session.
	if b, err := CurrentBypass(); err == nil {
		rec.Override = b
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// restoreUndo puts the recorded setting back and removes the record. Reports
// whether there was one.
func restoreUndo() (bool, error) {
	undoMu.Lock()
	path := undoPath
	undoMu.Unlock()
	if path == "" {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil // no record: the caller falls back to plain DIRECT
	}
	var rec undoRecord
	if json.Unmarshal(data, &rec) != nil {
		// A torn record is still a record, and it has to be acted on rather
		// than just deleted: `rec` stays zero, which applyUndo reads as "write
		// DIRECT and clear the fields" — the machine ends up direct instead of
		// left pointing at this app's inbound for the rest of the session,
		// with the record that could have fixed it already gone. The error is
		// still reported so the caller's log names the corrupt file.
		rec = undoRecord{}
		if aerr := applyUndo(rec); aerr != nil {
			return true, aerr
		}
		_ = os.Remove(path)
		return true, fmt.Errorf("sysproxy: undo record was unreadable, cleared instead of restored")
	}
	if err := applyUndo(rec); err != nil {
		return true, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return true, fmt.Errorf("sysproxy: undo record kept after restore: %w", err)
	}
	return true, nil
}

// applyUndo writes the four recorded values back through WinINet, the same
// per-connection route Set takes — a bare registry write would miss the
// connectoids Set also touched.
func applyUndo(rec undoRecord) error {
	flags := uint32(proxyTypeDirect)
	if rec.Enable {
		flags |= proxyTypeProxy
	}
	opts := []perConnOption{
		{dwOption: internetPerConnFlags, value: uintptr(flags)},
	}
	// Empty strings clear the value: "no server was set" is the record of a
	// machine that had none, and leaving our server behind would be the very
	// leftover this exists to prevent.
	//
	// keep pins the UTF-16 buffers: the option values are uintptrs, which the
	// GC cannot see — the same bargain Set pays with runtime.KeepAlive.
	var keep []*uint16
	pin := func(s string, opt uint32) {
		if s == "" {
			opts = append(opts, perConnOption{dwOption: opt, value: 0})
			return
		}
		p, err := windows.UTF16PtrFromString(s)
		if err != nil {
			return
		}
		opts = append(opts, perConnOption{dwOption: opt, value: uintptr(unsafe.Pointer(p))})
		keep = append(keep, p)
	}
	pin(rec.Server, internetPerConnProxyServer)
	pin(rec.Override, internetPerConnProxyBypass)
	if rec.PAC != "" {
		flags |= proxyTypeAutoProxyURL
		opts[0].value = uintptr(flags)
	}
	pin(rec.PAC, internetPerConnAutoConfigURL)
	// The release direction: LAN first, but the connectoids are written even
	// when it fails (see applyToAllConnectionsReleasing).
	err := applyToAllConnectionsReleasing(opts)
	runtime.KeepAlive(keep)
	if err != nil {
		return err
	}
	_ = notify()
	return nil
}

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

	// Before anything is written: the original is only still readable now.
	captureUndo()

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
	// Before anything is written: see Set.
	captureUndo()
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

// Clear hands the machine's proxy back: the recorded original when a takeover
// is on record (see SetUndoFile), the DIRECT flag when there is nothing to put
// back — a machine this app never took over, or one whose record went missing.
// The restore path is what makes the exit honest: Clear used to write DIRECT
// unconditionally, so one session with Eagle left a reader's corporate proxy
// setting behind on direct for good.
func Clear() error {
	if restored, err := restoreUndo(); restored {
		return err
	}
	opts := []perConnOption{
		{dwOption: internetPerConnFlags, value: uintptr(proxyTypeDirect)},
	}
	// Releasing, not setting: a connectoid left on this app's proxy because the
	// LAN write failed is exactly the leftover the undo record exists to
	// prevent (see applyToAllConnectionsReleasing).
	if err := applyToAllConnectionsReleasing(opts); err != nil {
		return err
	}
	// Written, not merely announced: see the note in Set.
	_ = notify()
	return nil
}

// Current reports what Windows is configured to send requests through: whether the proxy
// is enabled, the server list it carries (empty when only a PAC script is set) and the
// PAC URL (empty when no script is configured).
//
// The PAC field is not decoration: a PAC takeover writes AutoConfigURL with
// ProxyEnable off, so a reader that only asked "enabled + server" saw "no
// proxy" over a machine still pointing at a script this app wrote — the
// crash cleanup below skipped it and the PAC outlived the session.
//
// Read from the registry rather than through WinINet because the question is about the
// stored setting — the one that survives a crash — not about what a running browser
// currently holds.
func Current() (enabled bool, server, pac string, err error) {
	path, err := windows.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if err != nil {
		return false, "", "", err
	}
	var key windows.Handle
	if err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, path, 0, windows.KEY_READ, &key); err != nil {
		return false, "", "", err
	}
	defer func() { _ = windows.RegCloseKey(key) }()

	name, _ := windows.UTF16PtrFromString("ProxyEnable")
	var typ, size uint32 = 0, 4
	var on uint32
	if err := windows.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&on)), &size); err == nil {
		enabled = on != 0
	}
	// Absent values read as "not set" rather than as an error: a machine that
	// never had a server or a PAC configured answers every question here, and
	// the callers only act on what is actually present.
	server, _ = readRegString(key, "ProxyServer")
	pac, _ = readRegString(key, "AutoConfigURL")
	return enabled, server, pac, nil
}

// CurrentBypass reads the proxy bypass list (ProxyOverride) as stored. Empty
// when the value is absent — the machine has no override list of its own.
func CurrentBypass() (string, error) {
	path, err := windows.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if err != nil {
		return "", err
	}
	var key windows.Handle
	if err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, path, 0, windows.KEY_READ, &key); err != nil {
		return "", err
	}
	defer func() { _ = windows.RegCloseKey(key) }()
	return readRegString(key, "ProxyOverride")
}

// readRegString reads one REG_SZ value out of an open key. A missing value or a
// value of the wrong type is an empty answer, not an error: the callers above
// compare against strings and "absent" and "" mean the same to them.
func readRegString(key windows.Handle, name string) (string, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var typ, size uint32
	if err := windows.RegQueryValueEx(key, n, nil, &typ, nil, &size); err != nil {
		return "", err
	}
	if typ != windows.REG_SZ && typ != windows.REG_EXPAND_SZ {
		return "", fmt.Errorf("sysproxy: %s is not a string value", name)
	}
	buf := make([]uint16, size/2+1)
	if err := windows.RegQueryValueEx(key, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
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
	// The LAN write goes first and alone: if the default connection refused
	// the options, the connectoids are left alone rather than written behind a
	// failure. The old order wrote every RAS connection regardless and only
	// then returned the LAN error — so a Set that reported failure had still
	// put the app's proxy on the reader's VPN entries, and the caller's
	// rollback (flag off, no Clear) left exactly that behind: the UI saying
	// direct while the dial-up connection proxied.
	if err := applyToConnection(nil, opts); err != nil {
		return err
	}
	for _, name := range rasEntries() {
		conn, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		// Best-effort in this direction — one connectoid that refuses is not a
		// reason to fail a setting the default connection took.
		_ = applyToConnection(conn, opts)
	}
	return nil
}

// applyToAllConnectionsReleasing is the other direction: the same writes, but
// the connectoids are written even when the LAN connection refuses.
//
// The two directions are not symmetric and must not share one rule. On a SET,
// a LAN failure means nothing has been taken over yet, and writing the
// connectoids behind that failure would leave the reader's VPN entries proxied
// by a switch that reports "failed". On a CLEAR or a restore, the connectoids
// are very likely still carrying this app's proxy from a Set that succeeded
// earlier, and that Set wrote them too — skipping them here on a LAN error
// left exactly the state the undo record exists to prevent: the UI saying
// direct while the dial-up connection still went through the app.
func applyToAllConnectionsReleasing(opts []perConnOption) error {
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
