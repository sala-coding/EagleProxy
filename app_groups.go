package main

import (
	"eagle/internal/core"
	"eagle/internal/db"
	"eagle/internal/format"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// GroupInfo is the frontend-facing summary of a group.
type GroupInfo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Archive       bool   `json:"archive"`
	ProfileCount  int    `json:"profileCount"`
	URL           string `json:"url"`
	SubLastUpdate int64  `json:"subLastUpdate"`
	// Info is the subscription's own Subscription-UserInfo header, kept verbatim.
	// The list needs it because the quota and expiry it carries are the only
	// warning a subscription ever gives, and reading them means a round trip per
	// row would be the alternative.
	Info string `json:"info"`
}

// ProfileInfo is the frontend-facing summary of a profile.
type ProfileInfo struct {
	ID      int    `json:"id"`
	GID     int    `json:"gid"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Latency int    `json:"latency"`
	Report  string `json:"report"`
	// Speed is the download-speed result in MB/s (0 = not measured). The list
	// shows Speed when it is set, otherwise Latency.
	Speed float64 `json:"speed"`
	// Up / Down are the bytes this profile has carried in total.
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
	// Connection details shown as their own columns.
	Port     int    `json:"port"`
	Network  string `json:"network"`  // transport: tcp / ws / grpc / http / httpupgrade
	Security string `json:"security"` // TLS label, empty when the node is plain
	TLSNote  string `json:"tlsNote"`  // tooltip: SNI / Host / ALPN / uTLS / cert check
	// Group is the name of the group (subscription) the profile belongs to.
	Group string `json:"group"`
}

// ListGroups returns all groups in tab order.
func (a *App) ListGroups() []GroupInfo {
	out := []GroupInfo{}
	if a.curMgr() == nil {
		return out
	}
	for _, gid := range a.curMgr().TabOrder() {
		g := a.curMgr().SnapshotGroup(gid)
		if g == nil {
			continue
		}
		out = append(out, GroupInfo{
			ID:            g.ID,
			Name:          g.Name,
			Archive:       g.Archive,
			ProfileCount:  len(a.curMgr().ProfilesWithOrder(g.ID)),
			URL:           g.URL,
			SubLastUpdate: g.SubLastUpdate,
			Info:          g.Info,
		})
	}
	return out
}

// GetGroup returns a full group definition — a snapshot, so the caller (the edit
// dialog and its serialization) cannot race a concurrent writer's fields.
func (a *App) GetGroup(id int) (*db.Group, error) {
	g := a.curMgr().SnapshotGroup(id)
	if g == nil {
		return nil, fmt.Errorf("group %d not found", id)
	}
	return g, nil
}

// CreateGroup adds a new group.
func (a *App) CreateGroup(name string) (int, error) {
	g := &db.Group{ID: -1, FrontProxyID: -1, Name: name}
	if err := a.curMgr().AddGroup(g); err != nil {
		return -1, err
	}
	return g.ID, nil
}

// UpdateGroup persists a modified group — by field, not as a splice.
//
// The editor hands over a snapshot taken when it opened, minutes before the
// save lands; a background walk can have rewritten Order, Info and
// SubLastUpdate in between, and the whole-struct overwrite (SpliceGroup) rolled
// those back to the snapshot's values — the walk's own comment in
// internal/sub/update.go names this exact hazard and commits by field for that
// reason. The dialog owns four fields (name, url, front proxy, interval); only
// those go through, and the walk's fields pass untouched. Under subMu for the
// same reason DeleteGroup is: a walk in flight holds a snapshot of this group.
func (a *App) UpdateGroup(g db.Group) error {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	return a.curMgr().UpdateGroup(g.ID, true, func(live *db.Group) {
		live.Name = g.Name
		live.URL = g.URL
		live.FrontProxyID = g.FrontProxyID
		live.SubInterval = g.SubInterval
	})
}

// DeleteGroup removes a group and its profiles.
//
// Under the subscription lock, because a walk in flight holds a snapshot of this
// group and commits its nodes afterwards: deleting without it left a batch of
// profiles pointing at a group id that no longer exists — nodes nothing lists
// (ProfilesWithOrder answers nil for a missing group) and nothing ever cleans up.
// The delete therefore waits for a walk to finish, which is the price of the tree
// staying consistent.
func (a *App) DeleteGroup(gid int) error {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	// A group whose member is running cannot go: the deletion would take the
	// running id with it — the one anchor every restart path (mode switch,
	// crash recovery, remember) shares. The reader stops or switches first.
	if rid := a.RunningProfile(); rid != core.StoppedID {
		if p := a.curMgr().SnapshotProfile(rid); p != nil && p.GID == gid {
			return fmt.Errorf("该分组的节点正在运行，请先停止或切换节点再删除")
		}
	}
	return a.curMgr().DeleteGroup(gid)
}

// ListProfiles returns the profiles of a group (gid < 0 => all profiles).
// Each entity arrives as a deep snapshot: the walk of the bean maps below must
// not race the (locked) writers of those same live maps.
func (a *App) ListProfiles(gid int) []ProfileInfo {
	out := []ProfileInfo{}
	if a.curMgr() == nil {
		return out
	}
	// AllProfiles / ProfilesWithOrder hand back clones taken under the
	// manager's read lock (see the db comments); no further copy here.
	var ents []*db.ProxyEntity
	if gid < 0 {
		ents = a.curMgr().AllProfiles()
	} else {
		ents = a.curMgr().ProfilesWithOrder(gid)
	}
	for _, e := range ents {
		network, security, note := e.ConnectionInfo()
		gname := ""
		if g := a.curMgr().SnapshotGroup(e.GID); g != nil {
			gname = g.Name
		}
		out = append(out, ProfileInfo{
			ID:       e.ID,
			GID:      e.GID,
			Type:     e.DisplayType(),
			Name:     e.DisplayName(),
			Address:  e.DisplayAddress(),
			Latency:  e.Latency,
			Report:   e.Report,
			Speed:    e.Speed,
			Up:       e.Traffic.Int64("up"),
			Down:     e.Traffic.Int64("down"),
			Port:     e.ServerPort(),
			Network:  network,
			Security: security,
			TLSNote:  note,
			Group:    gname,
		})
	}
	return out
}

// CreateProfile adds an empty profile of the given protocol type.
func (a *App) CreateProfile(typ string, gid int) (int, error) {
	ent := &db.ProxyEntity{Type: typ, ID: -1, GID: gid, Bean: format.New(typ)}
	ent.Ensure()
	if gid < 0 {
		gid = a.defaultGroup()
	}
	if err := a.curMgr().AddProfile(ent, gid); err != nil {
		return -1, err
	}
	return ent.ID, nil
}

// CloneProfile duplicates a profile — from a snapshot, so the marshal below
// walks a map no writer can be mutating.
func (a *App) CloneProfile(id int) (int, error) {
	if a.curMgr() == nil {
		return -1, fmt.Errorf("backend not initialised")
	}
	src := a.curMgr().SnapshotProfile(id)
	if src == nil {
		return -1, fmt.Errorf("profile %d not found", id)
	}
	cp := &db.ProxyEntity{}
	data, err := json.Marshal(src)
	if err != nil {
		return -1, err
	}
	if err := json.Unmarshal(data, cp); err != nil {
		return -1, err
	}
	cp.ID = -1
	// Everything the measurements column reads is reset, not just the latency: the
	// clone is a node nobody has tested, and carrying the original's speed and its
	// accumulated traffic over made it show numbers it never earned — and, with both
	// listed, read as twice the traffic the subscription actually moved.
	cp.Latency = 0
	cp.Speed = 0
	cp.Report = ""
	cp.Ensure()
	cp.Traffic.Set("up", 0)
	cp.Traffic.Set("down", 0)
	if err := a.curMgr().AddProfile(cp, src.GID); err != nil {
		return -1, err
	}
	return cp.ID, nil
}

// DeleteProfile removes a profile.
//
// Under the subscription lock for the same reason as DeleteGroup: a walk in flight holds
// a snapshot of the list and commits it afterwards, which would put a node the reader just
// deleted straight back on disk.
func (a *App) DeleteProfile(id int) error {
	if a.curMgr() == nil {
		return fmt.Errorf("backend not initialised")
	}
	a.subMu.Lock()
	defer a.subMu.Unlock()
	// A running node cannot go: its id is the anchor every restart path shares (mode
	// switch, crash recovery, remember), and deleting the row under a live core leaves
	// that anchor naming nothing — the next restart then fails after the tunnel is
	// already down. DeleteGroup refuses for the same reason; this path did not.
	if rid := a.RunningProfile(); rid != core.StoppedID && rid == id {
		return fmt.Errorf("该节点正在运行，请先停止或切换到其他节点再删除")
	}
	if err := a.curMgr().DeleteProfile(id); err != nil {
		return err
	}
	// Every node-list change is announced: the cross-subscription search list holds rows
	// from other groups, and one of those may have just gone. -1 keeps the reader's view.
	a.emitGroupsChanged(-1)
	return nil
}

// MoveProfile moves a profile to another group, order lists and all; the whole
// dance runs under the manager's lock.
func (a *App) MoveProfile(id, gid int) error {
	if a.curMgr() == nil {
		return fmt.Errorf("backend not initialised")
	}
	return a.curMgr().MoveProfile(id, gid)
}

// GetProfileJSON returns the raw profile JSON for editing.
func (a *App) GetProfileJSON(id int) (string, error) {
	ent := a.curMgr().SnapshotProfile(id)
	if ent == nil {
		return "", fmt.Errorf("profile %d not found", id)
	}
	data, err := json.MarshalIndent(ent, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveProfileJSON persists an edited profile JSON.
//
// Under the subscription lock: a walk in flight commits a snapshot taken before this edit,
// and a save that landed in between would be overwritten by the node as it used to be.
func (a *App) SaveProfileJSON(id int, jsonStr string) error {
	if a.curMgr() == nil {
		return fmt.Errorf("backend not initialised")
	}
	a.subMu.Lock()
	defer a.subMu.Unlock()
	if !a.curMgr().HasProfile(id) {
		return fmt.Errorf("profile %d not found", id)
	}
	edited := &db.ProxyEntity{}
	if err := json.Unmarshal([]byte(jsonStr), edited); err != nil {
		return fmt.Errorf("invalid profile json: %w", err)
	}
	edited.ID = id
	edited.Ensure()
	if err := a.curMgr().ReplaceProfile(id, edited); err != nil {
		return err
	}
	// The running config is a snapshot: an edit lands in the store and in the next build,
	// not in the core that is already up. Said here rather than left to be discovered —
	// and the hot-switch path notices it by itself (see tryHotSwitch, which compares the
	// node against the content the config was built from and restarts instead of claiming
	// a switch it cannot make).
	if a.RunningProfile() == id {
		a.notifyMessage("配置已保存；该节点正在运行，重新双击该节点后生效", false)
	}
	a.emitGroupsChanged(-1)
	return nil
}

// ImportLink parses a share link and adds it to a group.
func (a *App) ImportLink(link string, gid int) (int, error) {
	if a.curMgr() == nil || a.curSetting() == nil {
		return -1, fmt.Errorf("backend not initialised")
	}
	typ, b, ok := format.ParseLink(strings.TrimSpace(link), a.curSetting())
	if !ok {
		return -1, fmt.Errorf("unrecognised share link")
	}
	ent := &db.ProxyEntity{Type: typ, ID: -1, GID: gid, Bean: b}
	ent.Ensure()
	if gid < 0 {
		gid = a.defaultGroup()
	}
	if err := a.curMgr().AddProfile(ent, gid); err != nil {
		return -1, err
	}
	return ent.ID, nil
}

// ImportLinks parses multiple share links (one per line). A partial import is
// an answer, not a success to be silent about: the ids that made it come back
// with the failures named, so the window can both show the new rows and say
// what did not — a batch of ten with three unrecognised used to answer nil
// error and never mention the three.
func (a *App) ImportLinks(text string, gid int) ([]int, error) {
	var ids []int
	var errs []string
	for _, line := range splitLines(text) {
		id, err := a.ImportLink(line, gid)
		if err != nil {
			errs = append(errs, line+": "+err.Error())
			continue
		}
		ids = append(ids, id)
	}
	if len(errs) > 0 {
		return ids, fmt.Errorf("已导入 %d 条，%d 条失败:\n%s", len(ids), len(errs), strings.Join(errs, "\n"))
	}
	return ids, nil
}

// ExportLink renders a profile as a share link — from a snapshot, since link
// building walks the bean map.
func (a *App) ExportLink(id int, nekoray bool) (string, error) {
	ent := a.curMgr().SnapshotProfile(id)
	if ent == nil {
		return "", fmt.Errorf("profile %d not found", id)
	}
	if nekoray {
		return format.ToNekorayShareLink(ent.Type, ent.Bean), nil
	}
	link := format.ToShareLink(ent.Type, ent.Bean, a.curSetting())
	if link == "" {
		return "", fmt.Errorf("this protocol does not support share links")
	}
	return link, nil
}

// --- batch operations over a group ---
//
// The three nekoray puts in its Edit menu, and the reason they live here rather than in
// the window: each one walks a whole group, and a group is often a hundred nodes from a
// subscription. They act on the group on screen, and each reports how many nodes it
// touched — "removed the duplicates" is worth a number.

// DeduplicateGroup removes nodes that repeat another node's type, address, port and
// credentials, keeping the first of each set along with its name and its measurements.
// Two subscriptions carrying the same servers is the ordinary way this happens: the
// same node arrives twice under different names, and nothing in the list says so.
//
// The credentials are part of the key on purpose. The same host and port can be two
// different nodes behind one CDN, and removing one of those removes a node the reader
// cannot get back.
func (a *App) DeduplicateGroup(gid int) (int, error) {
	if a.curMgr() == nil {
		return 0, fmt.Errorf("backend not initialised")
	}
	// The same walk a subscription update takes: two writers on one group's order
	// interleave, and the walk puts back what this function just removed.
	a.subMu.Lock()
	defer a.subMu.Unlock()
	seen := map[string]bool{}
	removed := 0
	// Every name this repository's bean schema gives a credential (see
	// internal/format/defaults.go): vmess authenticates with id, trojan / vless /
	// anytls / shadowsocks with pass, snell with psk, tuic with uuid+password,
	// hysteria with auth, socks / http / naive with username+password. Naming
	// only three of them left the rest empty, the key collapsed to
	// type|address|port, and the very case the comment above promises to protect
	// — two different nodes behind one CDN host — was the one that got one of
	// them deleted. A field a protocol does not carry reads as empty and costs
	// the key nothing.
	credentialKeys := []string{
		"uuid", "id", "aid", "pass", "password", "psk", "auth", "user", "username",
		"token", "method", "flow", "version", "protocol", "obfs", "obfsPassword",
	}
	for _, p := range a.curMgr().ProfilesWithOrder(gid) {
		// The running node never goes, even as a duplicate: its id is the anchor
		// every restart path shares. Its twin stays instead — a rare row, not a
		// dead tunnel.
		if a.RunningProfile() == p.ID {
			continue
		}
		fields := []string{p.Type, strings.ToLower(p.ServerAddress()), strconv.Itoa(p.ServerPort())}
		for _, k := range credentialKeys {
			fields = append(fields, p.Bean.Str(k))
		}
		key := strings.Join(fields, "|")
		if seen[key] {
			if err := a.curMgr().DeleteProfile(p.ID); err != nil {
				return removed, err
			}
			removed++
			continue
		}
		seen[key] = true
	}
	// Said out loud: the rows went without a per-row event, and a list that keeps them
	// answers the next double-click with "节点已不在列表中" — over an action the reader
	// had just taken themselves.
	if removed > 0 {
		a.emitGroupsChanged(-1)
	}
	return removed, nil
}

// RemoveFailedProfiles deletes the nodes of a group whose latency measurement failed.
//
// Failures only: an unmeasured node (latency 0) is one the reader has not asked about
// yet, and sweeping those up would empty a freshly imported group on the first press.
func (a *App) RemoveFailedProfiles(gid int) (int, error) {
	if a.curMgr() == nil {
		return 0, fmt.Errorf("backend not initialised")
	}
	// Serialised with a subscription walk for the same reason as DeduplicateGroup.
	a.subMu.Lock()
	defer a.subMu.Unlock()
	removed := 0
	for _, p := range a.curMgr().ProfilesWithOrder(gid) {
		if p.Latency >= 0 {
			continue
		}
		// A failed measurement on the node the core is serving is not a reason
		// to pull it out from under the tunnel.
		if a.RunningProfile() == p.ID {
			continue
		}
		if err := a.curMgr().DeleteProfile(p.ID); err != nil {
			return removed, err
		}
		removed++
	}
	if removed > 0 {
		a.emitGroupsChanged(-1)
	}
	return removed, nil
}

// ResolveGroupDomains replaces the domain names in a group's server addresses with the
// addresses those names resolve to, and reports how many changed.
//
// It is for the case where DNS is what keeps failing — a node whose name cannot be
// resolved here but whose address can be reached. It is not reversible from the window,
// which is why the note that follows the press says so: a profile that leaned on its
// own name for TLS (no explicit sni) does not survive the swap.
func (a *App) ResolveGroupDomains(gid int) (int, error) {
	if a.curMgr() == nil {
		return 0, fmt.Errorf("backend not initialised")
	}
	changed := 0
	for _, p := range a.curMgr().ProfilesWithOrder(gid) {
		addr := strings.TrimSpace(p.ServerAddress())
		if addr == "" || net.ParseIP(addr) != nil {
			continue
		}
		ips, err := net.LookupHost(addr)
		if err != nil || len(ips) == 0 {
			continue
		}
		// IPv4 first when there is one: it is what nearly all of these nodes are
		// reached on, and a v6-only pick would break a machine without working IPv6.
		pick := ips[0]
		for _, ip := range ips {
			if strings.Contains(ip, ".") {
				pick = ip
				break
			}
		}
		// The write lands under the manager's lock: bean maps are what the list
		// and the saves read concurrently.
		if err := a.curMgr().UpdateProfile(p.ID, true, func(e *db.ProxyEntity) {
			e.Bean.Set("addr", pick)
		}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func (a *App) defaultGroup() int {
	if a.curMgr() == nil || a.curSetting() == nil {
		return 0
	}
	if a.curMgr().HasGroup(a.curSetting().CurrentGroup) {
		return a.curSetting().CurrentGroup
	}
	if order := a.curMgr().TabOrder(); len(order) > 0 {
		return order[0]
	}
	return 0
}

func splitLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func removeInt(list []int, v int) []int {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
