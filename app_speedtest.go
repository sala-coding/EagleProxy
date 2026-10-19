package main

import (
	"context"
	"eagle/internal/db"
	"eagle/internal/logstore"
	"eagle/internal/speedtest"
	"eagle/internal/util"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// speedtestCategory names the log file a test mode writes to, so the three
// modes never mix their output: text-logs/<category>-YYYY-MM-DD.log.
func speedtestCategory(mode int) string {
	switch mode {
	case speedtest.ModeTCPPing:
		return "speedtest-tcp"
	case speedtest.ModeFullTest:
		return "speedtest-full"
	case speedtest.ModeSpeedTest:
		return "speedtest-download"
	default:
		return "speedtest-url"
	}
}

// logSpeedtest records a speed test line: to the UI panel and to the file of
// that test mode.
func (a *App) logSpeedtest(category, line string) {
	// Stamped before it goes anywhere: this line reaches the panel as an event and
	// the file as a write, and the two have to agree — a test's output used to show
	// up untimed while it ran and timed once the panel was reopened.
	line = logstore.Stamp(util.StripANSI(line))
	if a.logs != nil {
		a.logs.Write(category, line)
	}
	a.emitLog(category, line)
}

// SpeedTestResult is the outcome for a single profile. Mode tells the frontend
// which of Latency / Speed this run measured, so a live update only touches the
// column that the test produced.
type SpeedTestResult struct {
	ID      int     `json:"id"`
	Mode    int     `json:"mode"`
	Latency int     `json:"latency"`
	Speed   float64 `json:"speed"`
	Error   string  `json:"error"`
	Report  string  `json:"report"`
}

// SpeedtestProfiles tests the given profiles with the chosen mode.
func (a *App) SpeedtestProfiles(ids []int, mode int) ([]SpeedTestResult, error) {
	return a.speedtest(ids, mode, "")
}

// SpeedtestGroup tests all profiles of a group.
func (a *App) SpeedtestGroup(gid int, mode int) ([]SpeedTestResult, error) {
	var ids []int
	for _, p := range a.curMgr().ProfilesWithOrder(gid) {
		ids = append(ids, p.ID)
	}
	title := "单个分组"
	if g := a.curMgr().SnapshotGroup(gid); g != nil && g.Name != "" {
		title = "分组 " + g.Name
	}
	return a.speedtest(ids, mode, title)
}

// CancelSpeedtest ends the run in progress, if there is one.
//
// The nodes already measured keep their numbers: a test stopped halfway is a test
// that answered some of the questions, which is what the reader was watching for.
// What the cancel reaches is the probes themselves — the runner carries this
// context into its requests, so a download of tens of megabytes stops where it
// stands instead of running to its own timeout first (a full test used to take up
// to about three minutes even after the stop was pressed). Nothing happens when no
// run is going.
func (a *App) CancelSpeedtest() {
	a.speedtestMu.Lock()
	cancel := a.speedtestCancel
	a.speedtestMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// speedtestJoinWait bounds how long a new run waits for the previous one to drain.
// With the context reaching the probes the wait is a second or so in practice; the
// bound is there for a worker that never comes back, which must not hold the
// reader's next run forever (the log guard at the end of speedtest covers what a
// timed-out predecessor would otherwise write).
const speedtestJoinWait = 15 * time.Second

// takeOverSpeedtest ends the run in progress and waits for it to drain, then
// registers this run as the one the stop button reaches. It returns the channel
// the caller closes when its own tail is done.
//
// Cancelling is not the same as being rid of: a cancelled run still has workers in
// flight, still writes each measured node's numbers, and still appends its own
// report to the log file this run is about to truncate — which is how one run's
// results ended up under the next run's header, and why two runs at once doubled
// the concurrency the reader had asked for.
func (a *App) takeOverSpeedtest(ctx context.Context, cancel context.CancelFunc) chan struct{} {
	a.speedtestMu.Lock()
	prevCancel := a.speedtestCancel
	prevDone := a.speedtestDone
	a.speedtestMu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}
	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(speedtestJoinWait):
			a.logf("[speedtest] 上一轮测速未在 15 秒内收尾，本轮照常开始")
		}
	}
	done := make(chan struct{})
	a.speedtestMu.Lock()
	a.speedtestCtx = ctx
	a.speedtestCancel = cancel
	a.speedtestDone = done
	a.speedtestMu.Unlock()
	return done
}

