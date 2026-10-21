package sub

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"eagle/internal/db"
	"eagle/internal/format"
	"eagle/internal/setting"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newUpdater(t *testing.T, gid int) *rawUpdater {
	t.Helper()
	return &rawUpdater{s: &setting.Setting{}, gidAddTo: gid}
}

func clash(t *testing.T, body string) *rawUpdater {
	t.Helper()
	u := newUpdater(t, 1)
	u.update(body)
	return u
}

func byName(u *rawUpdater, name string) (string, map[string]interface{}) {
	for _, e := range u.updated {
		if e.Bean.Str("name") == name {
			out := map[string]interface{}{}
			for k, v := range e.Bean {
				out[k] = v
			}
			return e.Type, out
		}
	}
	return "", nil
}

// ---------------------------------------------------------------------------
// the Clash YAML field mapping
// ---------------------------------------------------------------------------

// A Clash TUIC proxy carries its keepalive as "heartbeat-interval", in SECONDS.
// The importer turns the bare number into a duration — and into MILLISECONDS, so
// "30" becomes "30ms": the keepalive fires three hundred times faster than the
// panel asked for.
func TestReviewClashTuicHeartbeatUnit(t *testing.T) {
	body := `proxies:
  - { name: t1, type: tuic, server: a.example.com, port: 443, uuid: u, password: p, heartbeat-interval: 30 }`
	u := clash(t, body)
	typ, b := byName(u, "t1")
	if typ == "" {
		t.Fatalf("the tuic node was not imported; skipped=%v", u.skipped)
	}
	got, _ := b["heartbeat"].(string)
	if got != "30s" {
		t.Errorf("heartbeat = %q, want %q — Clash writes heartbeat-interval in seconds, "+
			"the importer turned 30 into %q", got, "30s", got)
	}
}

// The same importer, and the speed limits a hysteria2 proxy carries as
// "up: 100 Mbps" / "down: 200 Mbps".
func TestReviewClashHysteria2SpeedUnits(t *testing.T) {
	body := `proxies:
  - { name: h1, type: hysteria2, server: a.example.com, port: 443, password: p, up: "100 Mbps", down: "200 Mbps" }`
	u := clash(t, body)
	typ, b := byName(u, "h1")
	if typ == "" {
		t.Fatalf("the hysteria2 node was not imported; skipped=%v", u.skipped)
	}
	if got, _ := b["uploadMbps"].(int); got != 100 {
		t.Errorf("uploadMbps = %v, want 100", got)
	}
	if got, _ := b["downloadMbps"].(int); got != 200 {
		t.Errorf("downloadMbps = %v, want 200", got)
	}
}

// A Clash shadowsocks proxy with a plugin: the plugin and its options have to
// arrive together.
func TestReviewClashShadowsocksPlugin(t *testing.T) {
	body := `proxies:
  - name: s1
    type: ss
    server: a.example.com
    port: 8388
    cipher: aes-128-gcm
    password: pw
    plugin: obfs
    plugin-opts:
      mode: http
      host: bing.com`
	u := clash(t, body)
	typ, b := byName(u, "s1")
	if typ == "" {
		t.Fatalf("the ss node was not imported; skipped=%v", u.skipped)
	}
	got, _ := b["plugin"].(string)
	if !strings.Contains(got, "obfs-local") || !strings.Contains(got, "obfs=http") {
		t.Errorf("plugin = %q, want it to carry obfs-local and obfs=http", got)
	}
}

// ---------------------------------------------------------------------------
// the plain link list dispatch
// ---------------------------------------------------------------------------

// A subscription served as a plain list of links often carries comment lines
// and blank lines. Neither is a node: neither may be reported as one.
func TestReviewLinkListCommentsAreNotNodes(t *testing.T) {
	body := "# my subscription\n" +
		"\n" +
		"trojan://pw@a.example.com:443#n1\n" +
		"# another comment\n" +
		"vless://uuid@b.example.com:443#n2\n"
	u := newUpdater(t, 1)
	u.update(body)
	if len(u.updated) != 2 {
		t.Fatalf("imported %d node(s), want 2: %v", len(u.updated), u.skipped)
	}
	if len(u.skipped) != 0 {
		t.Errorf("comment lines were reported as unsupported nodes: %v", u.skipped)
	}
}

// ---------------------------------------------------------------------------
// base64 subscriptions
// ---------------------------------------------------------------------------

