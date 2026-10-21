package main

import (
	"eagle/internal/configbuilder"
	"eagle/internal/core"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RuleSetInfo is one rule set, as the settings page shows it: what it is called, where it
// would come from, and whether a usable copy is already on disk.
type RuleSetInfo struct {
	Tag string `json:"tag"`
	URL string `json:"url"`
	// Needed is whether the active routing profile refers to it. A file that is not
	// needed is not wrong — it is what a changed profile leaves behind — and saying so is
	// what lets a reader decide whether to keep it.
	Needed   bool   `json:"needed"`
	Local    bool   `json:"local"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	// Country is what the reader said a rule set they imported themselves is for. It is
	// empty for every rule set the profile refers to by name, whose country the tag
	// already carries (geoip-cn, geosite-cn); a hand-imported file has no such suffix to
	// read, and without this the fold's name could not say what its rules bite in.
	Country string `json:"country"`
}

// RuleSets lists every rule set this app works with: the tags the active routing profile
// refers to, and every .srs already in the settings' rule-set directory.
//
// The two together are the point. A tag the profile needs and the disk does not have will
// be fetched by the core itself — from raw.githubusercontent.com, which is unreachable on
// many networks — and a rule set that cannot be downloaded is not an error the core
// reports: it is a rule that quietly never matches. This list is the only place the window
// can show that, and the only place a file can be put there without a file manager.
func (a *App) RuleSets() ([]RuleSetInfo, error) {
	if a.curSetting() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	out := []RuleSetInfo{}
	seen := map[string]bool{}
	countries := a.loadRuleSetCountries()
	for tag, url := range configbuilder.RuleSets(a.curSetting()) {
		info := RuleSetInfo{Tag: tag, URL: url, Needed: true, Country: countries[tag]}
		if fi, ok := ruleSetFile(a.curSetting().RuleSetDir(), tag); ok {
			info.Local, info.Size = true, fi.Size()
			info.Modified = fi.ModTime().Format(time.RFC3339)
		}
		out = append(out, info)
		seen[tag] = true
	}

	// The app's own presets, listed even when no profile refers to them. The rule
	// sets the presets are written against are the set a reader can always fetch and
	// keep fresh, and fetching them is what the card is for — they are not part of the
	// routing until a rule names them, but the file on disk is what makes the rule
	// work the moment it does. One row each, the same shape the profile-referred rows
	// carry, so a preset that a profile later names costs the list nothing.
	for _, tag := range configbuilder.PresetRuleSetTags() {
		if seen[tag] {
			continue
		}
		sources := configbuilder.RuleSetSources(tag)
		if len(sources) == 0 {
			continue
		}
		info := RuleSetInfo{Tag: tag, URL: sources[0], Needed: true, Country: countries[tag]}
		if fi, ok := ruleSetFile(a.curSetting().RuleSetDir(), tag); ok {
			info.Local, info.Size = true, fi.Size()
			info.Modified = fi.ModTime().Format(time.RFC3339)
		}
		out = append(out, info)
		seen[tag] = true
	}

	// Files nothing refers to: listed and marked as such rather than hidden. Deleting
	// them is a decision for the reader, not one this list makes on their behalf.
	entries, err := os.ReadDir(a.curSetting().RuleSetDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".srs") {
			continue
		}
		tag := strings.TrimSuffix(name, ".srs")
		if seen[tag] {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, RuleSetInfo{
			Tag:      tag,
			Local:    true,
			Size:     fi.Size(),
			Modified: fi.ModTime().Format(time.RFC3339),
			Country:  countries[tag],
		})
	}

	// Needed first, then by name: what matters comes before what is left over.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Needed != out[j].Needed {
			return out[i].Needed
		}
		return out[i].Tag < out[j].Tag
	})
	return out, nil
}

// ruleSetFile stats a tag's local copy, reporting it only when it is there and usable —
// the same judgement localRuleSet makes before handing a file to the core.
func ruleSetFile(dir, tag string) (os.FileInfo, bool) {
	path := filepath.Join(dir, tag+".srs")
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || !configbuilder.LooksLikeRuleSet(path) {
		return nil, false
	}
	return fi, true
}

// ruleSetsChanged puts a running core back on the files it reads once, at start-up.
//
// It is what ApplyRouting already does for the profile itself, and the two cards edit
// the same profile — so without it, "下载" answered 已下载 while the routing went on
// with whatever rule set was there before, and the reader was left comparing two cards
// that behaved differently for no reason they could see. sing-box reads a local rule set
// when it starts; a file arriving is not the rules changing.
func (a *App) ruleSetsChanged() {
	if a.RunningProfile() == core.StoppedID {
		return
	}
	if err := a.RestartProfile(); err != nil {
		a.notifyMessage("规则集已变更，但核心未能重建: "+err.Error(), true)
		return
	}
	a.notifyMessage("规则集已生效，核心已按新的规则重建")
}

// DownloadRuleSet fetches one tag's rule set into the rule-set directory.
func (a *App) DownloadRuleSet(tag string) error {
	if a.curSetting() == nil {
		return fmt.Errorf("backend not initialised")
	}
	if err := a.fetchRuleSet(tag); err != nil {
		return err
	}
	a.ruleSetsChanged()
	return nil
}

// DownloadMissingRuleSets fetches every rule set the active profile needs and does not
// have, and reports how many were written. It stops at the first failure and hands that
// error back: the usual cause is the very network this list exists for, and carrying on
// would spend a request per tag to produce the same failure.
func (a *App) DownloadMissingRuleSets() (int, error) {
	list, err := a.RuleSets()
	if err != nil {
		return 0, err
	}
	written := 0
	// One rebuild for the batch, not one per file: the reader pressed 全部下载, and the
	// core only needs to have read the directory once it has stopped changing.
	//
	// Deferred rather than done at the end: a batch that fails on its third tag
	// has still written two, and returning before the rebuild left the disk
	// changed while the core went on reading the old directory — the reader saw
	// "下载失败" over a list that had half of it. DownloadRuleSet (the single-file
	// path) already rebuilds whatever happened to the file itself.
	defer func() {
		if written > 0 {
			a.ruleSetsChanged()
		}
	}()
	for _, rs := range list {
		if !rs.Needed || rs.Local || rs.URL == "" {
			continue
		}
		if err := a.fetchRuleSet(rs.Tag); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// RemoveRuleSet deletes a tag's local copy, sending it back to the remote.
func (a *App) RemoveRuleSet(tag string) error {
	if a.curSetting() == nil {
		return fmt.Errorf("backend not initialised")
	}
	// The tag is a file name here, so a separator in it would step out of the directory.
	if tag == "" || strings.ContainsAny(tag, `/\`) || strings.Contains(tag, "..") {
		return fmt.Errorf("无效的规则集名称：%s", tag)
	}
	if err := os.Remove(filepath.Join(a.curSetting().RuleSetDir(), tag+".srs")); err != nil {
		return err
	}
	// Everything below is the read-modify-write of the two note files, so it is held
	// together: see ruleSetMu.
	ruleSetMu.Lock()
	defer ruleSetMu.Unlock()
	// The country note goes with the file it was made for. A leftover would say a rule
	// set is still in the fold that the directory no longer holds.
	if m := a.loadRuleSetCountries(); m[tag] != "" {
		delete(m, tag)
		_ = a.saveRuleSetCountries(m)
	}
	// The source's version record as well: a copy this app must fetch again has to start
	// from a record made of what arrives, not of what was here.
	if v := a.loadRuleSetVersions(); (v[tag] != ruleSetVersion{}) {
		delete(v, tag)
		_ = a.saveRuleSetVersions(v)
	}
	// The rule set is gone, so whatever the core built out of it is stale too: the file
	// it read at start-up is no longer the one on disk.
	a.ruleSetsChanged()
	return nil
}

// ruleSetMu serialises the read-modify-write of the two note files that live beside the
// rule sets. Every mutation — a download, a removal, an import — loads one of those
// maps, changes a key and saves it back, and two of them interleaving means the second
// save writes the first one's change away: the loser is a tag whose record says "same as
// the source" for ever (see RuleSetUpdates, which then never offers the update again) or
// a country note that simply vanishes. One lock for both files: the same operations
// touch both, and two locks would only be two things to keep in step.
var ruleSetMu sync.Mutex

// ruleSetDLMu serialises downloads of one tag, and only one tag: two entries can be
// fetched at once, but the same entry cannot be fetched twice at once. The temp name is
// fixed (path+".tmp"), so two downloads of one tag wrote the same file — one truncating
// what the other was still copying — and both renamed onto the real name afterwards. What
// landed was a mixture, and LooksLikeRuleSet checks the magic number only, so it passed
// for a rule set until the core refused the whole config at its next start.
var ruleSetDLMu sync.Map // tag -> *sync.Mutex

// lockRuleSetDownload takes the tag's download slot and returns its release.
func lockRuleSetDownload(tag string) func() {
	m, _ := ruleSetDLMu.LoadOrStore(tag, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// ruleSetCountriesPath is where a reader's own note about an imported rule set lives:
// tag → country, beside the files it describes rather than in the settings file, because
// it is about the contents of this directory and travels with it.
func (a *App) ruleSetCountriesPath() string {
	return filepath.Join(a.curSetting().RuleSetDir(), "countries.json")
}

// loadRuleSetCountries reads that note. A missing or unreadable file is no note at all,
// which is what every rule set the profile names by tag is.
func (a *App) loadRuleSetCountries() map[string]string {
	out := map[string]string{}
	if a.curSetting() == nil {
		return out
	}
	data, err := os.ReadFile(a.ruleSetCountriesPath())
	if err != nil {
		return out
	}
	var m map[string]string
	if json.Unmarshal(data, &m) == nil {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func (a *App) saveRuleSetCountries(m map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.curSetting().RuleSetDir(), 0o755); err != nil {
		return err
	}
	return writeNoteFile(a.ruleSetCountriesPath(), data)
}

// PickRuleSetFiles opens the native picker for rule-set files, several at a time —
// a country's rules rarely come as one file, and making the round trip per file is
// making it once for each file the reader meant to add together.
// An empty list means the reader cancelled.
func (a *App) PickRuleSetFiles() ([]string, error) {
	if a.curCtx() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	return runtime.OpenMultipleFilesDialog(a.curCtx(), runtime.OpenDialogOptions{
		Title: "选择规则集文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "规则集 (*.srs)", Pattern: "*.srs"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

// ImportRuleSets copies the picked files into the rule-set directory under their own
// names — which is the tag the routing lists will refer to them by — and records which
// country they are for. It returns how many were written.
//
// A file whose header is not a rule set is refused rather than filed: the core stops
// starting on a rule set it cannot parse, and this is the one door through which a reader
// can hand it anything. An existing tag is replaced, which is what re-importing an
// updated file of one's own has to mean.
func (a *App) ImportRuleSets(paths []string, country string) (int, error) {
	if a.curSetting() == nil {
		return 0, fmt.Errorf("backend not initialised")
	}
	dir := a.curSetting().RuleSetDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	// The country notes and the version records below are read here and written at the
	// end: the whole sequence is one edit of them (see ruleSetMu).
	ruleSetMu.Lock()
	defer ruleSetMu.Unlock()
	countries := a.loadRuleSetCountries()
	written := 0
	for _, p := range paths {
		if !configbuilder.LooksLikeRuleSet(p) {
			return written, fmt.Errorf("不是有效的规则集文件：%s", filepath.Base(p))
		}
		tag := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if tag == "" || strings.Contains(tag, "..") {
			return written, fmt.Errorf("无效的规则集名称：%s", filepath.Base(p))
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return written, err
		}
		dest := filepath.Join(dir, tag+".srs")
		tmp := dest + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return written, err
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Remove(tmp)
			return written, err
		}
		if c := strings.TrimSpace(country); c != "" {
			countries[tag] = c
		}
		// A file the reader brought over the desk supersedes any record about the copy
		// it replaced: the next check compares against the source, and this arrival did
		// not come from it.
		if v := a.loadRuleSetVersions(); v[tag] != (ruleSetVersion{}) {
			delete(v, tag)
			_ = a.saveRuleSetVersions(v)
		}
		written++
	}
	if err := a.saveRuleSetCountries(countries); err != nil {
		return written, err
	}
	if written > 0 {
		a.ruleSetsChanged()
	}
	return written, nil
}

// ruleSetVersion is what a rule set's source said about itself the last time this app
// fetched it: the entity tag and the last-modified stamp, the two headers a fresh HEAD
// is compared against. Both are blank until the first download through this app — a file
// picked up by hand or imported has no record, and a source that serves neither header
// reports same-as-saved rather than different.
type ruleSetVersion struct {
	ETag    string `json:"etag,omitempty"`
	LastMod string `json:"last_modified,omitempty"`
	// Source is the URL this copy was actually fetched from. It has to be part of the
	// record, because the sources answer the same question in different words: the core's
	// own speaks content etags, the mirror speaks weak ones, and a stamp compared across
	// the two reads as "newer" at every opening of the dialog — a button under a rule
	// whose file is the newest there is. The check therefore asks the source the copy
	// came from, never the other one.
	Source string `json:"source,omitempty"`
}

// ruleSetVersionsPath is where those records live: beside the files they describe, the
// way the country notes are, because they describe the contents of this directory.
func (a *App) ruleSetVersionsPath() string {
	return filepath.Join(a.curSetting().RuleSetDir(), "versions.json")
}

// loadRuleSetVersions reads that record. A missing or unreadable file is no record:
// every rule set then counts as without-a-version, which is what a directory this app
// has never downloaded into is.
func (a *App) loadRuleSetVersions() map[string]ruleSetVersion {
	out := map[string]ruleSetVersion{}
	if a.curSetting() == nil {
		return out
	}
	data, err := os.ReadFile(a.ruleSetVersionsPath())
	if err != nil {
		return out
	}
	var m map[string]ruleSetVersion
	if json.Unmarshal(data, &m) == nil {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func (a *App) saveRuleSetVersions(m map[string]ruleSetVersion) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.curSetting().RuleSetDir(), 0o755); err != nil {
		return err
	}
	return writeNoteFile(a.ruleSetVersionsPath(), data)
}

// RuleSetUpdates asks each source whether its copy has changed since this app last
// fetched it. The answer is cheap: one HEAD per rule set this app has a record for, the
// entity tag and the last-modified stamp read off the answer, no bodies moved. A rule
// set with no record — imported by hand, or downloaded before any of this existed, or
// from a source that serves neither header — is reported as up to date rather than
// changed, because a button under a rule every open of the dialog says has changed is
// a button that stops meaning anything.
//
// The asking happens all at once rather than one after another, so the card's own
// refresh does not wait a timeout per rule set on the very network that cannot reach
// the source.
func (a *App) RuleSetUpdates() (map[string]bool, error) {
	if a.curSetting() == nil {
		return nil, fmt.Errorf("backend not initialised")
	}
	list, err := a.RuleSets()
	if err != nil {
		return nil, err
	}
	versions := a.loadRuleSetVersions()
	out := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 10 * time.Second}
	for _, rs := range list {
		v, ok := versions[rs.Tag]
		if !rs.Needed || !rs.Local || rs.URL == "" || !ok || (v.ETag == "" && v.LastMod == "") {
			mu.Lock()
			out[rs.Tag] = false
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(tag string, saved ruleSetVersion) {
			defer wg.Done()
			if a.quitting.Load() {
				mu.Lock()
				out[tag] = false
				mu.Unlock()
				return
			}
			// The rule of the comparison: like for like. The record's own source is
			// asked, and only its answer is believed — raw.githubusercontent's etag and
			// the mirror's are written in different languages, and either one read
			// against the other says "newer" about a file that is the newest there is.
			// A record whose source was never written down (a copy fetched before this
			// field existed) is unverifiable for the same reason, and is answered "no
			// news" until a download through this app renews the record with its source.
			changed := false
			if ask := saved.Source; ask != "" {
				req, err := http.NewRequest(http.MethodHead, ask, nil)
				if err == nil {
					req.Header.Set("User-Agent", "Eagle-Go")
					resp, derr := client.Do(req)
					if derr == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
						if resp.StatusCode == http.StatusOK {
							etag := resp.Header.Get("ETag")
							lastMod := resp.Header.Get("Last-Modified")
							// Both headers move together at these sources; either one
							// differing from what was saved is a copy that has moved
							// under the reader. Headers neither serve give nothing to
							// compare against, which reads as no news rather than as
							// newer.
							if etag != "" && saved.ETag != "" && etag != saved.ETag {
								changed = true
							} else if etag == "" && lastMod != "" && saved.LastMod != "" && lastMod != saved.LastMod {
								changed = true
							}
						}
					}
				}
			}
			mu.Lock()
			out[tag] = changed
			mu.Unlock()
		}(rs.Tag, v)
	}
	wg.Wait()
	return out, nil
}

// fetchRuleSet downloads a tag's rule set from its sources in order — the core's own
// first, the mirror second — to a temporary neighbour, checks the rule-set header, and
// only then moves the file into place, recording what the source said about itself so
// the next check has something to compare against.
//
// The intermediate name is the whole reason for the two steps: a truncated or intercepted
// download written straight to the real name would be handed to the core on its next
// start, and a rule set the core cannot parse stops it from starting at all — a far worse
// outcome than a rule that does not match.
func (a *App) fetchRuleSet(tag string) error {
	if a.curSetting() == nil {
		return fmt.Errorf("backend not initialised")
	}
	// The tag becomes a file name below, so a separator in it would step out of the
	// rule-set directory. Removing one has always refused those; this side — the one that
	// WRITES — did not, so a tag carrying ".." put a .srs wherever it liked. The sources
	// lookup rejects such a tag first, in practice, but "in practice" is not a reason to
	// leave a write unguarded.
	if tag == "" || strings.ContainsAny(tag, `/\`) || strings.Contains(tag, "..") {
		return fmt.Errorf("无效的规则集名称：%s", tag)
	}
	// One download of this tag at a time; the buttons that start them (the row's own and
	// "download everything") do not know about each other (see ruleSetDLMu).
	unlockDownload := lockRuleSetDownload(tag)
	defer unlockDownload()
	sources := configbuilder.RuleSetSources(tag)
	if len(sources) == 0 {
		return fmt.Errorf("没有这个规则集的来源：%s", tag)
	}
	path := filepath.Join(a.curSetting().RuleSetDir(), tag+".srs")
	var etag, lastMod, used string
	var err error
	for _, url := range sources {
		etag, lastMod, err = a.fetchRuleSetFrom(url, path)
		if err == nil {
			used = url
			break
		}
	}
	if err != nil {
		return err
	}
	// The record of what this source said is a read-modify-write of versions.json like
	// any other mutation here (see ruleSetMu): two downloads finishing at once used to
	// leave one of the two records unwritten, and a missing record reads as "same as the
	// source" at every later check — the update is never offered again.
	ruleSetMu.Lock()
	defer ruleSetMu.Unlock()
	versions := a.loadRuleSetVersions()
	versions[tag] = ruleSetVersion{ETag: etag, LastMod: lastMod, Source: used}
	return a.saveRuleSetVersions(versions)
}

// writeNoteFile writes one of the two note files beside the rule sets — countries.json
// and versions.json — the atomic way: to a temporary neighbour, then a rename. A plain
// write was a truncate first, and a reader that landed inside it parsed half a file,
// answered "no notes at all", and left the card saying every rule set is up to date.
func writeNoteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// fetchRuleSetFrom downloads one source to the temporary neighbour and names what the
// response said about itself, for the record the caller writes on success.
func (a *App) fetchRuleSetFrom(url, path string) (string, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "Eagle-Go")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", "", err
	}
	// A rule set is tens of kilobytes. The cap is here so that a captive portal's page,
	// answered with 200, cannot be written into the directory as if it were one.
	n, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	// A body that stopped early is not a rule set either, and the check below cannot see
	// it: a truncated .srs still begins with the magic number, so it passes for one and
	// is only discovered by sing-box refusing the whole config at its next start — "the
	// core will not start", with the reason two screens away in another program's log.
	// The header knows the size, so ask it. (Left alone when the length is unknown, which
	// is what a chunked or transparently decompressed answer reports.)
	if err == nil && resp.ContentLength > 0 && n != resp.ContentLength {
		err = fmt.Errorf("下载不完整（%d/%d 字节）", n, resp.ContentLength)
	}
	if err == nil && !configbuilder.LooksLikeRuleSet(tmp) {
		err = fmt.Errorf("下载到的内容不是规则集（可能被网络拦截）")
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	return resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}
