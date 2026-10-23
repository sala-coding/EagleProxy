// Package setting loads and saves the nekoray-compatible application settings.
//
// Layout (identical to nekoray):
//
//	<base>/groups/eagle.json            DataStore
//	<base>/routes_box/<routing>.json    Routing profile
package setting

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Sniffing modes (matches NekoGui::SniffingMode).
const (
	SniffingDisable        = 0
	SniffingForRouting     = 1
	SniffingForDestination = 2
)

// InboundAuth is the optional credential for the mixed inbound.
type InboundAuth struct {
	Username string `json:"user"`
	Password string `json:"pass"`
}

// NeedAuth reports whether both credentials are set.
func (a InboundAuth) NeedAuth() bool {
	return strings.TrimSpace(a.Username) != "" && strings.TrimSpace(a.Password) != ""
}

// ExtraCore maps external core ids to executable paths.
type ExtraCore struct {
	CoreMap string `json:"core_map"`
}

// Get returns the path for a core id.
func (e *ExtraCore) Get(id string) string {
	m := e.mapOf()
	if v, ok := m[id].(string); ok {
		return v
	}
	return ""
}

// Set stores a core path.
func (e *ExtraCore) Set(id, path string) {
	m := e.mapOf()
	m[id] = path
	e.store(m)
}

// Delete removes a core path.
func (e *ExtraCore) Delete(id string) {
	m := e.mapOf()
	delete(m, id)
	e.store(m)
}

func (e *ExtraCore) mapOf() map[string]interface{} {
	m := map[string]interface{}{}
	if e.CoreMap != "" {
		_ = json.Unmarshal([]byte(e.CoreMap), &m)
	}
	return m
}

func (e *ExtraCore) store(m map[string]interface{}) {
	data, _ := json.Marshal(m)
	e.CoreMap = string(data)
}

// Routing mirrors nekoray's Routing profile.
type Routing struct {
	DirectIP     string `json:"direct_ip,omitempty"`
	DirectDomain string `json:"direct_domain,omitempty"`
	ProxyIP      string `json:"proxy_ip,omitempty"`
	ProxyDomain  string `json:"proxy_domain,omitempty"`
	BlockIP      string `json:"block_ip,omitempty"`
	BlockDomain  string `json:"block_domain,omitempty"`
	DefOutbound  string `json:"def_outbound,omitempty"`
	Custom       string `json:"custom,omitempty"`

	RemoteDNS              string `json:"remote_dns,omitempty"`
	RemoteDNSStrategy      string `json:"remote_dns_strategy,omitempty"`
	DirectDNS              string `json:"direct_dns,omitempty"`
	DirectDNSStrategy      string `json:"direct_dns_strategy,omitempty"`
	DomainStrategy         string `json:"domain_strategy,omitempty"`
	OutboundDomainStrategy string `json:"outbound_domain_strategy,omitempty"`

	DNSRouting   bool   `json:"dns_routing"`
	SniffingMode int    `json:"sniffing_mode"`
	UseDNSObject bool   `json:"use_dns_object"`
	DNSObject    string `json:"dns_object,omitempty"`
	DNSFinalOut  string `json:"dns_final_out,omitempty"`

	// DNSSmartSelect records that the two resolvers above were picked by measurement rather
	// than by hand (see App.TestDNS). The pick itself is stored in them, so this says
	// nothing about what the config does — it is the card's own note to itself, that the
	// choice was made for the reader and can be made again.
	DNSSmartSelect bool `json:"dns_smart_select"`

	// DNSSmartMinutes is how often that is done, in minutes. Zero means the app's own
	// default (see SMART_PICK_MINUTES in the frontend) — which is also what a profile
	// written before this field existed has, and it has to keep meaning "the usual".
	DNSSmartMinutes int `json:"dns_smart_minutes"`

	// DNSSmartSwitch is the other half of the same rounds, and the opposite policy: the
	// resolver in the field is the reader's own and is kept, however fast something else
	// measured — until it goes slow, at which point it is replaced. Where DNSSmartSelect
	// chases the fastest every round, this one only refuses to sit on a bad one. The two can
	// be on together, and then the first decides: a round that has already taken the fastest
	// has nothing left for this to react to.
	DNSSmartSwitch bool `json:"dns_smart_switch"`
}

// DefaultDirectDNS is the domestic resolver a routing profile starts with. It is also
// the one the config builder stands in when the profile's own choice cannot be dialled
// from this network: a bare IPv6 literal on a machine with no IPv6 route would
// otherwise be written into the config as a resolver that never answers, while the
// settings card goes on showing it looking perfectly valid (see buildDNS).
const DefaultDirectDNS = "https://doh.pub/dns-query"

