//go:build windows

// Package webview2rt answers one question before the window is asked for: is
// the machine's WebView2 runtime there? The window draws in WebView2, the
// runtime Windows is supposed to ship; a machine without it was a double-click
// that did nothing — wails.Run answered "runtime not found" to a window that
// did not exist to show it. This package reads the runtime's version from the
// registry, and an empty answer is the caller's cue to guide the reader to
// Microsoft's official download page.
package webview2rt

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// edgeUpdateClientID is the GUID Windows files the WebView2 runtime's version
// under, wherever it was installed from (Evergreen, embedded, or a Visual
// Studio bundle). "pv" carries the version string; an absent or empty "pv"
// means the runtime is not there.
const edgeUpdateClientID = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// registryPaths are the places a runtime's version may be written, in the order
// they are consulted: the machine-wide 64-bit view, the machine-wide view
// behind WOW64 redirection (where the Evergreen installer writes on a 64-bit
// Windows), and the per-user install. The first "pv" that says something wins.
var registryPaths = []struct {
	root registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + edgeUpdateClientID},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + edgeUpdateClientID},
	{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + edgeUpdateClientID},
}

// InstalledVersion returns the WebView2 runtime's version string, or "" when
// the machine has none. A silent read: every miss is a plain "key not found".
func InstalledVersion() string {
	for _, k := range registryPaths {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		pv, _, err := key.GetStringValue("pv")
		_ = key.Close()
		if err == nil && strings.TrimSpace(pv) != "" {
			return strings.TrimSpace(pv)
		}
	}
	return ""
}
