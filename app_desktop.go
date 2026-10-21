package main

import (
	"runtime/debug"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// prettyVersion writes a stamped version the way a reader reads it.
//
// A release's own number is left alone. A working copy's is "0.0.0-dev+9988017"
// — written that way because the update check has to be able to COMPARE it, and
// a number below every release is what a working copy is — but it reads as
// "dev-9988017" and says what it is.
func prettyVersion(v string) string {
	const dev = "0.0.0-dev"
	if !strings.HasPrefix(v, dev) {
		return v
	}
	if rest := strings.TrimPrefix(v, dev); rest != "" {
		return "dev-" + strings.TrimPrefix(rest, "+")
	}
	return "dev"
}

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
		"dataDir":     relToExec(a.curDataDir()),
		"corePath":    relToExec(a.curCorePath()),
		"dataDirAbs":  a.curDataDir(),
		"corePathAbs": a.curCorePath(),
		"repo":        updateRepo,
	}
	if appVersion != "" {
		info["version"] = prettyVersion(appVersion)
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
	if a.curCtx() != nil && url != "" {
		runtime.BrowserOpenURL(a.curCtx(), url)
	}
}

// ReadClipboard returns the system clipboard text.
func (a *App) ReadClipboard() string {
	if a.curCtx() == nil {
		return ""
	}
	text, _ := runtime.ClipboardGetText(a.curCtx())
	return text
}

// WriteClipboard sets the system clipboard text.
func (a *App) WriteClipboard(text string) {
	if a.curCtx() != nil {
		_ = runtime.ClipboardSetText(a.curCtx(), text)
	}
}
