//go:build windows

// Package hotkey registers global hotkeys (Windows RegisterHotKey).
package hotkey

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procPeekMessageW     = user32.NewProc("PeekMessageW")

	mu      sync.Mutex
	bound   = map[int]func(){}
	nextID  = 1
	started bool
	cmdCh   = make(chan func(), 32)
)

const (
	modAlt     = 0x0001
	modControl = 0x0002
	modShift   = 0x0004
	modWin     = 0x0008
	wmHotkey   = 0x0312
	pmRemove   = 0x0001
)

type point struct{ X, Y int32 }

type msgStruct struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

func start() {
	mu.Lock()
	if started {
		mu.Unlock()
		return
	}
	started = true
	mu.Unlock()
	go loop()
}

func loop() {
	runtime.LockOSThread()
	// force creation of the thread message queue
	var m msgStruct
	_, _, _ = procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 0)

	for {
		for {
			var m2 msgStruct
			// Filter on WM_HOTKEY: an unfiltered PeekMessage(PM_REMOVE) drains
			// the whole queue of this thread and this loop only understands
			// hotkeys, so every other posted message (notably the systray
			// window's click notifications and menu WM_COMMANDs, which share
			// the thread) was removed and thrown away.
			r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m2)), 0, wmHotkey, wmHotkey, pmRemove)
			if r == 0 {
				break
			}
			mu.Lock()
			fn := bound[int(m2.wParam)]
			mu.Unlock()
			if fn != nil {
				go fn()
			}
		}
		select {
		case c := <-cmdCh:
			c()
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// Apply replaces all registered hotkeys, and returns the ones it could not have: the
// strings it could not parse, and the combinations Windows refused.
//
// Refusing is the common case rather than the rare one — another program holds the
// combination, or the shell owns it (Alt+Tab), or it is one of the few Windows reserves
// for itself. It used to be swallowed here, which left a key that did nothing and no
// sign of why: the reader's own words for that are "the key is not there".
func Apply(bindings map[string]func()) []string {
	start()
	done := make(chan struct{})
	var refused []string
	cmdCh <- func() {
		defer close(done)
		mu.Lock()
		for id := range bound {
			_, _, _ = procUnregisterHotKey.Call(0, uintptr(id))
		}
		bound = map[int]func(){}
		mu.Unlock()

		for hk, fn := range bindings {
			if strings.TrimSpace(hk) == "" || fn == nil {
				continue
			}
			mods, vk, err := Parse(hk)
			if err != nil {
				refused = append(refused, hk)
				continue
			}
			id := allocID()
			r, _, _ := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk))
			if r == 0 {
				// may already be registered by a previous run; retry once
				_, _, _ = procUnregisterHotKey.Call(0, uintptr(id))
				r, _, _ = procRegisterHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk))
				if r == 0 {
					refused = append(refused, hk)
					continue
				}
			}
			mu.Lock()
			bound[id] = fn
			mu.Unlock()
		}
	}
	<-done
	sort.Strings(refused)
	return refused
}

// Test reports whether a combination can be registered, by registering it and giving it
// straight back. It answers the one question about a combination that reading it cannot:
// whether something else already holds it.
//
// It is asked while a combination is being captured, which is also when this app's own
// hotkeys are out of the way (see SuspendHotkeys) — asked with them still registered, a
// combination of ours would come back as someone else's.
func Test(hk string) bool {
	mods, vk, err := Parse(hk)
	if err != nil {
		return false
	}
	start()
	done := make(chan struct{})
	ok := false
	cmdCh <- func() {
		defer close(done)
		id := allocID()
		r, _, _ := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk))
		if r == 0 {
			return
		}
		ok = true
		_, _, _ = procUnregisterHotKey.Call(0, uintptr(id))
	}
	<-done
	return ok
}

// Stop unregisters every hotkey.
func Stop() {
	start()
	done := make(chan struct{})
	cmdCh <- func() {
		defer close(done)
		mu.Lock()
		for id := range bound {
			_, _, _ = procUnregisterHotKey.Call(0, uintptr(id))
		}
		bound = map[int]func(){}
		mu.Unlock()
	}
	<-done
}

func allocID() int {
	mu.Lock()
	defer mu.Unlock()
	id := nextID
	nextID++
	return id
}

// Parse converts a hotkey string (e.g. "Ctrl+Shift+A") into modifiers + VK code.
func Parse(s string) (mods uint32, vk uint32, err error) {
	parts := strings.Split(s, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if i < len(parts)-1 {
			switch strings.ToLower(p) {
			case "ctrl", "control":
				mods |= modControl
			case "shift":
				mods |= modShift
			case "alt":
				mods |= modAlt
			case "meta", "win", "super", "cmd":
				mods |= modWin
			}
			continue
		}
		vk = keyCode(p)
	}
	if vk == 0 {
		return 0, 0, fmt.Errorf("unknown hotkey: %s", s)
	}
	return mods, vk, nil
}

func keyCode(k string) uint32 {
	up := strings.ToUpper(strings.TrimSpace(k))
	if len(up) == 1 {
		c := up[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint32(c)
		}
	}
	var n int
	if _, err := fmt.Sscanf(up, "F%d", &n); err == nil && n >= 1 && n <= 24 {
		return uint32(0x70 + n - 1)
	}
	switch up {
	case "SPACE":
		return 0x20
	case "TAB":
		return 0x09
	case "ENTER", "RETURN":
		return 0x0D
	case "ESC", "ESCAPE":
		return 0x1B
	case "HOME":
		return 0x24
	case "END":
		return 0x23
	case "LEFT":
		return 0x25
	case "UP":
		return 0x26
	case "RIGHT":
		return 0x27
	case "DOWN":
		return 0x28
	}
	return 0
}
