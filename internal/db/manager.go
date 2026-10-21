package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Manager owns profiles and groups on disk. The layout is intentionally
// identical to nekoray so an existing config/ directory can be read directly:
//
//	<base>/groups/pm.json      {"groups":[gid,...]}
//	<base>/groups/<gid>.json   group definitions
//	<base>/profiles/<pid>.json profile definitions
//
// Every exported method is safe for concurrent use: the speed tester reads
// profiles from worker goroutines while the UI may add, delete or re-import
// them at the same time.
type Manager struct {
	mu sync.RWMutex

	baseDir string

	groupsTabOrder []int
	profiles       map[int]*ProxyEntity
	groups         map[int]*Group

	profilesIDOrder []int
	groupsIDOrder   []int
}

// NewManager creates a manager rooted at baseDir (the nekoray "config" dir).
func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir:  baseDir,
		profiles: map[int]*ProxyEntity{},
		groups:   map[int]*Group{},
	}
}

// BaseDir returns the config root.
func (m *Manager) BaseDir() string { return m.baseDir }

func (m *Manager) profilePath(id int) string {
	return filepath.Join(m.baseDir, "profiles", fmt.Sprintf("%d.json", id))
}
func (m *Manager) groupPath(id int) string {
	return filepath.Join(m.baseDir, "groups", fmt.Sprintf("%d.json", id))
}
func (m *Manager) pmPath() string {
	return filepath.Join(m.baseDir, "groups", "pm.json")
}

// Load reads groups/pm.json plus every profiles/*.json and groups/*.json.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_ = os.MkdirAll(filepath.Join(m.baseDir, "profiles"), 0o755)
	_ = os.MkdirAll(filepath.Join(m.baseDir, "groups"), 0o755)

	m.groupsTabOrder = nil
	var loadedOrder []int
	if data, err := os.ReadFile(m.pmPath()); err == nil {
		var pm struct {
			Groups []int `json:"groups"`
		}
		if json.Unmarshal(data, &pm) == nil {
			loadedOrder = append(loadedOrder, pm.Groups...)
		}
	}

	m.profiles = map[int]*ProxyEntity{}
	m.groups = map[int]*Group{}

	// profiles
	m.profilesIDOrder = listIntJSON(filepath.Join(m.baseDir, "profiles"))
	for _, id := range m.profilesIDOrder {
		ent := &ProxyEntity{}
		if err := readJSON(m.profilePath(id), ent); err != nil {
			continue
		}
		// corrupted / unknown protocol
		if ent.Type == "" || (ent.Bean != nil && ent.Bean.Int("_v") == -114514) {
			continue
		}
		ent.Ensure()
		ent.ID = id
		m.profiles[id] = ent
	}

	// groups
	m.groupsIDOrder = listIntJSON(filepath.Join(m.baseDir, "groups"))
	for _, id := range m.groupsIDOrder {
		g := &Group{ID: -1, FrontProxyID: -1}
		if err := readJSON(m.groupPath(id), g); err != nil || g.ID != id {
			continue
		}
		if !containsInt(loadedOrder, id) {
			loadedOrder = append(loadedOrder, id)
		}
		m.groups[id] = g
	}
	for _, id := range loadedOrder {
		if _, ok := m.groups[id]; ok {
			m.groupsTabOrder = append(m.groupsTabOrder, id)
		}
	}
	// No first-setup group is created: an empty tree is a legitimate state now
	// (the reader may delete every group there is), and the subscription card
	// is where the first one is made from anyway.
	return nil
}

// SaveManager persists groups/pm.json (the group tab order).
func (m *Manager) SaveManager() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveManagerLocked()
}

func (m *Manager) saveManagerLocked() error {
	order := append([]int{}, m.groupsTabOrder...)
	return writeJSON(m.pmPath(), map[string]interface{}{"groups": order})
}

// SaveProfile persists a single profile file.
func (m *Manager) SaveProfile(e *ProxyEntity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveProfileLocked(e)
}

// saveProfileLocked persists a single profile file; caller holds m.mu (at
// least for reading).
func (m *Manager) saveProfileLocked(e *ProxyEntity) error {
	e.Ensure()
	return writeJSON(m.profilePath(e.ID), e)
}

// SaveGroup persists a single group file.
func (m *Manager) SaveGroup(g *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveGroupLocked(g)
}

// saveGroupLocked persists a single group file; caller holds m.mu (at least
// for reading).
func (m *Manager) saveGroupLocked(g *Group) error {
	return writeJSON(m.groupPath(g.ID), g)
}

