package main

import (
	"eagle/internal/configbuilder"
	"eagle/internal/db"
	"eagle/internal/format"
	"eagle/internal/sub"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

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
// The window answers this event by re-reading the tree and the list (the
// groups:changed handler in App.vue), which is what a table of live ids needs.
// gid is the group the window should be showing (the hotkey switch uses the event that
// way). Pass -1 to leave the reader where they are: an update refreshes the list of the
// group it belongs to, and must not move the view to it — a background refresh of
// another subscription would otherwise yank the table out from under whoever was
// reading it.
func (a *App) emitGroupsChanged(gid int) {
	if a.curCtx() == nil {
		return
	}
	// gid < 0 means "keep the view where it is": the window re-reads the lists
	// it shows but leaves the reader's group and selection alone. Resolving -1
	// to CurrentGroup here used to yank the view back — the settings file lags
	// a just-made switch, so a background walk could answer with the group the
	// reader had already left.
	runtime.EventsEmit(a.curCtx(), "groups:changed", gid)
}

// UpdateSubscription refreshes the subscription of a group.
func (a *App) UpdateSubscription(gid int) (*sub.Result, error) {
	if a.curMgr() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
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

// ExportConfigFile writes the generated sing-box config to a user-chosen file.
func (a *App) ExportConfigFile(id int) (string, error) {
	cfg, err := a.PreviewConfig(id)
	if err != nil {
		return "", err
	}
	if a.curCtx() == nil {
		return "", fmt.Errorf("backend not initialised")
	}
	path, err := runtime.SaveFileDialog(a.curCtx(), runtime.SaveDialogOptions{
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

// ExportGroupConfig writes a whole subscription out in the format the reader
// picked — sing-box JSON, Clash YAML, a share-link list, or that list base64
// wrapped (the shape every panel reads as "订阅"). The builder puts the group's
// node list behind its selector, so the sing-box file carries every node the
// subscription holds rather than one of them — which node it is built from
// decides what the config starts on, not what it contains. The file is named
// after the subscription, since that is what it is; the extension follows the
// format.
//
// A format that cannot speak every node still writes what it can: the skipped
// ones are named in the window's console, because a file quietly missing nodes
// is a file the reader trusts more than it deserves.
func (a *App) ExportGroupConfig(gid int, kind string) (string, error) {
	mgr := a.curMgr()
	if mgr == nil {
		return "", fmt.Errorf("backend not initialised")
	}
	g := mgr.SnapshotGroup(gid)
	if g == nil {
		return "", fmt.Errorf("subscription %d not found", gid)
	}
	if len(g.Order) == 0 {
		return "", fmt.Errorf("subscription %q holds no nodes", g.Name)
	}
	if a.curCtx() == nil {
		return "", fmt.Errorf("backend not initialised")
	}

	content, ext, filter, err := a.exportGroupContent(g, kind)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.curCtx(), runtime.SaveDialogOptions{
		Title:           "导出订阅配置",
		DefaultFilename: fileNameOf(g.Name, gid) + ext,
		Filters:         []runtime.FileFilter{filter},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// exportGroupContent builds one subscription's bytes in the asked format. The
// extension and the dialog filter travel with it: the file offered is the one
// the format spells.
func (a *App) exportGroupContent(g *db.Group, kind string) ([]byte, string, runtime.FileFilter, error) {
	switch kind {
	case "singbox":
		cfg, err := a.PreviewConfig(g.Order[0])
		if err != nil {
			return nil, "", runtime.FileFilter{}, err
		}
		return []byte(cfg), ".json", runtime.FileFilter{DisplayName: "JSON", Pattern: "*.json"}, nil

	case "clash", "links", "base64":
		ents := make([]*db.ProxyEntity, 0, len(g.Order))
		for _, id := range g.Order {
			if e := a.curMgr().SnapshotProfile(id); e != nil {
				ents = append(ents, e)
			}
		}
		if kind == "clash" {
			doc, skipped := clashGroupDoc(g.Name, ents)
			if len(doc["proxies"].([]interface{})) == 0 {
				return nil, "", runtime.FileFilter{}, fmt.Errorf(
					"订阅里的 %d 个节点 Clash 都不认识：%s", len(skipped), strings.Join(skipped, "、"))
			}
			if len(skipped) > 0 {
				a.notifyMessage(fmt.Sprintf("Clash 导出跳过 %d 个节点：%s", len(skipped), strings.Join(skipped, "、")), false)
			}
			data, err := yaml.Marshal(doc)
			if err != nil {
				return nil, "", runtime.FileFilter{}, err
			}
			return data, ".yaml", runtime.FileFilter{DisplayName: "YAML", Pattern: "*.yaml;*.yml"}, nil
		}

		// The link list, and the same list base64 wrapped — the one shape every
		// panel and every client has agreed on since subscriptions began.
		var links, skipped []string
		for _, e := range ents {
			link := format.ToShareLink(e.Type, e.Bean, a.curSetting())
			if link == "" {
				skipped = append(skipped, e.Bean.Str("name")+" ("+e.Type+")")
				continue
			}
			links = append(links, link)
		}
		if len(links) == 0 {
			return nil, "", runtime.FileFilter{}, fmt.Errorf(
				"订阅里的 %d 个节点都写不成分享链接：%s", len(skipped), strings.Join(skipped, "、"))
		}
		if len(skipped) > 0 {
			a.notifyMessage(fmt.Sprintf("导出跳过 %d 个节点：%s", len(skipped), strings.Join(skipped, "、")), false)
		}
		joined := strings.Join(links, "\n")
		if kind == "base64" {
			return []byte(base64.StdEncoding.EncodeToString([]byte(joined))), ".b64",
				runtime.FileFilter{DisplayName: "Base64 订阅", Pattern: "*.b64;*.txt"}, nil
		}
		return []byte(joined), ".txt", runtime.FileFilter{DisplayName: "文本", Pattern: "*.txt"}, nil
	}
	return nil, "", runtime.FileFilter{}, fmt.Errorf("未知的导出格式 %q", kind)
}

// clashGroupDoc wraps the group's nodes into a minimal Clash configuration:
// the proxies, one select group named after the subscription, and a MATCH
// rule pointing at it — a file that carries the nodes and runs as-is, without
// pretending to be a rule set this app has its own opinion about.
func clashGroupDoc(name string, ents []*db.ProxyEntity) (map[string]interface{}, []string) {
	proxies := make([]interface{}, 0, len(ents))
	names := make([]string, 0, len(ents))
	var skipped []string
	for _, e := range ents {
		p := format.ToClashProxy(e.Type, e.Bean)
		if p == nil {
			skipped = append(skipped, e.Bean.Str("name")+" ("+e.Type+")")
			continue
		}
		proxies = append(proxies, p)
		names = append(names, e.Bean.Str("name"))
	}
	group := strings.TrimSpace(name)
	if group == "" {
		group = "PROXY"
	}
	doc := map[string]interface{}{
		"mixed-port": 7890,
		"mode":       "rule",
		"proxies":    proxies,
		"proxy-groups": []interface{}{
			map[string]interface{}{"name": group, "type": "select", "proxies": append(names, "DIRECT")},
		},
		"rules": []interface{}{"MATCH," + group},
	}
	return doc, skipped
}

// fileNameOf is a subscription's name as Windows will take it: the characters a file
// name cannot carry become underscores — dropped instead, two subscriptions would be
// offered the same file — and a name that is nothing but those falls back to its number.
// Cut to 48 characters, which leaves room for the extension and the folder.
func fileNameOf(name string, gid int) string {
	out := make([]rune, 0, len(name))
	for _, c := range strings.TrimSpace(name) {
		switch c {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			out = append(out, '_')
		default:
			if c >= 0x20 {
				out = append(out, c)
			}
		}
		if len(out) >= 48 {
			break
		}
	}
	s := strings.Trim(strings.TrimSpace(string(out)), ".")
	if s == "" {
		return fmt.Sprintf("subscription-%d", gid)
	}
	return s
}
