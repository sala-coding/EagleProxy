//go:build windows

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The window's big icon, set by hand because Wails does not.
//
// Wails loads the executable's icon group (resource 3) and hands it to the
// window as ICON_SMALL only (see wails' desktop/windows/window.go:
// SetIcon(0, ico)). The taskbar button draws ICON_BIG or, failing that, the
// window class's icon — and the class registers none — so the button came up
// blank on this machine while the tray and Explorer showed the same eagle
// perfectly: the icon was in the file, it just never reached the shape the
// taskbar asks for.
//
// The remedy is one WM_SETICON: the same resource, loaded again at the size
// the system calls its own icon, set as ICON_BIG. Done once, on a goroutine
// that waits for the window to exist.
const (
	wmSetIcon    = 0x0080
	iconBig      = 1
	imageIcon    = 1
	lrShared     = 0x8000
	smCXIcon     = 11
	smCYIcon     = 12
	appIconID    = 3 // wails' winc.AppIconID — the group winres baked into the exe
	wailsClass   = "wailsWindow"
	findDeadline = 20 * time.Second

	// GCLP_HICON is -14; written as its two's-complement so it fits uintptr.
	gclpHIcon       = ^uintptr(13)
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
)

var (
	procEnumWindows     = windows.NewLazySystemDLL("user32.dll").NewProc("EnumWindows")
	procGetWindowPID    = windows.NewLazySystemDLL("user32.dll").NewProc("GetWindowThreadProcessId")
	procGetClassNameW   = windows.NewLazySystemDLL("user32.dll").NewProc("GetClassNameW")
	procSendMessageW    = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageW")
	procLoadImageW      = windows.NewLazySystemDLL("user32.dll").NewProc("LoadImageW")
	procGetSystemMetric = windows.NewLazySystemDLL("user32.dll").NewProc("GetSystemMetrics")
	procSetClassLongPtr = windows.NewLazySystemDLL("user32.dll").NewProc("SetClassLongPtrW")
	procSetWindowPos    = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowPos")
)

// applyBigWindowIcon stamps ICON_BIG onto this process's wails window, retrying
// while the window is still being created.
//
// The stamp is repeated a few times on purpose. The taskbar button is built
// when the window is first shown, and Windows asks for the icon at that moment
// — a single WM_SETICON that lands before the button exists can be captured
// while the class icon is still null, and the button then stays blank for the
// rest of the session (seen here on 2026-11-17: the window carried a valid
// ICON_BIG while its button drew nothing). Re-stamping after the button is up
// makes Explorer redraw it, and the class icon is set too, since the button
// falls back to it when the window has no ICON_BIG yet.
func applyBigWindowIcon() {
	deadline := time.Now().Add(findDeadline)
	var hwnd uintptr
	for time.Now().Before(deadline) && hwnd == 0 {
		hwnd = findWailsWindow()
		if hwnd == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if hwnd == 0 {
		return
	}
	for _, wait := range []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond} {
		time.Sleep(wait)
		if hIcon := loadAppIcon(); hIcon != 0 {
			procSetClassLongPtr.Call(hwnd, uintptr(gclpHIcon), hIcon)
			procSendMessageW.Call(hwnd, wmSetIcon, iconBig, hIcon)
			procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
				swpNoSize|swpNoMove|swpNoZOrder|swpNoActivate|swpFrameChanged)
		}
	}
}

// loadAppIcon is the icon group the exe carries, at the system's own icon size.
//
// The module handle is asked for rather than passed as NULL: LoadImage with a
// NULL instance loads a SYSTEM icon (the IDI_* table), not a resource of this
// process — the first version of this did exactly that and got nothing, since
// 3 is not a system icon id. The exe file is the fallback, for the case where
// the resource walk fails some other way.
func loadAppIcon() uintptr {
	cx, _, _ := procGetSystemMetric.Call(smCXIcon)
	cy, _, _ := procGetSystemMetric.Call(smCYIcon)
	var mod windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &mod); err == nil {
		// LR_SHARED: one icon for the process, released with it — the handle
		// stays in the window for the whole session, so nothing here has to
		// own its lifetime.
		if hIcon, _, _ := procLoadImageW.Call(uintptr(mod), appIconID, imageIcon, cx, cy, lrShared); hIcon != 0 {
			return hIcon
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	path, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0
	}
	const lrLoadFromFile = 0x0010
	hIcon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(path)), imageIcon, cx, cy, lrLoadFromFile)
	return hIcon
}

// findWailsWindow is the window this process owns, of the class Wails registers
// — the process id is what keeps it from latching onto another Eagle.
func findWailsWindow() uintptr {
	pid := uint32(os.Getpid())
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1
		}
		var buf [64]uint16
		n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 && windows.UTF16ToString(buf[:n]) == wailsClass {
			found = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}
