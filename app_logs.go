package main

import (
	"eagle/internal/logstore"
	"eagle/internal/util"
	"eagle/internal/winframe"
	"fmt"
	"os"
	"strings"
	"sync"
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

// The outbound log stream is batched. A busy core writes dozens of lines a
// second — every proxied connection is three lines — and each one used to wake
// the WebView with its own event, which is IPC churn the panel never needed:
// the front end drains its own queue every 120ms anyway, so a window of the
// same length on this side changes nothing the reader can see and replaces
// hundreds of wake-ups a second with a handful. One event now carries an array
// of {cat, line} pairs (see core:log:batch in App.vue).
//
// The batcher's own state is self-contained and touched only through its
// methods, so it needs no lock of the App's own guards.
type logEmitLine struct {
	Cat  string `json:"cat"`
	Line string `json:"line"`
}

const logFlushInterval = 120 * time.Millisecond

type logBatcher struct {
	mu    sync.Mutex
	queue []logEmitLine
	timer *time.Timer
}

// push queues one line and arms the flush timer if this is the window's first
// line. flush is called back outside the lock.
func (b *logBatcher) push(category, line string, flush func([]logEmitLine)) {
	b.mu.Lock()
	b.queue = append(b.queue, logEmitLine{Cat: category, Line: line})
	if b.timer == nil {
		b.timer = time.AfterFunc(logFlushInterval, func() {
			b.mu.Lock()
			batch := b.queue
			b.queue = nil
			b.timer = nil
			b.mu.Unlock()
			if len(batch) > 0 {
				flush(batch)
			}
		})
	}
	b.mu.Unlock()
}

// emitLog pushes a line to the UI log panel, tagged with its category so the
// panel can colour each category differently. The line joins the outbound
// batch (see logBatcher) rather than waking the WebView on its own.
func (a *App) emitLog(category, line string) {
	if a.curCtx() == nil {
		return
	}
	a.logBatch.push(category, line, a.sendLogBatch)
}

// sendLogBatch hands one drained batch to the front end. The context is read
// again at flush time: the window may have been re-created while lines sat in
// the queue, and a stale context would send them nowhere.
func (a *App) sendLogBatch(batch []logEmitLine) {
	ctx := a.curCtx()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, "core:log:batch", batch)
}

// notifyMessage tells the user something in the app's own words — which step of
// the "mode first, node second" flow is missing, for instance.
//
// The words live in the console now: the floating message window that used to
// carry them is gone, and every notice takes the road every line of this
// app's own making takes — the process console, the "core" log file and the
// panel's live stream (see logf), plus the buffer the panel re-reads when it
// opens (see Controller.Note), so a notice raised while nothing was listening
// — the TUN handover, a second launch — is not the one line the reader cannot
// find again. The "[app]" tag marks these apart from the core's own output.
func (a *App) notifyMessage(text string, isErr ...bool) {
	line := "[app] " + text
	a.logf(line)
	if a.curController() != nil {
		a.curController().Note(logstore.Stamp(line))
	}
}

// domReady runs what the window's first frame owes: the caption icon and the
// remembered-mode restore, both delayed so neither lands in the first frame.
func (a *App) domReady() {
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