func TestReviewBase64SubscriptionForms(t *testing.T) {
	links := "trojan://pw@a.example.com:443#n1\nvless://uuid@b.example.com:443#n2\n"
	forms := map[string]string{
		"std padded":    base64.StdEncoding.EncodeToString([]byte(links)),
		"std raw":       base64.RawStdEncoding.EncodeToString([]byte(links)),
		"url padded":    base64.URLEncoding.EncodeToString([]byte(links)),
		"url raw":       base64.RawURLEncoding.EncodeToString([]byte(links)),
		"url no pad":    strings.TrimRight(base64.URLEncoding.EncodeToString([]byte(links)), "="),
		"std no pad":    strings.TrimRight(base64.StdEncoding.EncodeToString([]byte(links)), "="),
		"with newlines": base64.StdEncoding.EncodeToString([]byte(links)) + "\n",
	}
	for name, body := range forms {
		u := newUpdater(t, 1)
		u.update(body)
		if len(u.updated) != 2 {
			t.Errorf("%s: imported %d node(s), want 2 (skipped %v)", name, len(u.updated), u.skipped)
		}
	}
}

// ---------------------------------------------------------------------------
// a fake panel
// ---------------------------------------------------------------------------

type fakePanel struct {
	body   string
	header string

	mu   sync.Mutex
	hits map[string]int
}

func (f *fakePanel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ua := r.Header.Get("User-Agent")
	f.mu.Lock()
	if f.hits == nil {
		f.hits = map[string]int{}
	}
	f.hits[ua]++
	f.mu.Unlock()
	if f.header != "" {
		w.Header().Set("Subscription-UserInfo", f.header)
	}
	fmt.Fprint(w, f.body)
}

func (f *fakePanel) hitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

func newTestManager(t *testing.T) *db.Manager {
	t.Helper()
	mgr := db.NewManager(t.TempDir())
	if err := mgr.Load(); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func addTestGroup(t *testing.T, mgr *db.Manager, name, url string) int {
	t.Helper()
	g := &db.Group{ID: -1, FrontProxyID: -1, Name: name, URL: url}
	if err := mgr.AddGroup(g); err != nil {
		t.Fatal(err)
	}
	return g.ID
}

// ---------------------------------------------------------------------------
// the quota header
// ---------------------------------------------------------------------------

// The panel's quota header is the only warning a subscription ever gives about
// its traffic and its expiry. It has to reach the group.
func TestReviewQuotaHeaderReachesTheGroup(t *testing.T) {
	panel := &fakePanel{
		body:   "trojan://pw@a.example.com:443#n1\n",
		header: "upload=1024; download=2048; total=1073741824; expire=1735689600",
	}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	s := &setting.Setting{UserAgent: "test-ua/1.0"}

	res, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, func() int { return -1919 })
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	g := mgr.SnapshotGroup(gid)
	if g.Info == "" {
		t.Fatalf("the group's Info is empty — the quota header was dropped (Added=%v)", res.Added)
	}
	if !strings.Contains(g.Info, "total=1073741824") {
		t.Errorf("group Info = %q, want the Subscription-UserInfo header verbatim", g.Info)
	}
	if g.SubLastUpdate == 0 {
		t.Errorf("group SubLastUpdate was not set")
	}
}

// A panel that answers with nothing importable must leave the existing nodes
// alone, and must say so.
func TestReviewEmptyAnswerKeepsTheNodes(t *testing.T) {
	panel := &fakePanel{body: "trojan://pw@a.example.com:443#n1\n"}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	s := &setting.Setting{UserAgent: "test-ua/1.0"}

	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, func() int { return -1919 }); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	before := mgr.ProfilesWithOrder(gid)
	if len(before) != 1 {
		t.Fatalf("first update brought in %d node(s), want 1", len(before))
	}

	panel.body = "<html><body>login</body></html>"
	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, func() int { return -1919 }); err == nil {
		t.Errorf("the second update reported success on an unparseable body")
	}
	after := mgr.ProfilesWithOrder(gid)
	if len(after) != 1 {
		t.Errorf("after the bad answer the group holds %d node(s), want the 1 it had", len(after))
	}
}

