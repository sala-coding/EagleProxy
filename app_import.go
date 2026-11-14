package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"eagle/internal/db"
	"eagle/internal/sub"
)

// maxImportBytes caps one imported file. A config is kilobytes and a whole
// subscription base64 megabytes; anything past this is not a config someone
// meant to hand to the window, and reading it whole first would only make the
// failure slower.
const maxImportBytes = 8 << 20

// ImportConfigResult is what a file import hands back: the subscription card
// it landed under (a fresh one, named after the file) and the nodes it put
// there — so the window can select the card and show its rows.
type ImportConfigResult struct {
	GID int   `json:"gid"`
	Ids []int `json:"ids"`
}

// importFiltersFor aims the file picker at the format the chooser named. The
// pick only aims it — the bytes are still read by their own shape (see
// parseImportFile), so a file that is not what its suffix claims imports all
// the same, and every filter keeps an all-files escape hatch.
func importFiltersFor(kind string) []runtime.FileFilter {
	all := runtime.FileFilter{DisplayName: "所有文件", Pattern: "*.*"}
	switch kind {
	case "singbox":
		return []runtime.FileFilter{
			{DisplayName: "sing-box 配置 (JSON)", Pattern: "*.json"}, all,
		}
	case "clash":
		return []runtime.FileFilter{
			{DisplayName: "Clash 配置 (YAML)", Pattern: "*.yaml;*.yml"}, all,
		}
	case "links", "base64":
		return []runtime.FileFilter{
			{DisplayName: "分享链接 / Base64 订阅", Pattern: "*.txt;*.conf;*.b64"}, all,
		}
	}
	return []runtime.FileFilter{
		{DisplayName: "配置文件 (JSON / YAML / 文本)", Pattern: "*.json;*.yaml;*.yml;*.txt;*.conf"}, all,
	}
}

// ImportConfigFile opens the system file picker, imports the config file the
// reader picks, and gives it a subscription card of its own — a file behaves
// like a subscription: it arrives as a group named after the file, holding
// what the file carried, not as loose rows folded into whichever group the
// window was showing. The kind the chooser named aims the picker's filter;
// the shapes themselves arrive by the same door: a sing-box configuration
// (its outbounds are walked — the nodes the file runs, minus the groups the
// file wraps them in), a Clash configuration, and a plain list of share
// links or base64. The file is dispatched by its content rather than its
// suffix, so a mis-suffixed or extension-less file that is still one of the
// three imports all the same.
//
// A partial import is an answer, not a success to be silent about (the same
// rule ImportLinks reads by): the result comes back with the nodes that did
// not make it named, so the window can both show the new rows and say why.
func (a *App) ImportConfigFile(kind string) (*ImportConfigResult, error) {
	if a.curMgr() == nil || a.curSetting() == nil || a.curCtx() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	path, err := runtime.OpenFileDialog(a.curCtx(), runtime.OpenDialogOptions{
		Title:   "导入配置文件",
		Filters: importFiltersFor(kind),
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		// The reader closed the picker; not an error, just nothing done.
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("%s 是空文件", path)
	}
	if len(content) > maxImportBytes {
		return nil, fmt.Errorf("%s 超过 %d MB，不像一份配置文件", path, maxImportBytes>>20)
	}

	a.subMu.Lock()
	defer a.subMu.Unlock()

	// The card is made before the nodes are read, so a bad file answers an
	// error and nothing else — the empty husk is taken back out on the way
	// down, and a retry does not leave it behind.
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	g := &db.Group{ID: -1, FrontProxyID: -1, Name: name}
	if err := a.curMgr().AddGroup(g); err != nil {
		return nil, err
	}

	ents, skipped, err := a.parseImportFile(content, g.ID)
	if err != nil {
		_ = a.curMgr().DeleteGroup(g.ID)
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(ents) == 0 {
		_ = a.curMgr().DeleteGroup(g.ID)
		if len(skipped) > 0 {
			return nil, fmt.Errorf("%s 里的 %d 个节点本程序都不支持：%s",
				path, len(skipped), strings.Join(skipped, "、"))
		}
		return nil, fmt.Errorf("%s 里没有识别出任何节点", path)
	}
	if err := a.curMgr().AddProfiles(ents, g.ID); err != nil {
		_ = a.curMgr().DeleteGroup(g.ID)
		return nil, err
	}
	ids := make([]int, 0, len(ents))
	for _, e := range ents {
		ids = append(ids, e.ID)
	}
	a.emitGroupsChanged(g.ID)
	res := &ImportConfigResult{GID: g.ID, Ids: ids}
	if len(skipped) > 0 {
		return res, fmt.Errorf("已导入 %d 条，%d 条不支持：%s",
			len(ents), len(skipped), strings.Join(skipped, "、"))
	}
	return res, nil
}

// parseImportFile dispatches one file's bytes by shape. JSON is read as JSON:
// a sing-box configuration answers by its outbounds array, and a Clash config
// wearing JSON answers by its proxies array — that one is re-spelled as YAML,
// because the raw updater sniffs the "proxies:" key as text and JSON quotes
// its way past that sniff. Everything else goes to the raw updater, which
// sniffs base64 and link lists for itself.
func (a *App) parseImportFile(content []byte, gid int) ([]*db.ProxyEntity, []string, error) {
	trimmed := strings.TrimSpace(string(content))
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		var probe map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &probe); err == nil {
			if obs, ok := probe["outbounds"].([]interface{}); ok && len(obs) > 0 {
				ents, skipped, err := sub.ParseSingBoxConfig([]byte(trimmed), gid)
				return ents, skipped, err
			}
			if proxies, ok := probe["proxies"].([]interface{}); ok && len(proxies) > 0 {
				data, err := yaml.Marshal(probe)
				if err != nil {
					return nil, nil, err
				}
				ents, skipped := sub.ParseContent(data, a.curSetting(), gid)
				return ents, skipped, nil
			}
			return nil, nil, fmt.Errorf("无法识别的 JSON：既没有 outbounds 也没有 proxies")
		}
	}
	ents, skipped := sub.ParseContent(content, a.curSetting(), gid)
	return ents, skipped, nil
}