func defaultRouting() *Routing {
	return &Routing{
		DefOutbound: "proxy",
		Custom:      `{"rules": []}`,
		RemoteDNS:   "https://dns.google/dns-query",
		DirectDNS:   DefaultDirectDNS,
		DNSRouting:  true,
		DNSFinalOut: "proxy",
		// nekoray's struct default is "AsIs", which sing-box does not accept.
		// sing-box only knows "", ipv4_only, ipv6_only, prefer_ipv4, prefer_ipv6.
		DomainStrategy:         "",
		OutboundDomainStrategy: "",
		SniffingMode:           SniffingForRouting,
	}
}

// singBoxStrategies lists the domain strategy values sing-box accepts.
var singBoxStrategies = map[string]bool{
	"":            true,
	"ipv4_only":   true,
	"ipv6_only":   true,
	"prefer_ipv4": true,
	"prefer_ipv6": true,
}

// IsValidStrategy reports whether sing-box accepts the given domain strategy.
func IsValidStrategy(v string) bool { return singBoxStrategies[v] }

// Normalize coerces values sing-box rejects into their defaults, mirroring the
// check nekoray does when loading a routing (NekoGui.cpp).
func (r *Routing) Normalize() {
	if !singBoxStrategies[r.DomainStrategy] {
		r.DomainStrategy = ""
	}
	if !singBoxStrategies[r.OutboundDomainStrategy] {
		r.OutboundDomainStrategy = ""
	}
	if !singBoxStrategies[r.RemoteDNSStrategy] {
		r.RemoteDNSStrategy = ""
	}
	if !singBoxStrategies[r.DirectDNSStrategy] {
		r.DirectDNSStrategy = ""
	}
}

// DisplayRouting renders a short summary like nekoray's Routing::DisplayRouting.
func (r *Routing) DisplayRouting() string {
	return strings.Join([]string{
		"[Default Outbound] " + r.DefOutbound,
		"[DNS] " + map[bool]string{true: "DNS Object", false: "Simple DNS"}[r.UseDNSObject],
	}, "\n")
}

