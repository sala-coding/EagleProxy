//go:build !windows

// Package hotkey registers global hotkeys.
package hotkey

// Apply is a no-op on non-Windows platforms: there is nowhere to register a key, so
// every binding is reported as one that did not take — the caller says as much rather
// than leaving a key that quietly does nothing.
func Apply(bindings map[string]func()) []string {
	refused := make([]string, 0, len(bindings))
	for hk := range bindings {
		if hk != "" {
			refused = append(refused, hk)
		}
	}
	return refused
}

// Test always says yes off Windows: there is no register to fail, so there is no
// conflict to report. Apply is where a reader here learns that none of these keys will
// work — it reports every one of them as refused.
func Test(hk string) bool { return hk != "" }

// Stop is a no-op on non-Windows platforms.
func Stop() {}
