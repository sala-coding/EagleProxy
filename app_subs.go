package main

import (
	"eagle/internal/configbuilder"
	"eagle/internal/db"
	"eagle/internal/sub"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PreviewConfig returns the generated sing-box config for a profile.
func (a *App) PreviewConfig(id int) (string, error) {
	if a.curMgr() == nil {
		return "", fmt.Errorf("backend not initialised")
	}
	ent := a.curMgr().SnapshotProfile(id)
	if ent == nil {
		return "", fmt.Errorf("profile %d not found", id)
	}
	res, err := configbuilder.BuildConfig(ent, a.curMgr(), a.curSetting(), false)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(res.Config, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// emitGroupsChanged tells the window that a group's members have been rewritten under
// it — by a subscription update, whether the reader pressed the button or the
// background timer did it.
//
// Nothing used to say so, and that silence is what the reader's screenshot was made of:
// a subscription was fetched while the window stood open, its nodes were replaced on
// disk, and the table went on showing the rows it had read minutes before — old ids,
// nodes that no longer existed. The next double click on one of those rows asked the
// backend to start an id that named nothing, and the answer was "profile N not found".
// The window answers this event by re-reading the tree and the list (see App.tsx),
// which is what a table of live ids needs.
// gid is the group the window should be showing (the hotkey switch uses the event that
// way). Pass -1 to leave the reader where they are: an update refreshes the list of the
// group it belongs to, and must not move the view to it — a background refresh of
// another subscription would otherwise yank the table out from under whoever was
// reading it.
func (a *App) emitGroupsChanged(gid int) {
	if a.ctx == nil {
		return
	}
	// gid < 0 means "keep the view where it is": the window re-reads the lists
	// it shows but leaves the reader's group and selection alone. Resolving -1
	// to CurrentGroup here used to yank the view back — the settings file lags
	// a just-made switch, so a background walk could answer with the group the
	// reader had already left.
	runtime.EventsEmit(a.ctx, "groups:changed", gid)
}

// UpdateSubscription refreshes the subscription of a group.
func (a *App) UpdateSubscription(gid int) (*sub.Result, error) {
	g := a.curMgr().SnapshotGroup(gid)
	if g == nil {
		return nil, fmt.Errorf("group %d not found", gid)
	}
	if g.URL == "" {
		return nil, fmt.Errorf("this group has no subscription url")
	}
	if g.Archive {
		// An archived group is deliberately out of the update rotation — sub.Update
		// answers (nil, nil) for one — and that answer travelled back as a success, so
		// the window said 订阅已更新 over a subscription nothing had touched. Saying why
		// is the whole of the difference.
		return nil, fmt.Errorf("该分组已归档，不参与订阅更新")
	}
	// One walk at a time — see the comment on subMu in app.go.
	a.subMu.Lock()
	defer a.subMu.Unlock()
	// The archive check above ran before the lock: a group archived in between
	// (settings window open in both places) would answer (nil, nil) from inside
	// and travel back as a success — the same silent no-op the pre-lock check
	// was written to avoid. Re-checked under the lock, the answer is honest.
	if g2 := a.curMgr().SnapshotGroup(gid); g2 != nil && g2.Archive {
		return nil, fmt.Errorf("该分组已归档，不参与订阅更新")
	}
	res, err := sub.Update(a.curMgr(), a.curSetting(), a.runningID(), g.URL, gid, a.logf, a.runningID)
	// Announced either way: a walk that fails halfway has still rewritten part
	// of the batch on disk (AddProfiles saves as it goes), and the window must
	// re-read the list it is showing.
	a.emitGroupsChanged(-1)
	return res, err
}

// UpdateAllSubscriptions refreshes every group that has a subscription URL.
func (a *App) UpdateAllSubscriptions() error {
	// serialised with the per-group loop for the same reason UpdateSubscription is.
	a.subMu.Lock()
	defer a.subMu.Unlock()
	errs := sub.UpdateAll(a.curMgr(), a.curSetting(), a.runningID(), a.logf, a.runningID)
	// Announced even when some groups failed: the ones that came through have new
	// ids on disk, and the window is showing the old ones.
	a.emitGroupsChanged(-1)
	if len(errs) > 0 {
		lines := make([]string, 0, len(errs))
		for _, e := range errs {
			lines = append(lines, e.Error())
		}
		return fmt.Errorf("%s", strings.Join(lines, "\n"))
	}
	return nil
}

// CreateSubscription creates a group from a subscription URL and imports it.
func (a *App) CreateSubscription(rawURL string) (int, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return -1, fmt.Errorf("empty url")
	}
	name := sub.HostLabel(rawURL)
	if name == "" {
		// Not a URL this can make a name from; the address itself is the next best
		// thing, and the panel may still name the subscription on the first update.
		name = rawURL
	}
	g := &db.Group{ID: -1, FrontProxyID: -1, Name: name, URL: rawURL}
	if err := a.curMgr().AddGroup(g); err != nil {
		return -1, err
	}
	a.subMu.Lock()
	defer a.subMu.Unlock()
	if _, err := sub.Update(a.curMgr(), a.curSetting(), a.runningID(), rawURL, g.ID, a.logf, a.runningID); err != nil {
		// Even a failed first import can have written part of the list (the parse
		// lands before the error is raised); the window is told either way.
		a.emitGroupsChanged(g.ID)
		return g.ID, err
	}
	a.emitGroupsChanged(g.ID)
	return g.ID, nil
}

// ExportConfigFile writes the generated sing-box config to a user-chosen file.
func (a *App) ExportConfigFile(id int) (string, error) {
	cfg, err := a.PreviewConfig(id)
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", fmt.Errorf("backend not initialised")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出 sing-box 配置",
		DefaultFilename: fmt.Sprintf("config-%d.json", id),
		Filters:         []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