// Setting is the full nekoray DataStore.
type Setting struct {
	ExtraCore   *ExtraCore   `json:"extraCore,omitempty"`
	InboundAuth *InboundAuth `json:"inbound_auth,omitempty"`

	UserAgent           string `json:"user_agent2,omitempty"`
	TestLatencyURL      string `json:"test_url,omitempty"`
	TestDownloadURL     string `json:"test_url_dl,omitempty"`
	TestDownloadTimeout int    `json:"test_dl_timeout"`
	CurrentGroup        int    `json:"current_group"`
	InboundAddress      string `json:"inbound_address,omitempty"`
	InboundSocksPort    int    `json:"inbound_socks_port"`
	LogLevel            string `json:"log_level,omitempty"`
	MuxProtocol         string `json:"mux_protocol,omitempty"`
	MuxConcurrency      int    `json:"mux_concurrency"`
	MuxPadding          bool   `json:"mux_padding"`
	MuxDefaultOn        bool   `json:"mux_default_on"`
	TrafficLoopInterval int    `json:"traffic_loop_interval"`
	TestConcurrent      int    `json:"test_concurrent"`
	Theme               string `json:"theme,omitempty"`
	// ThemeSchema stamps the file with the layout of palettes the theme number
	// indexes: '2' is the trimmed sheet (nine to a row plus the custom entry).
	// The front end lifts old numbers to new places only when the stamp is
	// older, so the same number can never mean two palettes in one file.
	ThemeSchema       string `json:"theme_schema,omitempty"`
	CustomInbound     string `json:"custom_inbound,omitempty"`
	CustomRouteGlobal string `json:"custom_route,omitempty"`
	SubUseProxy       bool   `json:"sub_use_proxy"`
	RememberID        int    `json:"remember_id"`
	RememberEnable    bool   `json:"remember_enable"`
	Language          int    `json:"language"`
	// RememberSpmode is the mode the app was last left in, as a list of the two it can
	// be in: "system_proxy" and/or "vpn". Written on every successful switch, it is what
	// the next launch reads back (see RememberSpmodeOn and applyRememberedSpmode).
	//
	// The list rather than two booleans because that is the shape nekoray writes into
	// the same file, so a settings file that has passed through either program is read
	// the same way here.
	RememberSpmode []string `json:"spmode2,omitempty"`
	// RememberSpmodeOn puts that mode back on at start-up. Off by default: whether a
	// proxy comes up before the reader asks for it is not a thing to decide on their
	// behalf — and with TUN, "asking" means an administrator prompt at every launch.
	RememberSpmodeOn      bool   `json:"remember_spmode"`
	SkipCert              bool   `json:"skip_cert"`
	HotkeyMainwindow      string `json:"hk_mw,omitempty"`
	HotkeyGroup           string `json:"hk_group,omitempty"`
	HotkeyRoute           string `json:"hk_route,omitempty"`
	HotkeySystemProxyMenu string `json:"hk_spmenu,omitempty"`
	// HotkeyToggleNode starts the last node or stops the running one — the tray's two
	// items behind a single key, since a reader who wants a node up does not also want
	// to remember which of the two presses that is.
	HotkeyToggleNode string `json:"hk_toggle_node,omitempty"`
	// HotkeyEgress steps the outbound mode on to the next one and puts it in force.
	HotkeyEgress string `json:"hk_egress,omitempty"`
	// HotkeyToggleTun is the rail's shield button behind a key — the tray's other switch.
	// The slowest of the set: turning TUN on asks for the administrator token the first
	// time and stops and starts the core either way (see SetVPNMode).
	HotkeyToggleTun string `json:"hk_toggle_tun,omitempty"`
	// HotkeyNextNode starts the next node of the current group: the tray's "not this one",
	// and the third of the cycling family the keys already carry — groups, routing
	// profiles, and now the nodes inside one.
	HotkeyNextNode string `json:"hk_next_node,omitempty"`
	// HotkeyRestartNode stops the running node and starts it again, for a connection that
	// has gone stale. Not the same as HotkeyNextNode: this one keeps the node.
	HotkeyRestartNode string `json:"hk_restart_node,omitempty"`
	// HotkeyConnections opens the connections window, and puts it away again on a second
	// press. The window comes up with it: whether a panel is open belongs to the window,
	// so the backend only says which one was asked for (see emitHotkeyToWindow).
	HotkeyConnections string `json:"hk_connections,omitempty"`
	// HotkeyLogs the same for the log panel.
	HotkeyLogs string `json:"hk_logs,omitempty"`
	// HotkeyDataDir opens the data directory in the file manager — where the routing
	// profiles, the rule sets and the logs are, which is the folder a reader goes looking
	// for by hand.
	HotkeyDataDir string `json:"hk_datadir,omitempty"`
	FakeDNS       bool   `json:"fakedns"`
	ActiveRouting string `json:"active_routing,omitempty"`
	// EgressMode is where traffic goes, as the status bar's switch means it: "rule"
	// (the routing profile decides — the default), "direct" (everything out unproxied,
	// the profile's rules left out), "global" (everything through the proxy, also with
	// the rules left out) or "block" (nothing goes anywhere). Empty reads as "rule",
	// which is what a file written by nekoray, or by an older build of this one,
	// carries.
	EgressMode           string `json:"egress_mode,omitempty"`
	MWSize               string `json:"mw_size,omitempty"`
	ConnectionStatistics bool   `json:"conn_stat"`
	VPNImplementation    int    `json:"vpn_impl"`
	VPNMTU               int    `json:"vpn_mtu"`
	VPNIPv6              bool   `json:"vpn_ipv6"`
	// IPv6AutoDetect says who answers the address-family question: the measurement of
	// this machine's network (netx.HasIPv6), or the reader.
	//
	// On — the default, and what a settings file written before this key existed gets,
	// since defaultSetting sets it and Load overwrites only what the file carries — the
	// config follows what the network can actually carry: no IPv6 anywhere it cannot be
	// had, both families where it can. Off, nothing is measured and nothing is
	// substituted in: the TUN switch, the routing profile's resolvers and its strategy
	// fields go into the config exactly as they stand, which is the choice for a reader
	// whose network the measurement cannot see — a fixed tunnel, a lab, a machine whose
	// IPv6 comes and goes — and for one who would rather decide than be measured.
	IPv6AutoDetect    bool   `json:"ipv6_auto"`
	VPNHideConsole    bool   `json:"vpn_hide_console"`
	VPNStrictRoute    bool   `json:"vpn_strict_route"`
	VPNRuleProcess    string `json:"vpn_bypass_process,omitempty"`
	VPNRuleCIDR       string `json:"vpn_bypass_cidr,omitempty"`
	VPNRuleWhite      bool   `json:"vpn_rule_white"`
	CheckIncludePre   bool   `json:"check_include_pre"`
	SystemProxyFormat string `json:"sp_format,omitempty"`
	// SystemProxyBypass is the semicolon-separated list Windows is told never to send
	// through the proxy. Empty means the built-in default — loopback and single-label
	// names — which is what every build before this one wrote. It is how a reader keeps
	// their own network answering directly (a router's page, a NAS, an intranet host)
	// while everything else goes through the core.
	SystemProxyBypass string `json:"sp_bypass,omitempty"`
	// SystemProxyPAC is the address of a proxy auto-config script. When it is set, the
	// system proxy is pointed at that script instead of at the local inbound: the script
	// decides per request, so the fixed server, the format above and the bypass list all
	// stand down — Windows holds one or the other. Empty, the default, means the local
	// inbound as before.
	SystemProxyPAC string `json:"sp_pac,omitempty"`
	SubClear       bool   `json:"sub_clear"`
	SubInsecure    bool   `json:"sub_insecure"`
	// SubAutoUpdate is the global fallback for the background subscription timer,
	// in HOURS. A group with an interval of its own (sub_interval, minutes) uses
	// that instead, and a group with neither is never fetched on a timer. Zero or
	// negative is "off", which is the default — the manual act this replaces was
	// opt-in by press.
	//
	// There used to be a second, global timer beside it (auto_sub_update_min,
	// minutes) that re-fetched every subscribed group on its own clock. Two
	// timers doing one job overlapped in the one way that matters: the minutes one
	// refreshed every group's lastup on every tick, so the hours one — and with it
	// the per-group interval — could never come due, and the group's own interval
	// was a field nobody read. What remains is this fallback plus the group's own
	// interval; a group is left out of the rotation by having neither (see
	// subUpdateLoop in app.go).
	SubAutoUpdate         int      `json:"sub_auto_update"`
	LogIgnore             []string `json:"log_ignore,omitempty"`
	StartMinimal          bool     `json:"start_minimal"`
	MaxLogLine            int      `json:"max_log_line"`
	SplitterState         string   `json:"splitter_state,omitempty"`
	UtlsFingerprint       string   `json:"utlsFingerprint,omitempty"`
	CoreBoxClashAPI       int      `json:"core_box_clash_api"`
	CoreBoxClashAPISecret string   `json:"core_box_clash_api_secret,omitempty"`
	CoreBoxUnderlyingDNS  string   `json:"core_box_underlying_dns,omitempty"`
	// CoreBoxCacheFile keeps the core's own cache in a file in the data directory. The
	// part that matters here is store_fakeip: the domain ↔ fake address mapping then
	// survives a restart of the core, which is the common case — every node switch and
	// every config change restarts it. Without the file the mapping is made again from
	// scratch, and a fake address the system or an application has already cached
	// belongs to a mapping that no longer exists: that connection is routed by the
	// address, which loses the domain half of the split, or it fails outright, until the
	// cache expires.
	//
	// On by default. It is what the mapping wants, and the file is one more thing in the
	// data directory beside the routing profiles and the rule sets.
	CoreBoxCacheFile bool `json:"core_box_cache_file"`
	VPNInternalTun   bool `json:"vpn_internal_tun"`
	// PendingTun is the request that survives an elevated relaunch: internal tun
	// needs administrator rights, so the app restarts itself through a UAC prompt
	// and the new instance reads this to pick up where the old one left off.
	PendingTun        bool `json:"pending_tun"`
	PendingTunProfile int  `json:"pending_tun_profile"`
	// KeepTunFrontOnExit is the switch the fast-TUN work rides on: when the app
	// exits, the holder of the adapter is NOT torn down — the process (or, where a
	// scheduled-task server owns it, the server and its child together) lives on,
	// and the next launch adopts it instead of paying the PnP cost of building a
	// new adapter out of a dead one's remains. Every stop of the adapter pays a
	// Windows-time cost measured in seconds (see tunfast probe), so a launch that
	// can reuse what is already there is what reads as "instant".
	//
	// Default on: the surviving process is bounded, known (its pid is written) and
	// taken down by the very first TUN-off click.
	KeepTunFrontOnExit bool `json:"keep_tun_front"`

	routing *Routing
	baseDir string

	spmodeVPN         bool
	spmodeSystemProxy bool
}

