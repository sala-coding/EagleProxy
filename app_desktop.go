package main

import (
	"runtime/debug"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetAbout reports the identity of this build: which release it was built from, the
// Go it was built with, and where it keeps its data and its core. The two paths are
// reported relative to the executable — the short form the settings field asks for, and
// the one that still reads the same after the folder is moved — with the absolute
// paths beside them for the tooltip (see relToExec).
//
// A map rather than a struct: the front end lays these out as label/value rows and
// nothing else reads them, so a struct would be a second declaration of the same fields
// in models.ts. The version is the release number the build stamped (appVersion), the
// revision the toolchain recorded when it was not, and — for a dev build, which has
// neither — the word that says so.
func (a *App) GetAbout() map[string]string {
	info := map[string]string{
		"version":   "",
		"built":     "",
		"goVersion": "",
		// Relative, because that is the form the reader wrote the two paths in and the
		// one that still means something after the folder moves (see relToExec). The
		// full paths ride along for the tooltip, which is where a path gets copied out
		// of this card.
		"dataDir":     relToExec(a.dataDir),
		"corePath":    relToExec(a.corePath),
		"dataDirAbs":  a.dataDir,
		"corePathAbs": a.corePath,
		"repo":        updateRepo,
	}
	if appVersion != "" {
		info["version"] = appVersion
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info["goVersion"] = bi.GoVersion
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info["version"] == "" {
					v := s.Value
					if len(v) > 8 {
						v = v[:8]
					}
					info["version"] = v
				}
			case "vcs.time":
				info["built"] = s.Value
			}
		}
	}
	// The build date the build stamped, for the builds the toolchain's own stamp
	// never reaches: wails builds with -buildvcs=false, so a packaged binary
	// carries no vcs.time at all (see appBuilt).
	if info["built"] == "" && appBuilt != "" {
		info["built"] = appBuilt
	}
	if info["version"] == "" && devIDSuffix() != "" {
		info["version"] = "开发版"
	}
	return info
}

// OpenURL opens a URL in the default browser.
func (a *App) OpenURL(url string) {
	if a.ctx != nil && url != "" {
		runtime.BrowserOpenURL(a.ctx, url)
	}
}

// ReadClipboard returns the system clipboard text.
func (a *App) ReadClipboard() string {
	if a.ctx == nil {
		return ""
	}
	text, _ := runtime.ClipboardGetText(a.ctx)
	return text
}

// WriteClipboard sets the system clipboard text.
func (a *App) WriteClipboard(text string) {
	if a.ctx != nil {
		_ = runtime.ClipboardSetText(a.ctx, text)
	}
}