// The merge path: the second update of an unchanged subscription must keep the
// profile ids (and with them the reader's latency measurements).
func TestReviewMergeKeepsProfileIDs(t *testing.T) {
	panel := &fakePanel{body: "trojan://pw@a.example.com:443#n1\nvless://uuid@b.example.com:443#n2\n"}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	s := &setting.Setting{UserAgent: "test-ua/1.0"}
	live := func() int { return -1919 }

	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, live); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	first := mgr.ProfilesWithOrder(gid)
	if len(first) != 2 {
		t.Fatalf("first update brought in %d node(s), want 2", len(first))
	}
	ids := []int{first[0].ID, first[1].ID}
	for _, p := range first {
		if err := mgr.UpdateProfile(p.ID, false, func(e *db.ProxyEntity) { e.Latency = 123 }); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, live); err != nil {
		t.Fatalf("second Update: %v", err)
	}
	second := mgr.ProfilesWithOrder(gid)
	if len(second) != 2 {
		t.Fatalf("second update left %d node(s), want 2", len(second))
	}
	for i, p := range second {
		if p.ID != ids[i] {
			t.Errorf("node %d changed id: %d -> %d (the measurement went with it)", i, ids[i], p.ID)
		}
		if p.Latency != 123 {
			t.Errorf("node %d lost its latency: %d", p.ID, p.Latency)
		}
	}
}

// ---------------------------------------------------------------------------
// the "clear on update" path
// ---------------------------------------------------------------------------

// With clearing on, the group is rebuilt from the subscription. The node the
// core is running must survive — and it must still be in the group's order,
// or it is invisible in the list while still running.
func TestReviewClearKeepsTheRunningNodeInTheOrder(t *testing.T) {
	panel := &fakePanel{body: "trojan://pw@a.example.com:443#n1\nvless://uuid@b.example.com:443#n2\n"}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	s := &setting.Setting{UserAgent: "test-ua/1.0", SubClear: true}
	live := func() int { return -1919 }

	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, live); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	first := mgr.ProfilesWithOrder(gid)
	if len(first) != 2 {
		t.Fatalf("first update brought in %d node(s), want 2", len(first))
	}
	running := first[0].ID

	// The reader switches to the first node while the walk runs.
	switched := running
	live = func() int { return switched }

	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, live); err != nil {
		t.Fatalf("second Update: %v", err)
	}
	g := mgr.SnapshotGroup(gid)
	kept := false
	for _, id := range g.Order {
		if id == running {
			kept = true
		}
	}
	if !kept {
		t.Errorf("the running node (id %d) is not in the group's order %v — it is running but invisible",
			running, g.Order)
	}
	if !mgr.HasProfile(running) {
		t.Errorf("the running node (id %d) was deleted by the update", running)
	}
}

// The same, but the node the reader switched to lives in ANOTHER group: it must
// not be pulled into this group's order, and the node this group was running
// must not vanish from it.
func TestReviewClearWithRunningNodeInAnotherGroup(t *testing.T) {
	panel := &fakePanel{body: "trojan://pw@a.example.com:443#n1\n"}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	other := addTestGroup(t, mgr, "other", "")
	s := &setting.Setting{UserAgent: "test-ua/1.0", SubClear: true}

	// one node in the other group
	otherBean := format.New("trojan")
	otherBean.Set("name", "other-node")
	otherBean.Set("addr", "other.example.com")
	otherBean.Set("port", 443)
	otherBean.Set("pass", "pw")
	otherEnt := &db.ProxyEntity{Type: "trojan", ID: -1, GID: other, Bean: otherBean}
	if err := mgr.AddProfile(otherEnt, other); err != nil {
		t.Fatal(err)
	}
	otherID := otherEnt.ID

	// one node in the subscription group, and it is the one the walk started on
	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, func() int { return -1919 }); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	first := mgr.ProfilesWithOrder(gid)
	if len(first) != 1 {
		t.Fatalf("the group holds %d node(s), want 1", len(first))
	}
	started := first[0].ID

	// The reader switches to the node of the OTHER group while the walk runs.
	live := func() int { return otherID }
	if _, err := Update(mgr, s, started, srv.URL, gid, func(string) {}, live); err != nil {
		t.Fatalf("second Update: %v", err)
	}
	g := mgr.SnapshotGroup(gid)
	for _, id := range g.Order {
		if id == otherID {
			t.Errorf("the other group's node (id %d) was pulled into this group's order %v", otherID, g.Order)
		}
	}
	if mgr.HasProfile(started) && !containsInt(g.Order, started) {
		t.Errorf("the node the walk started on (id %d) survived on disk but is not in the order %v "+
			"— it is invisible in the group", started, g.Order)
	}
}

