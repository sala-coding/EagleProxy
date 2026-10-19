//go:build windows

package main

import "golang.org/x/sys/windows"

// The two options refreshSystemNetworkState hands to InternetSetOption.
const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var (
	modwininet             = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOptionW = modwininet.NewProc("InternetSetOptionW")
)

// refreshSystemNetworkState tells the system the machine's connectivity picture
// changed and it should re-evaluate now.
//
// What this answers. The TUN adapter is born without routes, and NCSI's first
// probe of it goes out into that routeless gap and loses; the next attempt
// follows NCSI's passive backoff, which runs into minutes — the reader watched
// an adapter that was carrying traffic just fine sit on "无法连接到 Internet"
// for all of it. The route writes themselves wake NCSI again, but a wake that
// arrives without a resolver change can still probe against the stale DNS
// setting, so the honest answer is to say "the picture moved" explicitly and
// let the stack look once more, this time with the takeover's resolver in
// place (the caller runs reconcileSystemDNS(true) first).
//
// Best-effort, like every other poke at the system from here: wininet is part
// of every Windows build, a failed call costs nothing, and the passive backoff
// remains as the backstop it always was.
func refreshSystemNetworkState() {
	if err := procInternetSetOptionW.Find(); err != nil {
		return
	}
	// hwnd NULL: the option is machine-wide. Both options succeed by queueing
	// the re-evaluation rather than performing it, so there is no error worth
	// reporting and no return value worth reading.
	_, _, _ = procInternetSetOptionW.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = procInternetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
}
