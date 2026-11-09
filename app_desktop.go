package main

import (
	"context"
	"os/exec"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/core"
	"eagle/internal/util"

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

// GetAbout reports the identity of this build: which release it was built from and
// the Go it was built with — the facts the About card and the bug-report body read.
// (The data and core paths the card used to carry are the settings fields' own
// business; see relToExec for the relative form those write.)
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
		// The platform the binary was compiled for, not the one it happens to be
		// running on: the two differ exactly when the answer matters (a 386 build
		// on an amd64 machine, the windows/arm64 package on a Snapdragon), and it
		// is the first thing a bug report is sorted by.
		"arch": goruntime.GOOS + "/" + goruntime.GOARCH,
		"repo": updateRepo,
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

// coreVersionWindow bounds the version probe: a core that answers takes tens of
// milliseconds, and one that does not is not worth a stalled card.
const coreVersionWindow = 2 * time.Second

// probeCoreVersion runs the version subcommand (see CoreVersion) — the shared
// implementation behind the About card's uncached ask and the config builder's
// cached one.
func probeCoreVersion(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), coreVersionWindow)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "version")
	// A console program started by a GUI one puts up a window of its own, and
	// this probe would flash one every time the card is opened.
	util.HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line = strings.TrimSpace(line)
	if i := strings.Index(strings.ToLower(line), "version "); i >= 0 {
		line = line[i+len("version "):]
	}
	if f := strings.Fields(line); len(f) > 0 {
		return f[0]
	}
	return ""
}

// refreshCoreVersion re-asks the core what it is and hands the answer to the
// config builder, which needs it on every build to decide whether the tun stack
// may be the core's own (sing-box 1.15+). Cached by the core it came from: a
// process spawn per build is a cost no start should pay for a number that
// changes only when the reader points the app at another core. An empty answer
// is not cached — the next build probes again, because the reader may have just
// fixed the path.
func (a *App) refreshCoreVersion() {
	exe, err := core.ResolveCore(resolveAppPath(a.curCorePathRaw()), a.curDataDir())
	if err != nil {
		return
	}
	if v := probeCoreVersion(exe); v != "" {
		configbuilder.SetCoreVersion(v)
	}
}

// CoreVersion asks the kernel binary what it is, for the About card's 内核 row.
//
// The core is an external executable (see GetCorePath), so its version is not
// something this build can know — the answer has to come from running it.
// `version` is the subcommand every sing-box-shaped core answers, and it prints
// one line ("sing-box version 1.9.0") above the environment block the card has no
// room for, so the first line is taken and the number is its last field.
//
// Empty means the card stays silent rather than guessing: no core on disk, a
// binary that does not answer, or one that overran the window. Deliberately not
// cached — the reader can point the app at another core between two openings.
func (a *App) CoreVersion() string {
	exe, err := core.ResolveCore(resolveAppPath(a.curCorePathRaw()), a.curDataDir())
	if err != nil {
		return ""
	}
	return probeCoreVersion(exe)
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