// The same, but the reader switched to ANOTHER NODE OF THE SAME GROUP while
// the walk ran — the ordinary case. The node the walk started on is kept on
// disk (it is exempt), but it must also be kept in the order, or it is a
// leftover the reader can neither see nor delete.
func TestReviewClearAfterSwitchingInsideTheGroup(t *testing.T) {
	panel := &fakePanel{body: "trojan://pw@a.example.com:443#n1\nvless://uuid@b.example.com:443#n2\n"}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	s := &setting.Setting{UserAgent: "test-ua/1.0", SubClear: true}

	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, func() int { return -1919 }); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	first := mgr.ProfilesWithOrder(gid)
	if len(first) != 2 {
		t.Fatalf("the group holds %d node(s), want 2", len(first))
	}
	started := first[0].ID
	other := first[1].ID

	// the reader switches to the other node of the same group
	live := func() int { return other }
	if _, err := Update(mgr, s, started, srv.URL, gid, func(string) {}, live); err != nil {
		t.Fatalf("second Update: %v", err)
	}
	g := mgr.SnapshotGroup(gid)
	if mgr.HasProfile(started) && !containsInt(g.Order, started) {
		t.Errorf("the node the walk started on (id %d) survived on disk but is not in the order %v "+
			"— it is invisible in the group and the reader cannot delete it", started, g.Order)
	}
}

// ---------------------------------------------------------------------------
// the user-agent walk
// ---------------------------------------------------------------------------

// The settings field holds a LIST of client identifiers, and netx.UserAgents
// documents them as "the order they should be tried". The walk, however, keeps
// only the first three of them — so a reader who pastes a fifth identifier
// into the field never sees it used, and the panel that needs it answers
// "nothing importable".
func TestReviewOnlyThreeUserAgentsAreTried(t *testing.T) {
	panel := &fakePanel{body: "trojan://pw@a.example.com:443#n1\n"}
	srv := httptest.NewServer(panel)
	defer srv.Close()

	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", srv.URL)
	s := &setting.Setting{UserAgent: "ua-1/1.0\nua-2/1.0\nua-3/1.0\nua-4/1.0\nua-5/1.0"}
	if _, err := Update(mgr, s, -1919, srv.URL, gid, func(string) {}, func() int { return -1919 }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n := panel.hitCount(); n != 5 {
		t.Errorf("the walk asked %d identifier(s), want 5 — the list the reader configured is "+
			"never walked past its third entry", n)
	}
}

// ---------------------------------------------------------------------------
// the group files on disk
// ---------------------------------------------------------------------------

// The group a subscription writes to must be a real file: the reader's
// configuration is what survives a restart.
func TestReviewGroupFileIsWritten(t *testing.T) {
	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "sub", "https://example.com/sub")
	p := filepath.Join(mgr.BaseDir(), "groups", fmt.Sprintf("%d.json", gid))
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the group file was not written: %v", err)
	}
}

// ---------------------------------------------------------------------------
// the db manager
// ---------------------------------------------------------------------------

// The ids the manager hands out must be unique, and a freed id must not be
// handed out again while another profile still carries it.
func TestReviewDBProfileIDsAreUnique(t *testing.T) {
	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "g", "")
	seen := map[int]bool{}
	var ids []int
	for i := 0; i < 5; i++ {
		e := &db.ProxyEntity{Type: "trojan", ID: -1, GID: gid, Bean: format.New("trojan")}
		if err := mgr.AddProfile(e, gid); err != nil {
			t.Fatal(err)
		}
		if seen[e.ID] {
			t.Fatalf("the id %d was handed out twice", e.ID)
		}
		seen[e.ID] = true
		ids = append(ids, e.ID)
	}
	// free the middle id and ask for another profile
	if err := mgr.DeleteProfile(ids[2]); err != nil {
		t.Fatal(err)
	}
	e := &db.ProxyEntity{Type: "trojan", ID: -1, GID: gid, Bean: format.New("trojan")}
	if err := mgr.AddProfile(e, gid); err != nil {
		t.Fatal(err)
	}
	if seen[e.ID] {
		t.Errorf("the id %d was handed out twice", e.ID)
	}
	// and it must be on disk under that id
	p := filepath.Join(mgr.BaseDir(), "profiles", fmt.Sprintf("%d.json", e.ID))
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the profile file %s was not written: %v", p, err)
	}
}