// SaveAll persists every profile, group and the manager index.
func (m *Manager) SaveAll() error {
	// Full write lock: the loop assigns e.ID / g.ID, and a read lock would let
	// that write race the other concurrent readers.
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.profiles {
		e.ID = id
		if err := m.saveProfileLocked(e); err != nil {
			return err
		}
	}
	for id, g := range m.groups {
		g.ID = id
		if err := m.saveGroupLocked(g); err != nil {
			return err
		}
	}
	return m.saveManagerLocked()
}

// GetProfile returns the LIVE entity by id, or nil. Readers that walk the
// bean maps — config building, speed tests, anything held past the call —
// must take SnapshotProfile instead: the live map is only ever touched under
// m.mu (see UpdateProfile), and an unlocked walk of it against a locked writer
// is a concurrent map access, which kills the process outright. This stays for
// nil-checks and single-field reads that do not survive a lock hold.
func (m *Manager) GetProfile(id int) *ProxyEntity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[id]
}

// GetGroup returns a group by id, or nil.
func (m *Manager) GetGroup(id int) *Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.groups[id]
}

// HasProfile reports whether a profile id exists.
func (m *Manager) HasProfile(id int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.profiles[id]
	return ok
}

// HasGroup reports whether a group id exists.
func (m *Manager) HasGroup(id int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.groups[id]
	return ok
}

// AllProfiles returns every profile, cloned (deep) while the read lock is held.
// Every caller walks the bean maps after receiving the list, and a live pointer
// would hand them a map that a locked writer may be mutating at the same
// instant — a concurrent map read that has fatal-crashed the process before.
func (m *Manager) AllProfiles() []*ProxyEntity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ProxyEntity, 0, len(m.profiles))
	for _, id := range m.profilesIDOrder {
		if p := m.profiles[id]; p != nil {
			out = append(out, p.clone())
		}
	}
	return out
}

// SnapshotProfile returns a deep copy of the profile, or nil. Read-only consumers
// take a snapshot so their walk of the bean maps is safe against a writer that
// mutates the live entity under the manager's lock.
func (m *Manager) SnapshotProfile(id int) *ProxyEntity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[id].clone()
}

// SnapshotGroup returns a deep copy of the group, or nil.
func (m *Manager) SnapshotGroup(id int) *Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.groups[id].clone()
}

// UpdateProfile applies fn to the live entity and — when save is set — persists
// it in the same lock hold, so writers of the bean maps can never run while a
// reader or a JSON save walks the map: concurrent map write/read has crashed the
// process before, and this is the one funnel every such mutation has to pass.
// fn must be quick and must not call back into the manager.
func (m *Manager) UpdateProfile(id int, save bool, fn func(*ProxyEntity)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.profiles[id]
	if e == nil {
		return fmt.Errorf("profile %d not found", id)
	}
	fn(e)
	if save {
		return m.saveProfileLocked(e)
	}
	return nil
}

// ReplaceProfile overwrites the stored entity with the edited one (SaveProfileJSON's
// whole-entity editor) under the lock, persisting both states in a single step.
func (m *Manager) ReplaceProfile(id int, e *ProxyEntity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.profiles[id]
	if cur == nil {
		return fmt.Errorf("profile %d not found", id)
	}
	e.ID = id
	e.Ensure()
	*cur = *e
	return m.saveProfileLocked(cur)
}

// UpdateGroup applies fn to the live group under the lock and persists it — the
// group twin of UpdateProfile.
func (m *Manager) UpdateGroup(id int, save bool, fn func(*Group)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.groups[id]
	if g == nil {
		return fmt.Errorf("group %d not found", id)
	}
	fn(g)
	if save {
		return m.saveGroupLocked(g)
	}
	return nil
}

// SpliceGroup overwrites the stored group's value with g's and persists it, under
// the lock (UpdateGroup's whole-overwrite form — the UI editor and the
// subscription update commit whole snapshots).
func (m *Manager) SpliceGroup(id int, g *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.groups[id]
	if cur == nil {
		return fmt.Errorf("group %d not found", id)
	}
	*cur = *g
	return m.saveGroupLocked(cur)
}

// MoveProfile moves a profile to another group, keeping both groups' order
// lists consistent — the old app-side body of MoveProfile under one lock.
func (m *Manager) MoveProfile(id, gid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ent := m.profiles[id]
	if ent == nil {
		return fmt.Errorf("profile %d not found", id)
	}
	if gid < 0 || gid == ent.GID {
		return nil
	}
	// A target that is not there (deleted by another window, or a stale id from
	// the front end) would take the entity out of its old group and put it
	// nowhere: it then shows up only in the cross-group search and no cleanup
	// ever reaches it, because every cleanup walks groups.
	if m.groups[gid] == nil {
		return fmt.Errorf("group %d not found", gid)
	}
	if old := m.groups[ent.GID]; old != nil && len(old.Order) > 0 {
		old.Order = removeInt(old.Order, id)
		if err := m.saveGroupLocked(old); err != nil {
			return err
		}
	}
	if next := m.groups[gid]; next != nil && len(next.Order) > 0 {
		next.Order = append(next.Order, id)
		if err := m.saveGroupLocked(next); err != nil {
			return err
		}
	}
	ent.GID = gid
	return m.saveProfileLocked(ent)
}