// speedtestModeName renders a test mode for logs.
func speedtestModeName(mode int) string {
	switch mode {
	case speedtest.ModeTCPPing:
		return "TCP Ping"
	case speedtest.ModeFullTest:
		return "完整测试"
	case speedtest.ModeSpeedTest:
		return "速度测试"
	default:
		return "延迟测试"
	}
}

// speedtestResultLine renders one result for the log file.
func (a *App) speedtestResultLine(r SpeedTestResult) string {
	label := fmt.Sprintf("#%d", r.ID)
	if ent := a.curMgr().GetProfile(r.ID); ent != nil {
		label = fmt.Sprintf("#%d %s %s", ent.ID, ent.DisplayName(), ent.DisplayAddress())
	}
	// The speed test reports a rate, the other modes report a latency.
	value := fmt.Sprintf("%.2f MB/s", r.Speed)
	if r.Speed <= 0 {
		value = fmt.Sprintf("%d ms", r.Latency)
	}
	switch {
	case r.Error != "":
		return fmt.Sprintf("%s → 失败: %s", label, r.Error)
	case r.Report != "":
		return fmt.Sprintf("%s → %s | %s", label, value, strings.ReplaceAll(r.Report, "\n", " ; "))
	default:
		return fmt.Sprintf("%s → %s", label, value)
	}
}