// Deleting a group has to take its profiles with it: a profile left behind
// belongs to a group that is gone and is invisible in every list.
func TestReviewDBDeleteGroupTakesItsProfiles(t *testing.T) {
	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "g", "")
	other := addTestGroup(t, mgr, "other", "")
	for i := 0; i < 3; i++ {
		e := &db.ProxyEntity{Type: "trojan", ID: -1, GID: gid, Bean: format.New("trojan")}
		if err := mgr.AddProfile(e, gid); err != nil {
			t.Fatal(err)
		}
	}
	keep := &db.ProxyEntity{Type: "trojan", ID: -1, GID: other, Bean: format.New("trojan")}
	if err := mgr.AddProfile(keep, other); err != nil {
		t.Fatal(err)
	}

	if err := mgr.DeleteGroup(gid); err != nil {
		t.Fatal(err)
	}
	if len(mgr.ProfilesWithOrder(gid)) != 0 {
		t.Errorf("the deleted group still holds %d profile(s)", len(mgr.ProfilesWithOrder(gid)))
	}
	if !mgr.HasProfile(keep.ID) {
		t.Errorf("the other group's profile (id %d) was deleted with the group", keep.ID)
	}
	// and the files are gone
	entries, _ := os.ReadDir(filepath.Join(mgr.BaseDir(), "profiles"))
	for _, en := range entries {
		if en.Name() == fmt.Sprintf("%d.json", keep.ID) {
			continue
		}
		if len(en.Name()) > 5 && en.Name()[len(en.Name())-5:] == ".json" {
			t.Errorf("the profile file %s survived the group's deletion", en.Name())
		}
	}
}

// A deleted profile must not stay in the group's order: the order is what the
// list is drawn from, and a stale id there is an entry that renders nothing.
func TestReviewDBDeleteProfileClearsTheGroupOrder(t *testing.T) {
	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "g", "")
	var ids []int
	for i := 0; i < 3; i++ {
		e := &db.ProxyEntity{Type: "trojan", ID: -1, GID: gid, Bean: format.New("trojan")}
		if err := mgr.AddProfile(e, gid); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	// the order a subscription update leaves behind
	if err := mgr.UpdateGroup(gid, true, func(g *db.Group) { g.Order = ids }); err != nil {
		t.Fatal(err)
	}
	// a profile added by hand joins that order
	extra := &db.ProxyEntity{Type: "trojan", ID: -1, GID: gid, Bean: format.New("trojan")}
	if err := mgr.AddProfile(extra, gid); err != nil {
		t.Fatal(err)
	}
	g := mgr.SnapshotGroup(gid)
	if !containsInt(g.Order, extra.ID) {
		t.Errorf("the hand-added profile (id %d) is not in the group's order %v", extra.ID, g.Order)
	}
	// and deleting it takes it out of the order
	if err := mgr.DeleteProfile(extra.ID); err != nil {
		t.Fatal(err)
	}
	g = mgr.SnapshotGroup(gid)
	for _, id := range g.Order {
		if id == extra.ID {
			t.Errorf("the deleted profile (id %d) is still in the group's order %v", extra.ID, g.Order)
		}
	}
}

// Two writers at once must not be handed the same id.
func TestReviewDBConcurrentAddsDoNotCollide(t *testing.T) {
	mgr := newTestManager(t)
	gid := addTestGroup(t, mgr, "g", "")
	const n = 24
	ids := make(chan int, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			e := &db.ProxyEntity{Type: "trojan", ID: -1, GID: gid, Bean: format.New("trojan")}
			if err := mgr.AddProfile(e, gid); err != nil {
				errs <- err
				return
			}
			ids <- e.ID
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatalf("AddProfile: %v", err)
		case id := <-ids:
			for _, other := range mgr.AllProfiles() {
				if other.ID != id {
					continue
				}
			}
		}
	}
	seen := map[int]bool{}
	for _, p := range mgr.AllProfiles() {
		if seen[p.ID] {
			t.Errorf("the id %d is held by two profiles", p.ID)
		}
		seen[p.ID] = true
	}
	if len(mgr.AllProfiles()) != n {
		t.Errorf("the manager holds %d profile(s), want %d", len(mgr.AllProfiles()), n)
	}
}