// ClearMetrics zeroes the stored latency, speed and traffic counters on every
// profile under the lock and persists each one.
func (m *Manager) ClearMetrics() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.profiles {
		e.Latency = 0
		e.Speed = 0
		e.Report = ""
		e.Ensure()
		e.Traffic.Set("up", 0)
		e.Traffic.Set("down", 0)
		if err := m.saveProfileLocked(e); err != nil {
			return err
		}
	}
	return nil
}

// TabOrder returns a copy of the group tab order.
func (m *Manager) TabOrder() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]int{}, m.groupsTabOrder...)
}

// AddProfile assigns a new id and persists the profile.
func (m *Manager) AddProfile(e *ProxyEntity, gid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	e.Ensure()
	if e.ID >= 0 {
		return fmt.Errorf("profile already has an id")
	}
	if gid < 0 {
		gid = e.GID
	}
	e.GID = gid
	e.ID = m.newProfileIDLocked()
	m.profiles[e.ID] = e
	m.profilesIDOrder = append(m.profilesIDOrder, e.ID)
	// Under the lock already: SaveProfile would re-enter m.mu and deadlock.
	if err := m.saveProfileLocked(e); err != nil {
		// The write is what makes the node exist. Without this the entity
		// stayed in the in-memory store with an id and no file: visible in
		// AllProfiles (cross-group search), absent from every group's order,
		// gone after the next restart — and a retry of the same import answered
		// "profile already has an id".
		m.undoAddLocked(e)
		return err
	}
	// A subscription group's order becomes non-empty at its first update, and
	// ProfilesWithOrder lists exactly that list — a profile added by hand
	// (CreateProfile, CloneProfile, ImportLink) that never entered it was
	// invisible in its own group and deleted outright by the next
	// subscription clean-up. The subscription path does not come through
	// here: its walk rebuilds the order itself (internal/sub/update.go), so
	// only the hand-added tail is maintained. Same tail care as MoveProfile.
	if g := m.groups[gid]; g != nil && len(g.Order) > 0 {
		g.Order = append(g.Order, e.ID)
		return m.saveGroupLocked(g)
	}
	return nil
}

// AddProfiles assigns fresh ids to a whole parse batch and persists each one
// under a single lock acquisition. The subscription import hands the manager
// hundreds of entities at once, and one lock queue per entity made the import
// wait on itself; the per-entity work (id, map, file) is unchanged.
func (m *Manager) AddProfiles(list []*ProxyEntity, gid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range list {
		if e == nil {
			continue
		}
		e.Ensure()
		if e.ID >= 0 {
			return fmt.Errorf("profile already has an id")
		}
		// Per entity, not the parameter: assigning back into gid made the first
		// element's group the whole batch's, so a call with gid<0 filed every
		// node after the first under the first one's group.
		target := gid
		if target < 0 {
			target = e.GID
		}
		e.GID = target
		e.ID = m.newProfileIDLocked()
		m.profiles[e.ID] = e
		m.profilesIDOrder = append(m.profilesIDOrder, e.ID)
		if err := m.saveProfileLocked(e); err != nil {
			// This one is taken back (see AddProfile); the batch's earlier
			// entities stay, because their files exist and they are whole.
			m.undoAddLocked(e)
			return err
		}
	}
	return nil
}

// undoAddLocked takes back the in-memory half of a profile whose file could not
// be written. Caller holds m.mu.
func (m *Manager) undoAddLocked(e *ProxyEntity) {
	if e == nil || e.ID < 0 {
		return
	}
	delete(m.profiles, e.ID)
	for i := len(m.profilesIDOrder) - 1; i >= 0; i-- {
		if m.profilesIDOrder[i] == e.ID {
			m.profilesIDOrder = append(m.profilesIDOrder[:i], m.profilesIDOrder[i+1:]...)
			break
		}
	}
	// Back to "no id yet", so the caller can retry the same entity once the
	// disk problem is gone.
	e.ID = -1
}

// DeleteProfile removes a profile file.
func (m *Manager) DeleteProfile(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleteProfileLocked(id)
}

