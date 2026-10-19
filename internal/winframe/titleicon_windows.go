//go:build windows

// Package winframe touches the window's own chrome — the parts of a Windows window that
// Wails does not expose as options.
package winframe

import (
	"os"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procCreateIcon               = user32.NewProc("CreateIcon")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmSetIcon = 0x0080
	// ICON_SMALL is the icon at the left of the title bar. ICON_BIG is deliberately left
	// alone: it is what the taskbar and Alt+Tab show, and the mark belongs there.
	iconSmall = 0
)

var (
	blankOnce sync.Once
	blankIcon uintptr
)

// blankTitleIcon builds a 16x16 icon with nothing in it.
//
// Setting ICON_SMALL to 0 does not remove the caption's icon. Windows resolves that
// icon in three steps — ICON_SMALL, then ICON_BIG, then the window class's own icon,
// which for a Wails window is the executable's resource — so a zero hands the decision
// to the next step and the same mark comes back, which is what it did here. An icon that
// is present but transparent is what actually stops anything being drawn: the first step
// answers, and its answer is nothing.
//
// A monochrome 16x16 icon: the AND mask all ones (no pixels drawn) and the XOR mask all
// zeros. CreateIcon copies both buffers, so the slices are only needed for the call.
func blankTitleIcon() uintptr {
	blankOnce.Do(func() {
		const stride = 16 * 16 / 8
		and := make([]byte, stride)
		for i := range and {
			and[i] = 0xFF
		}
		xor := make([]byte, stride)
		hinst, _, _ := procGetModuleHandleW.Call(0)
		h, _, _ := procCreateIcon.Call(hinst, 16, 16, 1, 1,
			uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
		blankIcon = h
	})
	return blankIcon
}

// ClearTitleBarIcon makes this process's main window draw no icon in its caption.
//
// The icon itself is not taken away: it is a resource of the executable (resource ID 3,
// put there by wails build), and the taskbar, Alt+Tab, Explorer and the installer keep
// reading it from there. This only answers the window's own question about its caption —
// the frame is the app's, and the app already shows its mark in the tray and the About
// panel.
//
// Sends nothing and returns quietly when no window is found, or when the blank icon
// cannot be created: a caption decoration is not worth a refusal to start.
func ClearTitleBarIcon() {
	icon := blankTitleIcon()
	if icon == 0 {
		return
	}
	pid := uint32(os.Getpid())
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var owner uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			return 1 // not ours: keep walking
		}
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, icon)
		return 1
	})
	procEnumWindows.Call(cb, 0)
}