// speedtest runs the tests and mirrors everything shown in the UI log panel to
// text-logs/speedtest-<mode>-YYYY-MM-DD.log.
func (a *App) speedtest(ids []int, mode int, title string) ([]SpeedTestResult, error) {
	if a.curMgr() == nil || a.curSetting() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	category := speedtestCategory(mode)

	// One run at a time, and one way to end it: the cancel is what the status bar's
	// stop button reaches (see CancelSpeedtest), and the context travels on into the
	// runner so it reaches the probes rather than only the loop handing work out.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := a.takeOverSpeedtest(ctx, cancel)
	defer func() {
		// Closed last of all, once this run's report is written: the next run waits
		// on it before truncating the file (see takeOverSpeedtest).
		close(done)
		a.speedtestMu.Lock()
		// A newer run may have already replaced this run's registration; only the
		// run still registered in the field may clear it, otherwise an old
		// run's cleanup would leave the new one un-cancellable.
		if a.speedtestCtx == ctx {
			a.speedtestCtx = nil
			a.speedtestCancel = nil
			a.speedtestDone = nil
		}
		a.speedtestMu.Unlock()
	}()

	// The store this run started against. The reader can move the data directory while a
	// group test runs — minutes — and the commit below goes through a.curMgr(), which by
	// then is the new store: an id that named one node here names another there, and the
	// measurement would be written onto a node nobody tested. Read at each commit for the
	// same reason.
	startDir := a.dataDir
	runner := speedtest.New(a.curMgr(), a.curSetting(), a.corePath, a.dataDir)
	runner.SetContext(ctx)
	runner.SetLogger(func(line string) { a.logSpeedtest(category, line) })

	if a.logs != nil {
		if title == "" {
			title = "单个节点"
		}
		// Each run replaces the file instead of appending to it, so the log
		// always describes the latest test of that mode.
		a.logs.Truncate(category)
		a.logs.Section(category, fmt.Sprintf("%s · %s · %d 个节点", title, speedtestModeName(mode), len(ids)))
		a.emitLog("core", logstore.Stamp(">>> 测速日志: "+a.logs.Path(category, time.Now())))
	}

	n := a.curSetting().TestConcurrent
	if n <= 0 {
		n = 1
	}

	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []SpeedTestResult{}
loop:
	for _, id := range ids {
		// A snapshot: the entity is walked in this goroutine and in the pool the
		// runner hands it to, while the sibling update below writes the live one
		// under the manager's lock — reading the live map here raced that write.
		ent := a.curMgr().SnapshotProfile(id)
		if ent == nil {
			continue
		}
		// Stopped means stopped: nothing new is handed to a worker once the run has
		// been cancelled, and the loop leaves with whatever has been measured so
		// far. The node a worker is in the middle of is not interrupted — one probe
		// is seconds, and cutting it off would throw away the answer it was about to
		// give.
		select {
		case <-ctx.Done():
			break loop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(ent *db.ProxyEntity) {
			defer wg.Done()
			defer func() { <-sem }()
			r := runSpeedTest(runner, ent, mode)
			if ctx.Err() != nil {
				// Stopped while this probe was in flight: the node was never measured,
				// and -1 in the column is what "measured, and it failed" means — a stop
				// that stamped every node still in flight with it would turn the reader's
				// own click into a wall of failures. Nothing is written, nothing is
				// announced: the work is simply not done.
				return
			}
			if a.dataDir != startDir {
				// The store moved under the run (see startDir).
				return
			}
			mu.Lock()
			// The metric write is funnelled through the manager's locked update
			// so it cannot interleave with a reader walking the bean maps, and
			// the saved copy is consistent with it.
			a.curMgr().UpdateProfile(ent.ID, true, func(e *db.ProxyEntity) {
				// Each column keeps the newest measurement of its own metric: a
				// speed test never wipes the latency and vice versa. -1 marks a
				// failed measurement of that metric, 0 means "never measured".
				switch mode {
				case speedtest.ModeSpeedTest:
					e.Speed = r.Speed
				case speedtest.ModeFullTest:
					e.Latency, e.Speed = r.Latency, r.Speed
				default:
					e.Latency = r.Latency
				}
				if r.Report != "" {
					e.Report = r.Report
				} else if r.Error != "" {
					e.Report = r.Error
				}
			})
			results = append(results, r)
			mu.Unlock()
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "speedtest:result", r)
			}
		}(ent)
	}
	wg.Wait()

	if a.logs != nil {
		// Only the run that is still the registered one writes its report. A
		// superseded run can reach here after the newer one has already truncated the
		// file — the join above is bounded on purpose, so this is the case it leaves
		// open — and its lines would land under the new run's header.
		a.speedtestMu.Lock()
		current := a.speedtestCtx == ctx
		a.speedtestMu.Unlock()
		if current {
			for _, r := range results {
				a.logs.Write(category, a.speedtestResultLine(r))
			}
			a.logs.Section(category, speedtestModeName(mode)+" 结束")
		}
	}
	return results, nil
}

func runSpeedTest(r *speedtest.Runner, ent *db.ProxyEntity, mode int) SpeedTestResult {
	res := SpeedTestResult{ID: ent.ID, Mode: mode}
	switch mode {
	case speedtest.ModeTCPPing:
		ms, err := r.RunTCPPing(ent)
		if err != nil {
			res.Error = err.Error()
			res.Latency = -1
		} else {
			res.Latency = ms
		}
	case speedtest.ModeFullTest:
		ms, speed, report, err := r.RunFullTest(ent)
		res.Latency = ms
		res.Speed = speed
		res.Report = report
		if err != nil {
			res.Error = err.Error()
			res.Latency, res.Speed = -1, -1
		}
	case speedtest.ModeSpeedTest:
		speed, report, err := r.RunSpeedTest(ent)
		res.Speed = speed
		res.Report = report
		if err != nil {
			res.Error = err.Error()
			res.Speed = -1
		}
	default:
		ms, err := r.RunURLTest(ent)
		if err != nil {
			res.Error = err.Error()
			res.Latency = -1
		} else {
			res.Latency = ms
		}
	}
	return res
}
