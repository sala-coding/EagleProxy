//go:build !windows

package tray

// Callbacks receives tray menu actions. Mirror of the Windows build: see
// tray_windows.go for what each action answers to.
type Callbacks struct {
	OnShow        func()
	OnStart       func()
	OnStop        func()
	OnQuit        func()
	OnSystemProxy func()
	OnTun         func()
	OnNextProfile func()
	OnEgress      func(mode string)
	OnDataDir     func()
	OnUpdate      func()
}

// SetModes is a no-op on non-Windows platforms.
func SetModes(systemProxy, tun bool, egress string) {}

// SetCurrent is a no-op on non-Windows platforms.
func SetCurrent(name string) {}

// Setup is a no-op on non-Windows platforms.
func Setup(icon []byte, cb Callbacks) {}

// Quit is a no-op on non-Windows platforms.
func Quit() {}
