//go:build !windows

// Package webview2rt is the WebView2 runtime's check on Windows, and a plain
// "always there" everywhere else — the other platforms' engines (WKWebView,
// WebKitGTK) are system components their package managers own, the way
// WebView2 is supposed to be on Windows.
package webview2rt

// InstalledVersion is always "ok": there is no registry to read and no runtime
// this package could miss.
func InstalledVersion() string { return "ok" }

// OfferDownload never fires: the runtime cannot be missing.
func OfferDownload(title, text string) bool { return false }

// OpenDownloadPage does nothing: there is no page to open.
func OpenDownloadPage() {}
