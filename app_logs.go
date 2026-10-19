package main

import (
	"eagle/internal/core"
	"eagle/internal/crashlog"
	"eagle/internal/logstore"
	"eagle/internal/util"
	"eagle/internal/winframe"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetLogs returns recent core output.
func (a *App) GetLogs() []string {
	if a.curController() == nil {
		return []string{}
	}
	return a.curController().Logs()
}

// LogCategories lists the log categories that have recorded something today.
// The panel's switcher only offers what actually has data, so a category that
// was never used does not show up as an empty tab.
func (a *App) LogCategories() []string {
	if a.logs == nil {
		return []string{}
	}
	return a.logs.Categories()
}

// GetCategoryLogs returns the tail of one category's log file. The core log is
// served from memory by GetLogs, so this exists for the speed-test categories:
// they stream live as they run, but the file is the panel's source once a run
// ends, and it is read fresh every time (logstore's Read flushes first).
func (a *App) GetCategoryLogs(category string) []string {
	if a.logs == nil {
		return []string{}
	}
	return a.logs.Read(category, 2000)
}

// ClearLogs clears the core log buffer and the core log file of the day.
func (a *App) ClearLogs() {
	if a.curController() != nil {
		a.curController().ClearLogs()
	}
	if a.logs != nil {
		a.logs.Truncate("core")
	}
}

// ClearCategoryLogs discards a file-backed category's log file of the day.
func (a *App) ClearCategoryLogs(category string) {
	if a.logs == nil {
		return
	}
	a.logs.Truncate(category)
}

// emitLog pushes a line to the UI log panel, tagged with its category so the
// panel can colour each category differently.
func (a *App) emitLog(category, line string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "core:log", line, category)
	}
}

// Notice is one message for the frontend to show as a toast.
type Notice struct {
	Text  string `json:"text"`
	IsErr bool   `json:"isErr"`
}

// notifyMessage tells the user something in the app's own words — which step of
// the "mode first, node second" flow is missing, for instance. Messages raised
// before the webview is listening are queued rather than dropped: see the field
// comment on pendingNotice for the case that makes this necessary.
func (a *App) notifyMessage(text string, isErr ...bool) {
	n := Notice{Text: text, IsErr: len(isErr) > 0 && isErr[0]}
	a.noticeMu.Lock()
	if !a.noticeReady {
		a.pendingNotice = append(a.pendingNotice, n)
		a.noticeMu.Unlock()
		return
	}
	a.noticeMu.Unlock()
	a.emitNotice(n)
}

// ConsumeNotices hands over the messages raised before the frontend was
// listening, and marks it as listening from then on: anything raised after this
// call arrives as an app:message event instead.
//
// The UI calls it immediately after installing its listeners, and that ordering
// is what makes the handover lossless — a message cannot fall into the gap
// between "backend raised it" and "frontend could hear it", whichever side of
// OnDomReady that gap happens to land on. Returns nil when there is nothing.
func (a *App) ConsumeNotices() []Notice {
	a.noticeMu.Lock()
	defer a.noticeMu.Unlock()
	a.noticeReady = true
	queued := a.pendingNotice
	a.pendingNotice = nil
	return queued
}

// domReady marks the frontend as listening and hands it whatever was queued
// while it was starting up.
func (a *App) domReady() {
	a.noticeMu.Lock()
	a.noticeReady = true
	queued := a.pendingNotice
	a.pendingNotice = nil
	a.noticeMu.Unlock()
	for _, n := range queued {
		a.emitNotice(n)
	}
	// The window's caption icon, cleared now that the window is on screen — it does not
	// exist yet when startup runs, which is where this used to be and why nothing
	// happened. Twice: the icon is a resource of the executable, and a window that is
	// handed its class icon as it is shown would put it back, so the second pass lands a
	// moment after that. See winframe.ClearTitleBarIcon.
	winframe.ClearTitleBarIcon()
	time.AfterFunc(700*time.Millisecond, winframe.ClearTitleBarIcon)
	// The mode the last session was left in, put back if that was asked for. Delayed for
	// the same reason and by a little more: switching a mode can restart a node or raise
	// an administrator prompt, and neither belongs in the first frame of the window.
	time.AfterFunc(time.Second, a.applyRememberedSpmode)
	time.AfterFunc(1500*time.Millisecond, func() {
		if a.curSetting() != nil && a.curSetting().VPNMode() && a.runningID() == core.StoppedID {
			go crashlog.Guard("warmTunFront", a.warmTunFront)
		}
	})
}

func (a *App) emitNotice(n Notice) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "app:message", n.Text, n.IsErr)
	}
}

// connNoiseFragments mark the per-connection request/response lines the core
// emits for every proxied connection (one inbound/outbound/dns line each).
// They are shown in the UI panel but kept out of the "core" log file, which
// only carries core lifecycle/status lines and errors — otherwise the file
// fills with noise nobody searches for afterwards.
var connNoiseFragments = []string{
	"inbound connection from",
	"inbound connection to",
	"outbound connection",
	"connection closed",
	"dns: ",
}

func isConnNoise(line string) bool {
	for _, s := range connNoiseFragments {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

// logf records a core line: to the UI panel, the "core" log file and the
// console. The console copy is what makes a failure visible while debugging —
// the UI panel and the log file both stay empty when the call never happens.
// Connection request/response noise is panel-only; see connNoiseFragments.
func (a *App) logf(line string) {
	line = util.StripANSI(line)
	_, _ = fmt.Fprintln(os.Stderr, line)
	// One timestamp per line, whichever way it arrived: the core's own lines come
	// in stamped (the controller stamps what it buffers, and that buffer is what
	// the panel reloads from), while the lines this app produces come in raw. The
	// store accepts both and writes what it is given, so the file, the buffer and
	// the live stream all read the same text for the same line.
	if !logstore.HasStamp(line) {
		line = logstore.Stamp(line)
	}
	if a.logs != nil && !isConnNoise(line) {
		a.logs.Write("core", line)
	}
	a.emitLog("core", line)
}
