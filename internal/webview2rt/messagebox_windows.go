//go:build windows

package webview2rt

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The failure path that needs this runs before there is a window — and this is
// a GUI-subsystem binary, so there is no console either: without a box, "the
// runtime is missing" was a line in a log nobody had opened, behind a
// double-click that did nothing at all.
var user32 = windows.NewLazySystemDLL("user32.dll")
var procMessageBoxW = user32.NewProc("MessageBoxW")

const (
	mbYESNO         = 0x00000004
	mbIconWarning   = 0x00000030
	mbSetForeground = 0x00010000
	mbTopmost       = 0x00040000
	idYES           = 6
)

// OfferDownload shows the "runtime missing" box with a Yes/No answer and
// reports whether the reader chose to be taken to the official download page.
// It blocks until answered — the caller has nothing to do in the meantime but
// exit.
func OfferDownload(title, text string) bool {
	tp, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return false
	}
	cp, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	r, _, _ := procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(tp)),
		uintptr(unsafe.Pointer(cp)),
		mbYESNO|mbIconWarning|mbSetForeground|mbTopmost)
	return r == idYES
}

// OpenDownloadPage opens Microsoft's WebView2 download page in the default
// browser: the bootstrapper the reader picks there always carries the newest
// runtime, which is the whole of the upgrade story this side needs.
func OpenDownloadPage() {
	url, err := windows.UTF16PtrFromString("https://developer.microsoft.com/microsoft-edge/webview2/")
	if err != nil {
		return
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return
	}
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	shell32.NewProc("ShellExecuteW").Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(url)),
		0, 0, 1 /* SW_SHOWNORMAL */)
}