// runtimeModeMu guards the two runtime mode flags above. They are written from
// the UI/hotkey goroutines and read on the poll loops (traffic, exit IP, tray);
// the fields themselves stay plain bools so the struct copies in
// Replace/With* keep vetting clean — one package-level mutex is enough, the
// process holds a single Setting anyway.
var runtimeModeMu sync.Mutex

// routingMu protects the live routing pointer and its selected name. Routing is
// replaced as a whole when the settings window saves it, while the DNS forwarder
// and config builder read it from background goroutines.
var routingMu sync.RWMutex

// VPNMode reports whether TUN/VPN mode is active (runtime state).
func (s *Setting) VPNMode() bool {
	runtimeModeMu.Lock()
	defer runtimeModeMu.Unlock()
	return s.spmodeVPN
}

// SetVPNMode updates the runtime VPN mode flag.
func (s *Setting) SetVPNMode(v bool) {
	runtimeModeMu.Lock()
	s.spmodeVPN = v
	runtimeModeMu.Unlock()
}

// SPModeSystemProxy reports whether system proxy mode is active (runtime state).
func (s *Setting) SPModeSystemProxy() bool {
	runtimeModeMu.Lock()
	defer runtimeModeMu.Unlock()
	return s.spmodeSystemProxy
}

// SetSPModeSystemProxy updates the runtime system proxy mode flag.
func (s *Setting) SetSPModeSystemProxy(v bool) {
	runtimeModeMu.Lock()
	s.spmodeSystemProxy = v
	runtimeModeMu.Unlock()
}

// InheritRuntime copies the runtime-only flags from a previously loaded
// Setting. They are intentionally not persisted, so a plain reload would
// otherwise reset the system-proxy and TUN toggles while the core is still
// running against them.
func (s *Setting) InheritRuntime(prev *Setting) {
	if prev == nil {
		return
	}
	runtimeModeMu.Lock()
	defer runtimeModeMu.Unlock()
	s.spmodeVPN = prev.spmodeVPN
	s.spmodeSystemProxy = prev.spmodeSystemProxy
}