func (m *Manager) deleteProfileLocked(id int) error {
	if id < 0 {
		return nil
	}
	ent := m.profiles[id]
	delete(m.profiles, id)
	m.profilesIDOrder = removeInt(m.profilesIDOrder, id)
	// The owning group's order holds this id too. A stale entry left there
	// re-arms the id-reuse trap: newProfileIDLocked recycles the tail id, and
	// the next profile to draw it then rendered inside a group it never
	// joined and was cleaned up by that group's subscription update. Drop it
	// here — saved only when something actually came off, so a clearing
	// subscription walk does not pay a group-file write per entity.
	if ent != nil {
		if g := m.groups[ent.GID]; g != nil && len(g.Order) > 0 {
			if next := removeInt(g.Order, id); len(next) != len(g.Order) {
				g.Order = next
				if err := m.saveGroupLocked(g); err != nil {
					return err
				}
			}
		}
	}
	// Any group's front proxy that names this node goes with it. A reference
	// left standing is not a cosmetic detail: the config builder answers
	// "front proxy entity not found" for one, and every node of that group
	// refused to start with an error naming a profile the reader had already
	// deleted — often minutes earlier, from a different group. Cleared here
	// rather than at the call sites so every delete path (manual, a clearing
	// subscription, a dedup) keeps the tree consistent.
	for _, g := range m.groups {
		if g != nil && g.FrontProxyID == id {
			g.FrontProxyID = -1
			if err := m.saveGroupLocked(g); err != nil {
				return err
			}
		}
	}
	return os.Remove(m.profilePath(id))
}

// AddGroup assigns a new id and persists the group.
func (m *Manager) AddGroup(g *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addGroupLocked(g)
}

func (m *Manager) addGroupLocked(g *Group) error {
	if g.ID >= 0 {
		return fmt.Errorf("group already has an id")
	}
	if g.FrontProxyID == 0 {
		g.FrontProxyID = -1
	}
	g.ID = m.newGroupIDLocked()
	m.groups[g.ID] = g
	m.groupsIDOrder = append(m.groupsIDOrder, g.ID)
	m.groupsTabOrder = append(m.groupsTabOrder, g.ID)
	// Locked already: SaveGroup would re-enter m.mu and deadlock.
	if err := m.saveGroupLocked(g); err != nil {
		return err
	}
	return m.saveManagerLocked()
}

// DeleteGroup removes a group and all profiles inside it. There is no
// last-group protection: the reader may empty the table entirely, and an
// empty tree is a state the rest of the app answers gracefully (an empty
// list, a fresh rail).
func (m *Manager) DeleteGroup(gid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var toDelete []int
	for id, p := range m.profiles {
		if p.GID == gid {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		_ = m.deleteProfileLocked(id)
	}
	delete(m.groups, gid)
	m.groupsIDOrder = removeInt(m.groupsIDOrder, gid)
	m.groupsTabOrder = removeInt(m.groupsTabOrder, gid)
	_ = m.saveManagerLocked()
	return os.Remove(m.groupPath(gid))
}

// ProfilesWithOrder returns a group's profiles honouring the custom order —
// cloned under the read lock, for the same map-safety reason as AllProfiles.
func (m *Manager) ProfilesWithOrder(gid int) []*ProxyEntity {
	m.mu.RLock()
	defer m.mu.RUnlock()

	g := m.groups[gid]
	if g == nil {
		return nil
	}
	var out []*ProxyEntity
	if len(g.Order) == 0 {
		for _, id := range m.profilesIDOrder {
			if p := m.profiles[id]; p != nil && p.GID == gid {
				out = append(out, p.clone())
			}
		}
		return out
	}
	for _, id := range g.Order {
		// The order is per-group but ids are global: a stale entry (a profile
		// deleted before order maintenance existed, an id later reused for a
		// profile of another group) must not render its current bearer here.
		if p := m.profiles[id]; p != nil && p.GID == gid {
			out = append(out, p.clone())
		}
	}
	return out
}

func (m *Manager) newProfileIDLocked() int {
	if len(m.profilesIDOrder) == 0 {
		return 0
	}
	return m.profilesIDOrder[len(m.profilesIDOrder)-1] + 1
}

func (m *Manager) newGroupIDLocked() int {
	if len(m.groupsIDOrder) == 0 {
		return 0
	}
	return m.groupsIDOrder[len(m.groupsIDOrder)-1] + 1
}

// --- helpers ---

func listIntJSON(dir string) []int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		stem := name[:len(name)-len(".json")]
		id, err := strconv.Atoi(stem)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON persists a value atomically: the bytes land in a sibling temp file
// that is then renamed over the target. os.WriteFile truncates first, so a kill
// or a power loss inside the write leaves a half-written file behind — and the
// reader answers a half-written file by dropping the profile or the group
// silently (see Load), which is a node the reader never sees go missing. One
// temp name per target rather than a random one: writers of the same file
// already serialise on m.mu, and a fixed name cannot litter the folder with
// orphans when a write dies.
func writeJSON(path string, v interface{}) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
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

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// removeInt returns a copy of list without v. The input slice is left alone so
// callers holding a reference never see it change underneath them.
func removeInt(list []int, v int) []int {
	out := make([]int, 0, len(list))
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
