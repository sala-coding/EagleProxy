package main

import (
	"eagle/internal/clash"
	"eagle/internal/core"
	"eagle/internal/db"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// apiDownMsg reports a dead core for actions the user explicitly took. Merely
// listing connections is not one of them: that path stays quiet (see
// GetConnections), because the button is clickable with no core running.
const apiDownMsg = "未连接到核心（端口 %d 无响应），请先启动一个节点"

func (a *App) GetConnections() (*clash.ConnectionsResp, error) {
	if a.curSetting() == nil {
		return nil, fmt.Errorf("not initialised")
	}
	if a.curSetting().CoreBoxClashAPI <= 0 {
		return nil, fmt.Errorf("Clash API 未启用：请在设置里将 core_box_clash_api 设为正数（例如 19090）")
	}
	resp, err := clash.New(a.curSetting().CoreBoxClashAPI, a.curSetting().CoreBoxClashAPISecret).Connections()
	if err != nil {
		// No core listening is a normal state, not a failure: the connection
		// button is always clickable, and opening it before starting a node
		// should just show an empty list. The reason still goes to the log for
		// anyone debugging, but the dialog stays quiet.
		a.logf("[connections] Clash API query failed: " + err.Error())
		return &clash.ConnectionsResp{}, nil
	}
	return resp, nil
}

// CloseConnection closes a connection by id.
func (a *App) CloseConnection(id string) error {
	if a.curSetting() == nil {
		return fmt.Errorf("not initialised")
	}
	if a.curSetting().CoreBoxClashAPI <= 0 {
		return fmt.Errorf("Clash API 未启用")
	}
	if err := clash.New(a.curSetting().CoreBoxClashAPI, a.curSetting().CoreBoxClashAPISecret).CloseConnection(id); err != nil {
		a.logf("[connections] close failed: " + err.Error())
		// The reason goes with the answer: an id the core does not know and a
		// secret it rejects both used to read as "未连接到核心", and the reader
		// went checking a core that was answering all along.
		return fmt.Errorf("关闭连接失败: %v", err)
	}
	return nil
}

// ResetMetrics clears the stored latency, speed and traffic counters on every
// profile, so the list can be re-measured from scratch.
func (a *App) ResetMetrics() error {
	if a.curMgr() == nil {
		return fmt.Errorf("not initialised")
	}
	// The poll's own baselines are deliberately NOT touched here. trafficLoop derives
	// each profile's delta from the totals it last saw (a.lastUp / a.lastDown), so
	// leaving them as they are is what makes the next poll report only the bytes moved
	// since that poll. Zeroing them — which is what "rebaseline" used to do — made
	// that next delta the core's whole lifetime total, so the counter the reader had
	// just cleared on the running node jumped straight to it, with a matching spike in
	// the rate and one tick of nonsense in the status bar.
	return a.curMgr().ClearMetrics()
}

// trafficLoop polls the Clash API and emits "traffic" events. It also attributes
// the bytes moved since the previous poll to the running profile, which is what
// the list's traffic column shows.
func (a *App) trafficLoop() {
	saves := 0
	firstPoll := true
	var lastErrLog time.Time
	ticks := 0
	for {
		// one read per iteration: settings are swapped, never mutated in place.
		s := a.curSetting()
		interval := 1000
		if s != nil && s.TrafficLoopInterval > 0 {
			interval = s.TrafficLoopInterval
		}
		time.Sleep(time.Duration(interval) * time.Millisecond)
		// a.ctx never reads nil after startup, so quitting is the real exit
		// signal: shutdown marks it before anything slow, and the loop then stops
		// polling a dying core or emitting into a closing context. Checked before
		// the reconcile below — the exit path's releaseSystemDNS writes the empty
		// state, and a reconcile that ran after it would write a takeover record
		// back over the cleanup.
		if a.ctx == nil || a.quitting.Load() {
			return
		}
		// The machine's resolvers are reconciled here as well as at every start and stop
		// (see emitRunningState): this loop is the only thing in the app that is always
		// running, so it is where a takeover that did not happen — the server not up yet,
		// the state file written while it was between ticks — is noticed and made good. The
		// work is idempotent and reads live state, so most ticks it is one file read.
		if ticks++; ticks%2 == 0 {
			a.reconcileSystemDNS(false)
		}
		s = a.curSetting()
		if s == nil || s.CoreBoxClashAPI <= 0 {
			continue
		}
		// VPN/TUN mode: the core runs as an elevated process outside the
		// controller, but it exposes the same Clash API, so the poll works
		// unchanged — only the "is anything running" check differs.
		vpnPID, vpnProfile := a.vpnState()
		vpnMode := vpnPID > 0
		ctrl := a.curController()
		if !vpnMode && (ctrl == nil || ctrl.RunningID() == core.StoppedID) {
			continue
		}
		c := clash.New(s.CoreBoxClashAPI, s.CoreBoxClashAPISecret)
		if firstPoll {
			// Said once, so a speed display that never moves has a first place to
			// look: the poll running at all is the difference between "the loop is
			// stuck upstream" and "the API is not answering".
			firstPoll = false
			a.logf(fmt.Sprintf("[traffic] 开始轮询 Clash API (127.0.0.1:%d)", s.CoreBoxClashAPI))
		}
		resp, err := c.Connections()
		if err != nil {
			// Throttled: the ordinary no-node stretch would otherwise write the
			// same line every second. The reason still has to reach the log — a
			// silently dead poll is exactly what hid the frozen speed display.
			if time.Since(lastErrLog) > 30*time.Second {
				lastErrLog = time.Now()
				a.logf("[traffic] Clash API 查询失败: " + err.Error())
			}
			continue
		}

		// The core counts from zero when its process starts, so the delta since
		// the previous poll is exactly this interval's traffic. A negative delta
		// means a new core process replaced the old one: just re-baseline.
		// The baseline is guarded by a mutex rather than plain locals: ResetMetrics
		// clears it from another goroutine, otherwise the bytes moved since the
		// last poll would immediately be re-attributed to the zeroed counter.
		a.trafficMu.Lock()
		dUp, dDown := resp.UploadTotal-a.lastUp, resp.DownloadTotal-a.lastDown
		a.lastUp, a.lastDown = resp.UploadTotal, resp.DownloadTotal
		a.trafficMu.Unlock()

		// The status bar shows a rate, not the core's lifetime counter, so
		// scale the interval delta up to bytes per second. A negative delta
		// (fresh core) reads as no traffic rather than a huge negative rate.
		var upRate, downRate int64
		if interval > 0 && (dUp > 0 || dDown > 0) {
			upRate = dUp * 1000 / int64(interval)
			downRate = dDown * 1000 / int64(interval)
		}

		runtime.EventsEmit(a.ctx, "traffic", map[string]interface{}{
			"up":       resp.UploadTotal,
			"down":     resp.DownloadTotal,
			"upRate":   upRate,
			"downRate": downRate,
			"count":    len(resp.Connections),
		})

		if dUp < 0 || dDown < 0 || (dUp == 0 && dDown == 0) {
			continue
		}
		// Attribution: the main core carries every byte in both shapes — under TUN the
		// front-end holds only the adapter, and vpnProfile has never carried a node id
		// (it is StoppedID by construction), so the controller's own record is the one
		// to read. A warm-up with no node answers StoppedID and is skipped below, as
		// before.
		id := core.StoppedID
		if ctrl != nil {
			id = ctrl.RunningID()
		} else if vpnMode {
			id = vpnProfile
		}
		// The counter update (and any persisted copy of it) lands under the
		// manager's lock: the bean maps are what ListProfiles walks and what the
		// save marshals, and a Set running while either reads the map has been
		// known to take the process down with a concurrent-map panic.
		var up, down int64
		saves++
		doSave := saves >= 30
		if doSave {
			saves = 0
		}
		if a.curMgr().UpdateProfile(id, doSave, func(ent *db.ProxyEntity) {
			ent.Ensure()
			up = ent.Traffic.Int64("up") + dUp
			down = ent.Traffic.Int64("down") + dDown
			ent.Traffic.Set("up", up)
			ent.Traffic.Set("down", down)
		}) != nil {
			continue
		}
		runtime.EventsEmit(a.ctx, "profile:traffic", map[string]interface{}{
			"id": id, "up": up, "down": down,
		})
	}
}

// flushTraffic persists the counters of the profile that is about to stop, so
// the last interval is not lost.
func (a *App) flushTraffic() {
	if a.curMgr() == nil || a.curController() == nil {
		return
	}
	if ent := a.curMgr().GetProfile(a.curController().RunningID()); ent != nil {
		_ = a.curMgr().SaveProfile(ent)
	}
}