// DefaultUserAgents is the built-in list of client identifiers a subscription is
// fetched with, in the order they are tried. It is what the settings field starts
// out holding, and what a field left empty falls back to.
//
// The order is not a preference, it is a cost. Every identifier tried before the
// one that works is a request the reader paid for, and two different things can go
// wrong with the ones in front: a panel can refuse the identifier outright — 403,
// so the attempt fails visibly — or accept it and simply leave the quota header
// out, which is the quieter failure the walk exists for.
//
// mihomo leads because of a third failure, met on a real panel: it accepted an older
// Clash spelling, sent the quota header, and quietly sent a *shorter* node list — the
// hysteria entries only, with a whole family of AnyTLS nodes left out. Nothing in that
// answer says it is partial, so the walk stopped at it and the subscription stayed ten
// nodes short for as long as it was used. The same panel answers in full to mihomo,
// which is why it is in front now. v2rayN follows, then the Clash spellings — a panel
// that refuses those is the reason they are not first: putting them there cost four
// refused requests on every update of such a subscription.
func DefaultUserAgents() []string {
	return []string{
		"mihomo/1.19.0",
		"v2rayN/6.23",
		"ClashMeta/1.18.0",
		"Clash/1.18.0",
		"clash-verge/v1.5.11",
		"Shadowrocket/2.2.20",
		"sing-box/1.10.0",
		"Eagle/PC/2.0 (Prefer ClashMeta Format)",
		"Surge/5.8.0",
		"Stash/2.5.0",
		"Quantumult%20X/1.0.30",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
	}
}

func defaultSetting(baseDir string) *Setting {
	// The user agent is prefilled with the built-in list, one identifier per line,
	// so the settings field shows what will actually be tried instead of opening
	// blank and looking unset.
	return &Setting{
		ExtraCore:           &ExtraCore{},
		InboundAuth:         &InboundAuth{},
		UserAgent:           strings.Join(DefaultUserAgents(), "\n"),
		TestLatencyURL:      "http://cp.cloudflare.com/",
		TestDownloadURL:     "http://cachefly.cachefly.net/10mb.test",
		TestDownloadTimeout: 30,
		CurrentGroup:        0,
		InboundAddress:      "127.0.0.1",
		InboundSocksPort:    2080,
		LogLevel:            "info",
		MuxProtocol:         "h2mux",
		MuxConcurrency:      8,
		TrafficLoopInterval: 1000,
		TestConcurrent:      5,
		Theme:               "0",
		CustomInbound:       `{"inbounds": []}`,
		CustomRouteGlobal:   `{"rules": []}`,
		RememberID:          -1919,
		ActiveRouting:       "Default",
		VPNMTU:              9000,
		VPNHideConsole:      true,
		// Detection is on for a fresh install and for a file written before the key
		// existed alike: Load starts from this value and replaces it only if the file
		// says something else, so "absent" keeps meaning the behaviour that shipped.
		IPv6AutoDetect: true,
		// Tun2Socket is the only TUN implementation now; the field stays so an older
		// settings file still parses, but nothing reads it any more.
		VPNInternalTun: false,
		// -1919 is core.StoppedID; the setting package cannot import core, so the
		// sentinel is spelled out here.
		PendingTunProfile:  -1919,
		KeepTunFrontOnExit: true,
		SubAutoUpdate:      -30,
		MaxLogLine:         200,
		CoreBoxClashAPI:    19090,
		CoreBoxCacheFile:   true,
		baseDir:            baseDir,
	}
}

// BaseDir returns the config root.
func (s *Setting) BaseDir() string { return s.baseDir }

// dataStoreName is the DataStore's file name under groups/. It was named after the
// program when that was NekoBox and is eagle.json now; the old name is still read
// once on first launch so an existing installation keeps its subscriptions, routes
// and settings rather than coming up empty (see migrateDataStore).
const dataStoreName = "eagle.json"

// legacyDataStoreName is what Load used to open, kept as the source of the one-time
// migration. Once eagle.json exists it is never consulted again, which is what makes
// the migration happen once rather than on every launch.
const legacyDataStoreName = "nekobox.json"

func (s *Setting) dataStorePath() string {
	return filepath.Join(s.baseDir, "groups", dataStoreName)
}

// migrateDataStore copies the old nekobox.json to eagle.json when the new file is not
// there yet and the old one is. It writes the new file and leaves the old one alone:
// a reader who keeps a copy of the old build, or who rolls the config back, still has
// a file that build can open, and a half-written migration costs nothing either way —
// the copy is a whole-file write and the next launch simply retries it.
func (s *Setting) migrateDataStore() {
	path := s.dataStorePath()
	if _, err := os.Stat(path); err == nil {
		return // the new file is already there: nothing to carry over
	}
	legacy := filepath.Join(s.baseDir, "groups", legacyDataStoreName)
	data, err := os.ReadFile(legacy)
	if err != nil {
		return // no old file either: a genuinely fresh install
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = writeFileAtomic(path, data)
}

// Load reads settings from baseDir, falling back to defaults.
func Load(baseDir string) *Setting {
	s := defaultSetting(baseDir)
	s.migrateDataStore()
	if data, err := os.ReadFile(s.dataStorePath()); err == nil {
		if json.Unmarshal(data, s) != nil {
			// A store that will not parse answers as defaults — the one read of
			// this file that has no way to ask the reader first. Name the broken
			// file on the way past it: since the writes went atomic, a file this
			// side of the change is half-written by a kill mid-save, and the
			// bytes beside it are the reader's only copy of what was lost.
			// Leaving it in place unnamed is how a settings reset became a
			// mystery instead of a sentence.
			//
			// The reset runs whether or not the naming succeeds: the rename
			// fails for the same reasons any file operation does (the file
			// held open, permissions), and gating the reset on it answered that
			// failure with the exact state the comment above exists to avoid —
			// a half-parsed struct served as the live setting, some fields the
			// corrupt file's and some the defaults', with nothing said. A reset
			// with the broken file still beside it is retried (and renamed) on
			// the next Load; a half-read struct is read as truth forever.
			corrupt := s.dataStorePath() + ".corrupt"
			if err := os.Rename(s.dataStorePath(), corrupt); err != nil {
				// Left where it is; the bytes are already quoted above as the
				// reader's only copy — removing them was never the point.
			}
			*s = *defaultSetting(baseDir)
		}
	}
	if s.InboundAuth == nil {
		s.InboundAuth = &InboundAuth{}
	}
	if s.ExtraCore == nil {
		s.ExtraCore = &ExtraCore{}
	}
	s.routing = loadRouting(baseDir, s.ActiveRouting)
	s.ensureRoutingFile()
	return s
}

// ensureRoutingFile writes the active routing back to disk when it is missing,
// so the routing dialog is never empty (nekoray always ships a Default routing).
func (s *Setting) ensureRoutingFile() {
	name := s.ActiveRouting
	if name == "" {
		name = "Default"
		s.ActiveRouting = name
	}
	path := filepath.Join(s.RoutingDir(), name+".json")
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.MkdirAll(s.RoutingDir(), 0o755); err != nil {
		return
	}
	_ = s.SaveRouting(name, s.routing)
}

// saveMu serialises the eagle.json writes: every save marshals the whole store,
// and two saves still in flight would write their wholes past each other — the
// later write overwriting the fresher one's fields.
var saveMu sync.Mutex

// Save writes the DataStore to groups/eagle.json. The whole store is what goes
// out, so the busy file writers (the settings save, the mode flags) never
// interleave: each Save's state is what lands on the file.
func (s *Setting) Save() error {
	saveMu.Lock()
	defer saveMu.Unlock()
	routingMu.RLock()
	defer routingMu.RUnlock()
	path := s.dataStorePath()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic puts the bytes in a sibling temp file and renames it over the
// target. The direct write it replaces truncates first: a kill inside the write
// leaves half a file, and Load reads a half-written file by swallowing the
// Unmarshal error and answering the whole store with defaults — every setting,
// every port, the remembered node, all reset with nothing in the log to say so.
// A rename is the one Windows leaves the old file intact through.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

// Routing returns the active routing profile.
func (s *Setting) Routing() *Routing {
	routingMu.RLock()
	defer routingMu.RUnlock()
	return s.routing
}

// UnderlyingDNS returns the DNS address used as the final resolver.
func (s *Setting) UnderlyingDNS() string {
	if s.CoreBoxUnderlyingDNS == "" {
		return "local"
	}
	return s.CoreBoxUnderlyingDNS
}

// RoutingDir returns the directory holding routing profiles.
func (s *Setting) RoutingDir() string { return filepath.Join(s.baseDir, "routes_box") }

// RuleSetDir returns the directory holding rule sets kept on disk. A file here wins
// over the remote copy of the same tag (see configbuilder's addRuleSet): the
// databases these stand in for are served from raw.githubusercontent.com, which is
// unreachable on many networks, and a rule set that fails to download is not an error
// the core reports — the rule simply never matches.
func (s *Setting) RuleSetDir() string { return filepath.Join(s.baseDir, "rule-set") }

// ListRoutings returns the available routing profile names.
func (s *Setting) ListRoutings() []string {
	// never return a nil slice: it marshals to JSON null and the frontend
	// would then call .map() on null.
	out := []string{}
	entries, err := os.ReadDir(s.RoutingDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".json") {
			out = append(out, name[:len(name)-len(".json")])
		}
	}
	sort.Strings(out)
	return out
}

// Replace returns a copy of s with the exported values of in merged in. The
// receiver is left untouched so goroutines reading the current settings never
// observe a half updated struct.
//
// The merge is field-wise rather than a JSON round trip, and it has to be: a
// round trip marshals with `omitempty`, which drops every empty value from the
// JSON, and a key that is missing from the JSON leaves the old value standing.
// So every setting the reader can empty — a hotkey binding, the PAC address,
// the bypass list, the user-agent list, the ignored-log list — came back with
// its old value, silently thrown away.
//
// The two pointer fields are given their own objects as well. The copy shares
// them with the settings it came from, and decoding over them writes straight
// into the live object — the previous snapshot, which is supposed to stay
// frozen for the goroutines still reading it.
func (s *Setting) Replace(in *Setting) (*Setting, error) {
	if in == nil {
		return nil, errors.New("no settings")
	}
	next := s.Clone() // keeps baseDir, the loaded routing and the runtime spmode flags
	mergeExported(next, in)
	next.routing = loadRouting(next.baseDir, next.ActiveRouting)
	next.ensureRoutingFile()
	return next, nil
}

// mergeExported copies every exported field of src onto dst.
//
// Slices are copied by content and the two pointer fields by object, so that
// nothing in dst shares memory with src: the settings that Replace hands back
// are the live ones afterwards, and they must not be able to reach back into
// the object they were merged from.
func mergeExported(dst, src *Setting) {
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	t := dv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported: baseDir and the loaded routing stay as they are
		}
		switch f.Type {
		case reflect.TypeOf((*InboundAuth)(nil)):
			dv.Field(i).Set(reflect.ValueOf(cloneInboundAuth(src.InboundAuth)))
			continue
		case reflect.TypeOf((*ExtraCore)(nil)):
			dv.Field(i).Set(reflect.ValueOf(cloneExtraCore(src.ExtraCore)))
			continue
		}
		if f.Type.Kind() == reflect.Slice {
			s := sv.Field(i)
			if s.IsNil() {
				dv.Field(i).Set(reflect.Zero(f.Type))
				continue
			}
			c := reflect.MakeSlice(f.Type, s.Len(), s.Len())
			reflect.Copy(c, s)
			dv.Field(i).Set(c)
			continue
		}
		dv.Field(i).Set(sv.Field(i))
	}
}

// cloneInboundAuth returns a copy of a, or nil.
func cloneInboundAuth(a *InboundAuth) *InboundAuth {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}

// cloneExtraCore returns a copy of e, or nil.
func cloneExtraCore(e *ExtraCore) *ExtraCore {
	if e == nil {
		return nil
	}
	c := *e
	return &c
}

// Clone returns a copy of s.
//
// The copy is taken under the runtime-mode lock, because the two runtime flags live
// in the struct and are written from the tray, the hotkeys and the window while
// other goroutines copy the whole thing. A plain `next := *s` read them without the
// lock, which is a data race on two bytes — and the copies are what the settings
// are replaced by, so the race was on the path every save went through.
func (s *Setting) Clone() *Setting {
	runtimeModeMu.Lock()
	defer runtimeModeMu.Unlock()
	routingMu.RLock()
	defer routingMu.RUnlock()
	next := *s
	return &next
}

// WithRememberID returns a copy of s with the remembered profile id replaced.
func (s *Setting) WithRememberID(id int) *Setting {
	next := s.Clone()
	next.RememberID = id
	return next
}

// WithEgressMode returns a copy of s with the outbound mode replaced.
//
// A copy rather than a field write, for the same reason as WithRememberID: this value is
// edited from the tray, a hotkey and the window at once, and a write into a pointer another
// goroutine has already replaced is an update nobody sees (see App.setEgress).
func (s *Setting) WithEgressMode(mode string) *Setting {
	next := s.Clone()
	next.EgressMode = mode
	return next
}

// WithRememberSpmode returns a copy of s with the remembered mode list replaced.
func (s *Setting) WithRememberSpmode(modes []string) *Setting {
	next := s.Clone()
	next.RememberSpmode = modes
	return next
}

// WithActiveRouting returns a copy of s with name as the active routing.
func (s *Setting) WithActiveRouting(name string) (*Setting, error) {
	r, err := LoadRouting(s.baseDir, name)
	if err != nil {
		return nil, err
	}
	next := s.Clone()
	next.routing = r
	next.ActiveRouting = name
	return next, nil
}

// SetActiveRouting switches the active routing profile and persists the choice.
func (s *Setting) SetActiveRouting(name string) error {
	r, err := LoadRouting(s.baseDir, name)
	if err != nil {
		return err
	}
	routingMu.Lock()
	s.routing = r
	s.ActiveRouting = name
	routingMu.Unlock()
	return s.Save()
}

// SaveRouting persists a routing profile by name.
//
// The profile the app runs on is the in-memory copy (Setting.Routing), not the
// file: the forwarder (App.syncDNSStub) and the config builder both read it.
// A save that left the in-memory copy alone would keep the app running the
// previous rules while the file — and the settings card, which reads the file —
// showed the new ones.
func (s *Setting) SaveRouting(name string, r *Routing) error {
	if err := validRoutingName(name); err != nil {
		return err
	}
	// Refused here rather than left to the core: a resolver whose scheme the
	// core does not know is a config it will not start on — and the one shape
	// that reaches it is worse than an error, because the forwarder rejects the
	// address first (dnsx.Parse) and the builder falls back to writing the
	// reader's own string verbatim (splitDNSAddress). `h2` is the case in
	// point: offered by this app's own type list, unknown to sing-box.
	if err := validateDNSAddress("remote_dns", r.RemoteDNS); err != nil {
		return err
	}
	if err := validateDNSAddress("direct_dns", r.DirectDNS); err != nil {
		return err
	}
	if err := os.MkdirAll(s.RoutingDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	// Atomic like the settings store: a kill inside the write would leave a
	// half-written profile, and loadRouting falls back to the default routing
	// for one it cannot read — silently replacing the reader's own profile.
	if err := writeFileAtomic(filepath.Join(s.RoutingDir(), name+".json"), data); err != nil {
		return err
	}
	routingMu.Lock()
	if s.isActiveRoutingLocked(name) {
		s.routing = r
	}
	routingMu.Unlock()
	return nil
}

// dnsServerSchemes are the DNS server types sing-box itself reads in a server
// object. `h2` is deliberately absent: it is xray's spelling, and a profile
// carrying it is a core that refuses to start.
var dnsServerSchemes = map[string]bool{
	"https": true, "h3": true, "tls": true, "quic": true, "udp": true, "tcp": true,
}

// validateDNSAddress rejects a resolver the core cannot read. Empty, "local",
// "fakeip" and a bare host/IP are all legitimate: the first three name no
// transport, and a bare address is plain UDP by construction (splitDNSAddress).
func validateDNSAddress(field, addr string) error {
	s := strings.TrimSpace(addr)
	if s == "" || strings.EqualFold(s, "local") || strings.EqualFold(s, "fakeip") {
		return nil
	}
	i := strings.Index(s, "://")
	if i <= 0 {
		return nil // bare host or IP: plain UDP
	}
	scheme := strings.ToLower(s[:i])
	if dnsServerSchemes[scheme] {
		return nil
	}
	return fmt.Errorf("%s: 不支持的协议 %q（核心只认 https/h3/tls/quic/udp/tcp，或直接写地址当成 UDP）", field, scheme)
}

// isActiveRouting reports whether name is the profile the app is running.
func (s *Setting) isActiveRouting(name string) bool {
	routingMu.RLock()
	defer routingMu.RUnlock()
	return s.isActiveRoutingLocked(name)
}

func (s *Setting) isActiveRoutingLocked(name string) bool {
	active := s.ActiveRouting
	if active == "" {
		active = "Default"
	}
	return name == active
}

// validRoutingName rejects a profile name that would not stay inside the
// routing directory. The name comes from the front end and is joined into a
// path, so a name carrying a separator or a ".." would write the profile
// somewhere else on disk.
func validRoutingName(name string) error {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return errors.New("bad routing name: " + name)
	}
	return nil
}

// DeleteRouting removes a routing profile.
func (s *Setting) DeleteRouting(name string) error {
	if err := validRoutingName(name); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.RoutingDir(), name+".json"))
}

// PresetRouting returns a preset routing ("Default" or "Bypass LAN and Mainland").
func PresetRouting(preset int) *Routing { return defaultRoutingWithPreset(preset) }

func loadRouting(baseDir, active string) *Routing {
	r, err := LoadRouting(baseDir, active)
	if err != nil {
		return defaultRouting()
	}
	return r
}

// LoadRouting loads a routing profile by name.
func LoadRouting(baseDir, name string) (*Routing, error) {
	if name == "" {
		name = "Default"
	}
	r := defaultRouting()
	data, err := os.ReadFile(filepath.Join(baseDir, "routes_box", name+".json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, err
	}
	r.Normalize()
	return r, nil
}

// defaultRoutingWithPreset seeds a new routing profile with a bypass preset. One preset
// per country: the profile's direct lists become that country's network ranges and its
// own domain list, so a reader who lives there is off the tunnel for their own internet
// and on it for everything else — the same shape for each, only the country changed.
// The rule sets these names refer to (geoip:ru, geosite:category-ru, geoip:us) sit with
// every other rule set under one source per kind (see configbuilder.RuleSetSources);
// category-ru rather than geosite:ru because the upstream list does not ship one for
// Russia, and the US profile's domain side stays empty for the same reason — its ranges
// alone are what it has.
func defaultRoutingWithPreset(preset int) *Routing {
	r := defaultRouting()
	if preset == 1 {
		r.DirectIP = "geoip:cn\ngeoip:private"
		r.DirectDomain = "geosite:cn"
	}
	if preset == 2 {
		r.DirectIP = "geoip:ru\ngeoip:private"
		r.DirectDomain = "geosite:category-ru"
	}
	if preset == 3 {
		r.DirectIP = "geoip:us\ngeoip:private"
	}
	if preset >= 1 {
		r.BlockDomain = "geosite:category-ads-all\ndomain:appcenter.ms\ndomain:firebase.io\ndomain:crashlytics.com"
	}
	return r
}
