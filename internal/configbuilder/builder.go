// Package configbuilder turns a nekoray profile (ProxyEntity) plus settings
// into a full sing-box configuration JSON, mirroring db/ConfigBuilder.cpp.
//
// External cores (naive / custom external / hysteria2-with-forceExternal) are
// not supported yet and produce a descriptive error.
package configbuilder

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"eagle/internal/bean"
	"eagle/internal/db"
	"eagle/internal/dnsstub"
	"eagle/internal/netx"
	"eagle/internal/setting"
	"eagle/internal/util"
)

// Result is the outcome of a build.
type Result struct {
	Config         map[string]interface{}
	OutboundTags   []string
	Externals      []*ExternalSpec
	NeedKeepVPNOff bool
	// NeedVPN marks a config that carries a tun inbound, so the launcher knows
	// the platform's tun driver has to be present before it starts the core.
	NeedVPN bool
	// HotNodes maps each profile this config can be switched to without being built again
	// to the outbound tag that profile answers to inside it: they are all behind the
	// selector the proxy tag now names (see proxyGroup). A node that is not in this map is
	// not in the config, and starting it means starting the core again — which is what a
	// caller has to fall back to.
	HotNodes map[int]string
}

// BuildConfig builds a full sing-box config for a profile.
func BuildConfig(ent *db.ProxyEntity, mgr *db.Manager, s *setting.Setting, forTest bool) (*Result, error) {
	// raw sing-box config profile
	if ent.Type == "custom" && ent.Bean.Str("core") == "internal-full" {
		var cfg map[string]interface{}
		if err := json.Unmarshal([]byte(ent.Bean.Str("cs")), &cfg); err != nil {
			return nil, fmt.Errorf("invalid custom config: %w", err)
		}
		if cc := ent.Bean.Str("c_cfg"); cc != "" {
			var extra map[string]interface{}
			if json.Unmarshal([]byte(cc), &extra) == nil {
				util.MergeJSON(cfg, extra)
			}
		}
		if forTest {
			stripTestConflicts(cfg)
		}
		return &Result{Config: cfg}, nil
	}

	b := &builder{
		mgr:            mgr,
		s:              s,
		forTest:        forTest,
		globalProfiles: map[int]bool{},
	}
	cfg, err := b.buildSingBox(ent)
	if err != nil {
		return nil, err
	}
	if cc := ent.Bean.Str("c_cfg"); cc != "" {
		var extra map[string]interface{}
		if json.Unmarshal([]byte(cc), &extra) == nil {
			util.MergeJSON(cfg, extra)
		}
	}
	return &Result{
		Config:         cfg,
		OutboundTags:   b.outboundTags,
		Externals:      b.externals,
		NeedKeepVPNOff: b.needKeepVPNOff,
		// What this config can be switched between without being built again.
		// Filled by proxyGroup while the config is built, and it has to travel
		// with the Result or the whole hot-switch path is dead on arrival: the
		// controller records this map for tryHotSwitch to look a profile up in,
		// and a Result that omits it leaves that map nil, which reads as "this
		// config carries no selector" — every node switch restarts the core,
		// which is exactly what the selector exists to avoid. (BuildVPNConfig
		// carries it; this path, the one every mode actually uses, did not.)
		HotNodes: b.hotNodes,
	}, nil
}

// stripTestConflicts takes out of a raw config whatever would make the speed test's
// own core fight the app's for a resource.
//
// A custom profile marked internal-full is the reader's whole sing-box config —
// inbounds, management API and all — and the test core is a second instance of the
// same program on the same machine. Left alone, it tries to bind the mixed port and
// the Clash API the app's core is already holding (and, for a config carrying a tun
// inbound, to open the adapter the front-end owns). It then dies on its first line,
// and the node is reported as unavailable: the reader is told their node is broken
// when the truth is that the test never ran.
//
// The probe needs none of it. It reaches the test core through the mixed inbound
// the runner appends itself (see speedtest.withTestCore) and reads its answer from
// there, so what is taken out is exactly what a measurement does not use.
func stripTestConflicts(cfg map[string]interface{}) {
	// The tun one matters as much as the port: a raw config that carries its own tun
	// inbound would have the test core reach for the adapter the front-end is holding.
	delete(cfg, "inbounds")
	exp, ok := cfg["experimental"].(map[string]interface{})
	if !ok {
		return
	}
	// The Clash API is the app's own port, and the cache file is a file the app's
	// core holds open: neither is a thing a probe reads.
	delete(exp, "clash_api")
	delete(exp, "cache_file")
	if len(exp) == 0 {
		delete(cfg, "experimental")
	}
}

// BuildVPNConfig builds a TUN/VPN config for the profile — the single-process shape where
// the tun inbound and the node live in one core. Nothing calls it since the two-process
// Tun2Socket setup became the only TUN path (see app.ensureTunFront); it is kept until
// that path has soaked in real use, then deleted together with the vpn_internal_tun
// switch it served.
func BuildVPNConfig(ent *db.ProxyEntity, mgr *db.Manager, s *setting.Setting) (*Result, error) {
	b := &builder{
		mgr:            mgr,
		s:              s,
		vpn:            true,
		globalProfiles: map[int]bool{},
	}
	cfg, err := b.buildSingBox(ent)
	if err != nil {
		return nil, err
	}
	return &Result{
		Config:         cfg,
		OutboundTags:   b.outboundTags,
		Externals:      b.externals,
		NeedKeepVPNOff: b.needKeepVPNOff,
		NeedVPN:        true,
		HotNodes:       b.hotNodes,
	}, nil
}

type builder struct {
	mgr     *db.Manager
	s       *setting.Setting
	forTest bool
	vpn     bool

	inbounds     []interface{}
	outbounds    []interface{}
	ignoreConn   []string
	outboundTags []string
	chainRules   []interface{}

	externals      []*ExternalSpec
	needKeepVPNOff bool

	globalProfiles map[int]bool

	// sing-box 1.13 removed the per-inbound sniff / domain-strategy fields and
	// 1.12 removed the special `dns-out` and `block` outbounds. What is left of
	// them here are the *intent*: the tags that should be sniffed, whether sniffing
	// is for the destination rather than for routing, and the strategy for resolving
	// domains — all of which buildRoute turns into route actions.
	sniffInbounds []string
	// sniffForDestination is the third sniffing mode: the sniffed domain is to be kept
	// and handed to the outbound, so the far end resolves it, instead of being resolved
	// here for the rules to match on. It used to be written into the config as an
	// `override_destination` flag on the sniff rule, which sing-box 1.13 refuses to
	// decode — the core exits on startup naming that field. See buildRoute for what
	// carries the intent now.
	sniffForDestination bool
	resolveStrategy     string
	// rule sets pulled in by geoip:/geosite: rule items, tag -> download url
	ruleSets map[string]string
	// firstHopTag is the tag given to the first outbound of chain 0. Everything else in
	// the config names that outbound, so it is normally "proxy"; when the rest of the
	// group is carried along too, that name belongs to the selector and this one steps
	// aside (see proxyGroup).
	firstHopTag string
	// hotNodes maps a profile that ended up behind that selector to the outbound tag it
	// answers to there — which is what a caller needs to point the selector at it.
	hotNodes map[int]string
	// defaultResolverServer is the DNS server the route's default_domain_resolver
	// names. It is one of this builder's own tags, unless the reader supplied a DNS
	// object of their own — that replaces the servers wholesale, and the tag has to
	// be one that is in it.
	defaultResolverServer string

	domainListDNSRemote []string
	domainListDNSDirect []string
	domainListRemote    []string
	domainListDirect    []string
	domainListBlock     []string
	ipListRemote        []string
	ipListDirect        []string
	ipListBlock         []string
}

// setStrategy writes a domain strategy only when sing-box accepts it. nekoray
// stores "AsIs" for the default, which sing-box rejects with
// "unknown domain strategy", so such values are dropped instead — and so is an
// empty one, which would otherwise land in the config as `"strategy": ""`.
func setStrategy(obj map[string]interface{}, key, value string) {
	if value != "" && setting.IsValidStrategy(value) {
		obj[key] = value
	}
}

func (b *builder) buildSingBox(ent *db.ProxyEntity) (map[string]interface{}, error) {
	cfg := map[string]interface{}{}
	cfg["log"] = map[string]interface{}{"level": b.s.LogLevel}

	// inbounds
	if b.vpn {
		b.inbounds = append(b.inbounds, b.tunInbound(true))
	} else if util.IsValidPort(b.s.InboundSocksPort) && !b.forTest {
		in := map[string]interface{}{
			"tag":         "mixed-in",
			"type":        "mixed",
			"listen":      b.s.InboundAddress,
			"listen_port": b.s.InboundSocksPort,
		}
		b.noteSniffing("mixed-in")
		if b.s.InboundAuth != nil && b.s.InboundAuth.NeedAuth() {
			in["users"] = []interface{}{map[string]interface{}{
				"username": b.s.InboundAuth.Username,
				"password": b.s.InboundAuth.Password,
			}}
		}
		b.inbounds = append(b.inbounds, in)
	}

	// outbounds, behind a selector when the rest of the group can be carried along too
	// (see proxyGroup)
	tagProxy, err := b.proxyGroup(ent)
	if err != nil {
		return nil, err
	}

	// `bypass` stays a plain direct outbound: rules route to it by name, which
	// the new router still accepts. The special `block` and `dns-out` outbounds
	// are gone in 1.13 — their jobs are rules with the reject and hijack-dns
	// actions now (see buildRoute).
	b.outbounds = append(b.outbounds,
		map[string]interface{}{"type": "direct", "tag": "direct"},
		map[string]interface{}{"type": "direct", "tag": "bypass"},
	)

	// custom inbounds
	if !b.forTest && b.s.CustomInbound != "" {
		var ci map[string]interface{}
		if json.Unmarshal([]byte(b.s.CustomInbound), &ci) == nil {
			if arr, ok := ci["inbounds"].([]interface{}); ok {
				b.inbounds = append(b.inbounds, arr...)
			}
		}
	}

	cfg["inbounds"] = b.inbounds
	cfg["outbounds"] = b.outbounds

	// user routing rules
	if !b.forTest {
		b.collectUserRules()
	}

	cfg["dns"] = b.buildDNS(tagProxy)
	cfg["route"] = b.buildRoute(tagProxy)

	if !b.forTest {
		exp := map[string]interface{}{}
		if b.s.CoreBoxCacheFile {
			// No path: the core's default is cache.db in the working directory, and the
			// working directory is the data directory (see cmd.Dir in the controller), so
			// the file lands beside the routing profiles and the rule sets rather than
			// wherever the app happened to be launched from.
			//
			// store_fakeip is the reason this is written at all: it keeps the domain ↔
			// fake address mapping across a restart of the core. See the comment on the
			// setting for what goes wrong without it.
			exp["cache_file"] = map[string]interface{}{
				"enabled":      true,
				"store_fakeip": true,
			}
		}
		if b.s.CoreBoxClashAPI > 0 {
			exp["clash_api"] = map[string]interface{}{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", b.s.CoreBoxClashAPI),
				"secret":              b.s.CoreBoxClashAPISecret,
				"external_ui":         "dashboard",
			}
		}
		if len(exp) > 0 {
			cfg["experimental"] = exp
		}
	}
	return cfg, nil
}

// --- chain ---

// hopPortRanges rewrites a hopped port list the way sing-box reads one. The field the
// subscriptions carry spells a range with a dash and joins several with commas
// ("22000-27000", or "1000-2000,3000-4000"); sing-box's "server_ports" is a list of colon
// ranges (["22000:27000"], ["1000:2000", "3000:4000"]). A piece that is already written the
// second way goes through unchanged, so a correct profile is not rewritten into something
// else.
func hopPortRanges(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		// A single port is not a range. sing-box reads "start:end", and a bare
		// number was refused with "bad port range: 443" — the whole config went
		// down over a hop list that named one port.
		if !strings.Contains(part, "-") && !strings.Contains(part, ":") {
			part = part + ":" + part
		}
		out = append(out, strings.ReplaceAll(part, "-", ":"))
	}
	return out
}

func (b *builder) resolveChain(ent *db.ProxyEntity) ([]*db.ProxyEntity, error) {
	if ent.Type != "chain" {
		return []*db.ProxyEntity{ent}, nil
	}
	list := ent.Bean.IntList("list")
	// stored in->out, reverse so index 0 is the closest hop to the network
	var resolved []*db.ProxyEntity
	for i := len(list) - 1; i >= 0; i-- {
		// Snapshots: every entity this build walks is read while other
		// goroutines may be writing the live one under the manager's lock (see
		// Manager.GetProfile), and this walk happens outside that lock.
		p := b.mgr.SnapshotProfile(list[i])
		if p == nil {
			return nil, fmt.Errorf("chain missing entity: %d", list[i])
		}
		if p.Type == "chain" {
			return nil, fmt.Errorf("chain inside chain is not allowed: %d", list[i])
		}
		resolved = append(resolved, p)
	}
	return resolved, nil
}

func (b *builder) buildChain(chainID int, ent *db.ProxyEntity) (string, error) {
	group := b.mgr.SnapshotGroup(ent.GID)
	if group == nil {
		return "", fmt.Errorf("this profile is not in any group, your data may be corrupted")
	}
	ents, err := b.resolveChain(ent)
	if err != nil {
		return "", err
	}
	if group.FrontProxyID >= 0 {
		fe := b.mgr.SnapshotProfile(group.FrontProxyID)
		if fe == nil {
			return "", fmt.Errorf("front proxy entity not found")
		}
		feEnts, err := b.resolveChain(fe)
		if err != nil {
			return "", err
		}
		ents = append(ents, feEnts...)
	}
	// The same node twice in one chain: the front proxy's members and the
	// node's own can name the same node — a group's front proxy is usually one
	// of the group's own. The outbound tag is derived from the chain and the
	// node, so the node would be built twice under one name and the core
	// refuses the whole config ("duplicate outbound/end point tag"). One copy
	// of a node is one hop: routing through it twice is the same hop.
	ents = dedupeEntities(ents)
	return b.buildChainInternal(chainID, ents)
}

// dedupeEntities drops the repeats from a chain, keeping the first occurrence.
func dedupeEntities(ents []*db.ProxyEntity) []*db.ProxyEntity {
	seen := map[int]bool{}
	out := make([]*db.ProxyEntity, 0, len(ents))
	for _, e := range ents {
		if e == nil || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return out
}

// The most members a selector is given. A group of a hundred subscriptions is ordinary; a
// group of a thousand is not, and every member is a whole outbound that the core parses at
// start-up — the cap keeps that file, and that start, finite.
const maxSelectorMembers = 256

// ProxyTag is the name the rest of a config calls the proxy by: the detour of the remote
// dns server, the outbound the split rules point at, the final outbound. It is the selector
// when the config carries one and the single chain when it does not — and it is this name
// either way, which is what lets a running core be pointed at another node without any of
// the rest of the config being rewritten.
const ProxyTag = "proxy"

// proxyGroup builds the proxy side of the config and returns the tag the rest of it should
// point at.
//
// The node that was asked for keeps chain ID 0 but not the name: its first hop is called
// something else, and the name everything else in the config uses — "proxy", which dns, the
// route rules and the tun inbound all refer to — is given to a selector listing this node
// and the rest of its group. The other nodes are built behind chain IDs of their own, which
// is what keeps their tags apart.
//
// The point of it: switching nodes no longer means starting the core again. sing-box reads
// its config once, so a selector is the only way to move between nodes at all — and the
// alternative is what happens without one: stop the core, which takes the tun adapter down
// with it, and start a new one.
//
// Best-effort in one direction only. A node that cannot be carried in this config — one
// that needs an external core, or one that shares a front proxy another node already
// claimed — is left out of the selector, and switching to it falls back to the old path.
func (b *builder) proxyGroup(ent *db.ProxyEntity) (string, error) {
	if b.forTest {
		// A test config is one throwaway node: no selector, and nothing to rename in it.
		return b.buildChain(0, ent)
	}
	// "proxy" is about to belong to the selector, so the first hop of the node that was
	// asked for is given a name of its own.
	b.firstHopTag = "proxy-0"
	tag, err := b.buildChain(0, ent)
	if err != nil {
		return "", err
	}
	if tag != "proxy-0" {
		// The chain decided it could not be built as asked; there is nothing to select.
		return "", fmt.Errorf("this configuration cannot be set automatically, please try another")
	}
	b.hotNodes = map[int]string{ent.ID: tag}
	members := []interface{}{tag}

	candidates := b.mgr.ProfilesWithOrder(b.s.CurrentGroup)
	if len(candidates) > maxSelectorMembers {
		candidates = candidates[:maxSelectorMembers]
	}
	for i, p := range candidates {
		if p.ID == ent.ID {
			continue
		}
		// A node needing an external core would have this app start a second process for a
		// node nobody asked for; one sharing the group's front proxy is already in the
		// config under that name, and building it again would collide on the tag.
		if b.needExternal(p, true) != 0 {
			continue
		}
		// An incomplete node — one created from the add menu and not filled in
		// yet, or a broken subscription entry — has no credential to dial with,
		// and the core refuses the WHOLE config over one empty password
		// ("missing password" took down every node in the group with it). Left
		// out of the selector; switching to it falls back to the old path (see
		// the note above), and the moment it is filled in it joins like any
		// other member.
		if !b.outboundComplete(p) {
			continue
		}
		t, err := b.buildChain(i+1, p)
		if err != nil || t == "" {
			continue
		}
		members = append(members, t)
		b.hotNodes[p.ID] = t
	}
	if len(members) < 2 {
		// Nothing to select between: leave the config exactly as it was, a plain chain.
		return tag, nil
	}
	b.outbounds = append(b.outbounds, map[string]interface{}{
		"type":      "selector",
		"tag":       ProxyTag,
		"outbounds": members,
	})
	return ProxyTag, nil
}

// outboundComplete reports whether the entity can be dialled as built —
// the credential its protocol requires, and the server to dial at all. It is
// the selector's membership test: sing-box refuses an entire config over one
// member's missing password, so a node still being filled in must not sit in
// it. A chain's members are the node it is made of, and they answer for
// themselves; a chain naming a missing entity is a build error further down,
// and is treated as incomplete here so the selector simply leaves it out.
func (b *builder) outboundComplete(ent *db.ProxyEntity) bool {
	if ent == nil {
		return false
	}
	// A chain is made of other nodes and has no address of its own, so what
	// decides it is its members — and that has to be asked BEFORE the address
	// below, which a chain never has. Asked after it, every chain was left out
	// of the selector: a chain the reader had built sat in the list of nodes
	// and could not be switched to, and the group it was in read as if the
	// chain were not there at all.
	if ent.Type == "chain" {
		members, err := b.resolveChain(ent)
		if err != nil {
			return false
		}
		for _, m := range members {
			if !b.outboundComplete(m) {
				return false
			}
		}
		return true
	}
	if strings.TrimSpace(ent.ServerAddress()) == "" || ent.ServerPort() <= 0 {
		return false
	}
	switch ent.Type {
	case "shadowsocks", "trojan", "vless", "anytls":
		return strings.TrimSpace(ent.Bean.Str("pass")) != ""
	case "vmess":
		return strings.TrimSpace(ent.Bean.Str("id")) != ""
	case "snell":
		return strings.TrimSpace(ent.Bean.Str("psk")) != ""
	case "tuic":
		return strings.TrimSpace(ent.Bean.Str("uuid")) != "" &&
			strings.TrimSpace(ent.Bean.Str("password")) != ""
	case "hysteria2":
		// buildQUIC writes the password through unchanged, and the schema the
		// bundled core checks requires it.
		return strings.TrimSpace(ent.Bean.Str("password")) != ""
	}
	return true
}

func (b *builder) buildChainInternal(chainID int, ents []*db.ProxyEntity) (string, error) {
	chainTag := fmt.Sprintf("c-%d", chainID)
	chainTagOut := ""
	muxApplied := false

	pastTag := ""
	pastExternalStat := 0

	for index, ent := range ents {
		tagOut := fmt.Sprintf("%s-%d", chainTag, ent.ID)
		isFirstProfile := index == len(ents)-1
		// The front proxy's outbound already built by another member's chain is
		// not rebuilt; this chain still needs its wiring, and only the build is
		// what gets skipped.
		skipBuild := false
		if isFirstProfile {
			tagOut = fmt.Sprintf("g-%d", ent.ID)
			if b.globalProfiles[ent.ID] {
				skipBuild = true
			} else {
				b.globalProfiles[ent.ID] = true
			}
		}
		if chainID == 0 && index == 0 {
			tagOut = "proxy"
			if b.firstHopTag != "" {
				tagOut = b.firstHopTag
			}
		}
		if index != 0 && !skipBuild {
			b.ignoreConn = append(b.ignoreConn, tagOut)
		}

		// The wiring reaches across a skipped build on purpose: the detour (or
		// the chain rule, when the previous hop ran its own core) names this
		// hop's tag, and this hop's tag is shared with the chain that built it.
		// Left unwired, the previous outbound would dial the target directly —
		// a group's front proxy silently bypassed by every member whose chain
		// ran after the first one's, with nothing anywhere to say so.
		if index > 0 {
			if pastExternalStat == 0 {
				// attach previous outbound as a detour of this one
				if prev, ok := b.outbounds[len(b.outbounds)-1].(map[string]interface{}); ok {
					prev["detour"] = tagOut
				}
			} else {
				b.chainRules = append(b.chainRules, map[string]interface{}{
					"inbound":  []interface{}{pastTag + "-mapping"},
					"outbound": tagOut,
				})
			}
		} else {
			chainTagOut = tagOut
		}
		if skipBuild {
			continue
		}

		// external core decision
		extStat := b.needExternal(ent, isFirstProfile)
		if extStat < 0 {
			return "", fmt.Errorf("this configuration cannot be set automatically, please try another")
		}
		extMappingPort, extSocksPort := 0, 0
		if extStat > 0 {
			if ent.Type == "custom" {
				extMappingPort = ent.Bean.Int("mapping_port")
				if !util.IsValidPort(extMappingPort) {
					extMappingPort = mkPort()
				}
				extSocksPort = ent.Bean.Int("socks_port")
				if !util.IsValidPort(extSocksPort) {
					extSocksPort = mkPort()
				}
			} else {
				extMappingPort = mkPort()
				extSocksPort = mkPort()
			}
		}
		if extStat == 2 {
			b.needKeepVPNOff = true
		}
		if extStat == 1 {
			b.inbounds = append(b.inbounds, map[string]interface{}{
				"type":             "direct",
				"tag":              tagOut + "-mapping",
				"listen":           "127.0.0.1",
				"listen_port":      extMappingPort,
				"override_address": ent.ServerAddress(),
				"override_port":    ent.ServerPort(),
			})
			if isFirstProfile {
				b.chainRules = append(b.chainRules, map[string]interface{}{
					"inbound":  []interface{}{tagOut + "-mapping"},
					"outbound": "direct",
				})
			}
		}

		var outbound map[string]interface{}
		if extStat > 0 {
			spec, err := b.buildExternal(ent, extMappingPort, extSocksPort, extStat)
			if err != nil {
				return "", err
			}
			if spec.Program == "" {
				return "", fmt.Errorf("core not found: %s", ent.DisplayCoreType())
			}
			spec.Tag = ent.DisplayType()
			b.externals = append(b.externals, spec)
			outbound = map[string]interface{}{
				"type":        "socks",
				"server":      "127.0.0.1",
				"server_port": extSocksPort,
			}
		} else {
			var err error
			outbound, err = buildOutbound(ent, b.s)
			if err != nil {
				return "", err
			}
		}

		outbound["tag"] = tagOut

		if !muxApplied && b.needMux(ent, outbound) {
			outbound["multiplex"] = map[string]interface{}{
				"enabled":     true,
				"protocol":    b.s.MuxProtocol,
				"padding":     b.s.MuxPadding,
				"max_streams": b.s.MuxConcurrency,
			}
			muxApplied = true
		}
		if co := ent.Bean.Str("c_out"); co != "" {
			var extra map[string]interface{}
			if json.Unmarshal([]byte(co), &extra) == nil {
				util.MergeJSON(outbound, extra)
			}
		}

		// keep DNS lookups for the first profile on the direct path
		if addr := ent.ServerAddress(); addr != "" && !util.IsIPAddress(addr) {
			b.domainListDNSDirect = append(b.domainListDNSDirect, "full:"+addr)
		}

		b.outbounds = append(b.outbounds, outbound)
		b.outboundTags = append(b.outboundTags, tagOut)
		pastTag = tagOut
		pastExternalStat = extStat
	}
	return chainTagOut, nil
}

func (b *builder) needMux(ent *db.ProxyEntity, outbound map[string]interface{}) bool {
	needMux := ent.Type == "vmess" || ent.Type == "trojan" || ent.Type == "vless"
	needMux = needMux && b.s.MuxConcurrency > 0
	if stream := ent.Bean.Peek("stream"); stream != nil {
		net := stream.Str("net")
		if net == "" {
			net = "tcp"
		}
		if net == "grpc" || net == "quic" || (net == "http" && stream.Str("sec") == "tls") {
			needMux = false
		}
		switch stream.Int("mux_s") {
		case 0:
			if !b.s.MuxDefaultOn {
				needMux = false
			}
		case 1:
			needMux = true
		case 2:
			needMux = false
		}
	}
	if ent.Type == "vless" {
		if flow, _ := outbound["flow"].(string); flow != "" {
			needMux = false
		}
	}
	return needMux
}

// needExternal returns -1 (unsupported), 0 (built-in), 1 (mapping) or 2 (direct).
func (b *builder) needExternal(e *db.ProxyEntity, isFirstProfile bool) int {
	switch e.Type {
	case "naive":
		if isFirstProfile {
			if b.s.VPNMode() {
				return 1
			}
			return 2
		}
		return 1
	case "hysteria2", "tuic":
		if !e.Bean.Bool("forceExternal") {
			return 0
		}
		if isFirstProfile {
			if b.s.VPNMode() && strings.TrimSpace(e.Bean.Str("hopPort")) == "" {
				return 1
			}
			return 2
		}
		if strings.TrimSpace(e.Bean.Str("hopPort")) != "" {
			return -1
		}
		return 1
	case "custom":
		core := e.Bean.Str("core")
		if core == "internal" || core == "internal-full" {
			return 0
		}
		return 1
	default:
		return 0
	}
}

// mkPort returns a free local port.
func mkPort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// --- outbound builders ---

func buildOutbound(ent *db.ProxyEntity, s *setting.Setting) (map[string]interface{}, error) {
	switch ent.Type {
	case "socks", "http":
		return buildSocksHTTP(ent, s), nil
	case "shadowsocks":
		return buildShadowSocks(ent, s), nil
	case "vmess":
		return buildVMess(ent, s), nil
	case "trojan", "vless":
		return buildTrojanVLESS(ent, s), nil
	case "anytls":
		return buildAnyTLS(ent, s), nil
	case "snell":
		return buildSnell(ent), nil
	case "hysteria":
		return buildHysteria1(ent), nil
	case "hysteria2", "tuic":
		return buildQUIC(ent), nil
	case "custom":
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(ent.Bean.Str("cs")), &obj); err != nil {
			return nil, fmt.Errorf("invalid custom outbound: %w", err)
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("unsupported outbound type: %s", ent.Type)
	}
}

func buildSocksHTTP(ent *db.ProxyEntity, s *setting.Setting) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{}
	v := b.Int("v")
	if ent.Type == "http" || v == -80 {
		out["type"] = "http"
	} else {
		out["type"] = "socks"
		if v == 4 {
			out["version"] = "4"
		}
	}
	out["server"] = ent.ServerAddress()
	out["server_port"] = ent.ServerPort()
	if u, p := b.Str("username"), b.Str("password"); u != "" && p != "" {
		out["username"] = u
		out["password"] = p
	}
	applyStreamSettings(ent, out, s)
	return out
}

func buildShadowSocks(ent *db.ProxyEntity, s *setting.Setting) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{
		"type":        "shadowsocks",
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
		"method":      b.Str("method"),
		"password":    b.Str("pass"),
	}
	if uot := b.Int("uot"); uot != 0 {
		out["udp_over_tcp"] = map[string]interface{}{"enabled": true, "version": uot}
	}
	if plugin := strings.TrimSpace(b.Str("plugin")); plugin != "" {
		out["plugin"] = before(plugin, ";")
		out["plugin_opts"] = after(plugin, ";")
	}
	applyStreamSettings(ent, out, s)
	return out
}

func buildVMess(ent *db.ProxyEntity, s *setting.Setting) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{
		"type":        "vmess",
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
		"uuid":        strings.TrimSpace(b.Str("id")),
		"alter_id":    b.Int("aid"),
		"security":    b.Str("sec"),
	}
	applyStreamSettings(ent, out, s)
	return out
}

func buildTrojanVLESS(ent *db.ProxyEntity, s *setting.Setting) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
	}
	if ent.Type == "vless" {
		out["type"] = "vless"
		flow := b.Str("flow")
		if strings.HasSuffix(flow, "-udp443") {
			flow = strings.TrimSuffix(flow, "-udp443")
		} else if flow == "none" {
			flow = ""
		}
		out["uuid"] = strings.TrimSpace(b.Str("pass"))
		out["flow"] = flow
	} else {
		out["type"] = "trojan"
		out["password"] = b.Str("pass")
	}
	applyStreamSettings(ent, out, s)
	return out
}

// buildAnyTLS emits an anytls outbound. The protocol is new in sing-box 1.12 and
// is TLS-only, so everything TLS-shaped lives in the stream's `tls` object,
// which applyStreamSettings already builds.
func buildAnyTLS(ent *db.ProxyEntity, s *setting.Setting) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{
		"type":        "anytls",
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
		"password":    b.Str("pass"),
	}
	applyStreamSettings(ent, out, s)
	return out
}

// buildSnell emits a snell outbound. Its obfuscation is not a stream setting: snell
// carries a mode and, for that mode, a host — and sing-box wants them as an object of
// their own beside the key rather than inside the transport.
func buildSnell(ent *db.ProxyEntity) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{
		"type":        "snell",
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
		"psk":         b.Str("psk"),
	}
	// 1 through 4. It is what tells one generation of server from the next, and the
	// protocol is not one wire format across them.
	v := b.Int("version")
	if v <= 0 {
		v = 4
	}
	out["version"] = v
	mode := strings.TrimSpace(b.Str("obfsMode"))
	host := strings.TrimSpace(b.Str("obfsHost"))
	if mode != "" {
		out["obfs_mode"] = mode
	}
	if host != "" {
		out["obfs_host"] = host
	}
	return out
}

// buildHysteria1 emits a hysteria v1 outbound. Written out here rather than shared with
// buildQUIC, because the two versions agree on little beyond "there is a server and it
// speaks QUIC": v1 authenticates with an auth string, and its obfuscation is a plain
// value with a UDP protocol beside it — neither of which v2 has.
//
// The speed limits are the one thing the core insists on. It refuses the outbound
// without them, so a node that arrived without its up/down pair gets the pair this app
// assumes for a link it has not measured — refusing to start the node over a missing
// number would be the worse answer.
func buildHysteria1(ent *db.ProxyEntity) map[string]interface{} {
	b := ent.Bean
	out := map[string]interface{}{
		"type":        "hysteria",
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
		"up_mbps":     positiveOr(b.Int("uploadMbps"), 50),
		"down_mbps":   positiveOr(b.Int("downloadMbps"), 100),
	}
	if auth := strings.TrimSpace(b.Str("auth")); auth != "" {
		out["auth_str"] = auth
	}
	if obfs := strings.TrimSpace(b.Str("obfs")); obfs != "" {
		out["obfs"] = obfs
	}
	// "udp" is the default of both sides, and the only one worth not writing down.
	if p := strings.TrimSpace(b.Str("protocol")); p != "" && p != "udp" {
		out["protocol"] = p
	}
	if hop := strings.TrimSpace(b.Str("hopPort")); hop != "" {
		if ports := hopPortRanges(hop); len(ports) > 0 {
			out["server_ports"] = ports
		}
	}
	tls := map[string]interface{}{
		"enabled":     true,
		"insecure":    b.Bool("allowInsecure"),
		"disable_sni": b.Bool("disableSni"),
	}
	if sni := strings.TrimSpace(b.Str("sni")); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := strings.TrimSpace(b.Str("alpn")); alpn != "" {
		tls["alpn"] = splitComma(alpn)
	}
	if ca := strings.TrimSpace(b.Str("caText")); ca != "" {
		tls["certificate"] = ca
	}
	out["tls"] = tls
	return out
}

// positiveOr is v when it is a usable one and the fallback otherwise.
func positiveOr(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func buildQUIC(ent *db.ProxyEntity) map[string]interface{} {
	b := ent.Bean
	tls := map[string]interface{}{
		"enabled":     true,
		"disable_sni": b.Bool("disableSni"),
		"insecure":    b.Bool("allowInsecure"),
		"certificate": strings.TrimSpace(b.Str("caText")),
		"server_name": b.Str("sni"),
	}
	if alpn := strings.TrimSpace(b.Str("alpn")); alpn != "" {
		tls["alpn"] = splitComma(alpn)
	}
	out := map[string]interface{}{
		"server":      ent.ServerAddress(),
		"server_port": ent.ServerPort(),
		"tls":         tls,
	}
	if ent.Type == "hysteria2" {
		tls["alpn"] = "h3"
		out["type"] = "hysteria2"
		out["password"] = b.Str("password")
		out["up_mbps"] = b.Int("uploadMbps")
		out["down_mbps"] = b.Int("downloadMbps")
		if hopPort := strings.TrimSpace(b.Str("hopPort")); hopPort != "" {
			// The field the subscriptions fill and the field sing-box reads are spelled
			// differently, and so is the range inside them: this side carries "22000-27000"
			// (dashes, several joined by commas), sing-box wants a list of colon ranges —
			// "server_ports": ["22000:27000"]. Written through unchanged, the core refuses
			// the whole config at start-up, as a decode error that reads like anything but a
			// separator.
			if ports := hopPortRanges(hopPort); len(ports) > 0 {
				out["server_ports"] = ports
			}
			// And hop_interval is a duration, not a number: "10s", never 10. A number here
			// is what made every start of a hopping node die at decode.
			if iv := b.Int("hopInterval"); iv > 0 {
				out["hop_interval"] = strconv.Itoa(iv) + "s"
			}
		}
		if obfs := b.Str("obfsPassword"); obfs != "" {
			out["obfs"] = map[string]interface{}{"type": "salamander", "password": obfs}
		}
	} else {
		out["type"] = "tuic"
		out["uuid"] = b.Str("uuid")
		out["password"] = b.Str("password")
		out["congestion_control"] = b.Str("congestionControl")
		if b.Bool("uos") {
			out["udp_over_stream"] = true
		} else {
			out["udp_relay_mode"] = b.Str("udpRelayMode")
		}
		out["zero_rtt_handshake"] = b.Bool("zeroRttHandshake")
		if hb := strings.TrimSpace(b.Str("heartbeat")); hb != "" {
			out["heartbeat"] = hb
		}
	}
	return out
}

// applyStreamSettings mirrors V2rayStreamSettings::BuildStreamSettingsSingBox.
func applyStreamSettings(ent *db.ProxyEntity, outbound map[string]interface{}, s *setting.Setting) {
	stream := ent.Bean.Peek("stream")
	if stream == nil {
		return
	}
	network := stream.Str("net")
	if network == "" {
		network = "tcp"
	}
	security := stream.Str("sec")

	if network != "tcp" {
		transport := map[string]interface{}{"type": network}
		switch network {
		case "ws":
			if host := stream.Str("host"); host != "" {
				transport["headers"] = map[string]interface{}{"Host": host}
			}
			path := stream.Str("path")
			pathWithoutEd := path
			if idx := strings.Index(path, "?ed="); idx >= 0 {
				pathWithoutEd = path[:idx]
				if ed, err := strconv.Atoi(path[idx+4:]); err == nil && ed > 0 {
					transport["max_early_data"] = ed
					transport["early_data_header_name"] = "Sec-WebSocket-Protocol"
				}
			}
			if pathWithoutEd != "" {
				transport["path"] = pathWithoutEd
			}
			if l := stream.Int("ed_len"); l > 0 {
				transport["max_early_data"] = l
				transport["early_data_header_name"] = stream.Str("ed_name")
			}
		case "http":
			if p := stream.Str("path"); p != "" {
				transport["path"] = p
			}
			if h := stream.Str("host"); h != "" {
				transport["host"] = splitComma(h)
			}
		case "grpc":
			if p := stream.Str("path"); p != "" {
				transport["service_name"] = p
			}
		case "httpupgrade":
			if p := stream.Str("path"); p != "" {
				transport["path"] = p
			}
			if h := stream.Str("host"); h != "" {
				transport["host"] = h
			}
		}
		outbound["transport"] = transport
	} else if stream.Str("h_type") == "http" {
		outbound["transport"] = map[string]interface{}{
			"type":    "http",
			"method":  "GET",
			"path":    stream.Str("path"),
			"headers": map[string]interface{}{"Host": splitComma(stream.Str("host"))},
		}
	}

	if security == "tls" {
		tls := map[string]interface{}{"enabled": true}
		if stream.Bool("insecure") || s.SkipCert {
			tls["insecure"] = true
		}
		if sni := strings.TrimSpace(stream.Str("sni")); sni != "" {
			tls["server_name"] = sni
		}
		if cert := strings.TrimSpace(stream.Str("cert")); cert != "" {
			tls["certificate"] = cert
		}
		if alpn := strings.TrimSpace(stream.Str("alpn")); alpn != "" {
			tls["alpn"] = splitComma(alpn)
		}
		fp := stream.Str("utls")
		if pbk := strings.TrimSpace(stream.Str("pbk")); pbk != "" {
			sid := splitComma(stream.Str("sid"))
			shortID := ""
			if len(sid) > 0 {
				shortID, _ = sid[0].(string)
			}
			tls["reality"] = map[string]interface{}{"enabled": true, "public_key": pbk, "short_id": shortID}
			if fp == "" {
				fp = "random"
			}
		}
		if fp != "" {
			tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
		}
		outbound["tls"] = tls
	}

	if (outbound["type"] == "vmess" || outbound["type"] == "vless") && stream != nil {
		if pe := strings.TrimSpace(stream.Str("pac_enc")); pe != "" {
			outbound["packet_encoding"] = pe
		}
	}
}

// --- dns & route ---

func (b *builder) collectUserRules() {
	r := b.s.Routing()
	for _, line := range util.SplitLinesSkipSharp(r.ProxyDomain, 0) {
		if r.DNSRouting {
			b.domainListDNSRemote = append(b.domainListDNSRemote, line)
		}
		b.domainListRemote = append(b.domainListRemote, line)
	}
	for _, line := range util.SplitLinesSkipSharp(r.DirectDomain, 0) {
		if r.DNSRouting {
			b.domainListDNSDirect = append(b.domainListDNSDirect, line)
		}
		b.domainListDirect = append(b.domainListDirect, line)
	}
	for _, line := range util.SplitLinesSkipSharp(r.BlockDomain, 0) {
		b.domainListBlock = append(b.domainListBlock, line)
	}
	for _, line := range util.SplitLinesSkipSharp(r.BlockIP, 0) {
		b.ipListBlock = append(b.ipListBlock, line)
	}
	for _, line := range util.SplitLinesSkipSharp(r.ProxyIP, 0) {
		b.ipListRemote = append(b.ipListRemote, line)
	}
	for _, line := range util.SplitLinesSkipSharp(r.DirectIP, 0) {
		b.ipListDirect = append(b.ipListDirect, line)
	}
}

// dnsServer builds one entry of dns.servers in the format sing-box 1.12+ wants:
// an explicit transport `type` plus `server`, and `domain_resolver` where the
// old format had `address_resolver`. The old `address` string ("tls://1.1.1.1",
// "https://dns.google/dns-query", "local") is what the settings still store, so
// it is split here rather than migrated in the store — a user who typed a
// custom address keeps working.
func dnsServer(tag, address, detour, domainResolver string) map[string]interface{} {
	typ, host, port, path := splitDNSAddress(address)
	obj := map[string]interface{}{"tag": tag, "type": typ}
	if host != "" {
		obj["server"] = host
	}
	if port > 0 {
		obj["server_port"] = port
	}
	if path != "" {
		obj["path"] = path
	}
	// `local` resolves through the system and takes neither detour nor resolver.
	// A detour naming the plain `direct` outbound is rejected at startup —
	// "detour to an empty direct outbound makes no sense" — and going direct is
	// what happens by default anyway, so the field is simply left off.
	if typ != "local" {
		if detour != "" && detour != "direct" {
			obj["detour"] = detour
		}
		if domainResolver != "" {
			obj["domain_resolver"] = domainResolver
		}
	}
	return obj
}

// stubServer is one entry pointing at the app's own forwarder (see internal/dnsstub).
//
// Plain TCP to the loopback: an answer that does not fit in a datagram would arrive
// truncated over UDP, and the forwarder relays whole answers. No detour — the loopback is
// not something to send through the node — and no domain_resolver, because an address that
// is already an IP has nothing to resolve.
func stubServer(tag, addr string) map[string]interface{} {
	i := strings.LastIndexByte(addr, ':')
	if i <= 0 {
		return dnsServer(tag, addr, "direct", "")
	}
	port, err := strconv.Atoi(addr[i+1:])
	if err != nil || port <= 0 || port > 65535 {
		return dnsServer(tag, addr, "direct", "")
	}
	return map[string]interface{}{
		"tag":         tag,
		"type":        "tcp",
		"server":      addr[:i],
		"server_port": port,
	}
}

// splitDNSAddress turns a nekoray-style DNS address into the pieces the new
// server format expects. Unknown or bare hosts fall back to plain UDP.
func splitDNSAddress(raw string) (typ, host string, port int, path string) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.EqualFold(s, "local") {
		return "local", "", 0, ""
	}
	if strings.EqualFold(s, "fakeip") {
		return "fakeip", "", 0, ""
	}
	rest := s
	if i := strings.Index(s, "://"); i > 0 {
		typ = strings.ToLower(s[:i])
		rest = s[i+3:]
	}
	switch typ {
	case "tls", "udp", "tcp", "quic", "h3", "https":
	case "":
		// A bare host or IP, by which the reader meant plain UDP.
		typ = "udp"
	default:
		// An unrecognised scheme is left as written rather than rewritten to
		// udp: the core refuses the type and names it, which a reader can act
		// on, where a silent udp on port 53 was every name failing with
		// nothing in the log to say why. The known-wrong spelling (`h2`) is
		// also refused on the way in now (setting.validateDNSAddress); this is
		// the path a hand-edited profile file takes.
	}
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		path = rest[j:]
		rest = rest[:j]
	}
	host = rest
	// A port may only be read off a colon that is not part of an IPv6 address. Splitting
	// on the last colon — which is what this did — reads "2001:4860:4860::8888" as host
	// "2001:4860:4860:" on port 8888, because a trailing group of digits is the norm in
	// an IPv6 address rather than the sign of a port. Brackets are the one unambiguous
	// way to say "and this is the port"; without them, a bare address with more than one
	// colon is the address entire.
	switch {
	case strings.HasPrefix(rest, "["):
		if i := strings.IndexByte(rest, ']'); i > 0 {
			host = rest[1:i]
			if k := strings.LastIndexByte(rest, ':'); k > i {
				if p, err := strconv.Atoi(rest[k+1:]); err == nil {
					port = p
				}
			}
		}
	case strings.Count(rest, ":") == 1:
		if k := strings.IndexByte(rest, ':'); k > 0 {
			if p, err := strconv.Atoi(rest[k+1:]); err == nil {
				host = rest[:k]
				port = p
			}
		}
	}
	if typ == "https" && path == "" {
		path = "/dns-query"
	}
	return typ, host, port, path
}

// dnsHostIsIPv6 reports whether a nekoray-style DNS address is a bare IPv6 literal —
// "2400:3200:baba::1", "tls://[2400:3200:baba::1]:853" — rather than a name or an IPv4
// address, either of which the network resolves or routes for itself.
func dnsHostIsIPv6(raw string) bool {
	_, host, _, _ := splitDNSAddress(raw)
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

func (b *builder) buildDNS(tagProxy string) map[string]interface{} {
	var servers []interface{}
	var dnsRules []interface{}

	// The app's own forwarder, when it is up (see internal/dnsstub). Naming its ports
	// instead of the resolvers themselves is what makes the smart pick live: the core's
	// config no longer changes when a resolver does, because which resolver stands behind
	// a port is the app's state rather than the core's configuration. Either side may be
	// missing — no upstream, no listener, an upstream that cannot be reached the way that
	// side has to be — and the reader's own address is written then, exactly as before.
	stubRemote, stubDirect := dnsstub.Endpoints()

	if !b.forTest {
		if stubRemote != "" {
			servers = append(servers, stubServer("dns-remote", stubRemote))
		} else {
			servers = append(servers, dnsServer("dns-remote", b.s.Routing().RemoteDNS, tagProxy, "dns-local"))
		}
	}
	// The domestic resolver, and with it every direct dial: route rules resolve through
	// it (route.default_domain_resolver) and so does the node's own address. A bare IPv6
	// literal here is dialled from this machine, so on a network with no IPv6 it is not
	// a slow resolver, it is none — every query to it fails and takes the country's
	// domains, the node's address and the rule sets' own downloads down with it, while
	// the settings card goes on showing the reader's pick. The pick is kept whenever it
	// can be dialled; only the case that cannot is answered with the app's default.
	//
	// The remote side is left alone on purpose: its queries travel through the proxy
	// node's own network (dns-remote carries `detour`), where an IPv6 literal may well
	// be reachable even when this machine has no route of its own.
	directDNS := b.s.Routing().DirectDNS
	if dnsHostIsIPv6(directDNS) && !b.ipv6Usable() {
		directDNS = setting.DefaultDirectDNS
	}
	directObj := dnsServer("dns-direct", directDNS, "direct", "dns-local")
	if stubDirect != "" {
		// The domestic side goes through the forwarder too, which is what lets the pick
		// move it without a rebuild. The forwarder reaches a plain resolver by pinning its
		// dials to the machine's real interface, and refuses a plain one when it cannot
		// (see dnsstub.Wires) — so a side that cannot be served here is served by the core
		// itself, which is the line above.
		directObj = stubServer("dns-direct", stubDirect)
	}
	if b.s.Routing().DNSFinalOut == "bypass" {
		servers = append([]interface{}{directObj}, servers...)
	} else {
		servers = append(servers, directObj)
	}

	servers = append(servers, dnsServer("dns-local", b.s.UnderlyingDNS(), "", ""))

	// Whether a DNS rule carries the legacy `strategy`. sing-box refuses a DNS
	// configuration that has a rule matching by query_type beside one carrying
	// it — the whole config is rejected at start-up — so the two cannot both be
	// written.
	strategyOnRule := false
	addRuleDNS := func(list []string, server, strategy string) {
		rule := b.makeRule(list, false)
		if rule == nil {
			return
		}
		rule["server"] = server
		// The per-server `strategy` field is gone as well; the address family a
		// query is allowed to return is decided by the rule that selects the
		// server, so the settings value lands here.
		setStrategy(rule, "strategy", strategy)
		if _, ok := rule["strategy"]; ok {
			strategyOnRule = true
		}
		dnsRules = append(dnsRules, rule)
	}
	// The connectivity probe's names resolve the way its request is routed (see
	// buildRoute): domestically, so Windows does not spend the probe's short allowance
	// waiting for a remote lookup before it has even sent the request.
	if !b.forTest {
		dnsRules = append(dnsRules, map[string]interface{}{
			"domain_suffix": []interface{}{"msftconnecttest.com", "msftncsi.com"},
			"server":        "dns-direct",
		})
	}
	addRuleDNS(b.domainListDNSRemote, "dns-remote", b.s.Routing().RemoteDNSStrategy)
	addRuleDNS(b.domainListDNSDirect, "dns-direct", b.s.Routing().DirectDNSStrategy)

	if !b.forTest {
		// The old dns-block server (address "rcode://success") is not a
		// transport any more: rejecting is a DNS rule action in 1.12+.
		//
		// The block on the query types is the one part of this that cannot
		// live beside a per-side strategy. sing-box refuses the whole DNS
		// configuration when a rule matches by query_type and another rule
		// carries the legacy `strategy` — the core exits at start-up with
		// "initialize dns router: Legacy `strategy` DNS rule action option is
		// deprecated in sing-box 1.14.0 ...". So a profile that set a strategy
		// produced a config the core would not start at all.
		//
		// The reader's own address family is worth more than a block on two
		// query types nothing asks for, so the block is left out when a
		// strategy is in play, and the rest of the rules stand.
		if !strategyOnRule {
			dnsRules = append(dnsRules,
				map[string]interface{}{"query_type": []interface{}{32, 33}, "action": "reject"})
		}
		dnsRules = append(dnsRules,
			map[string]interface{}{"domain_suffix": ".lan", "action": "reject"},
		)
	}

	// FakeDNS (nekoray: fake_dns, internal tun only). The tun inbound answers
	// from a reserved range and keeps the mapping in the core, so the domain
	// name survives all the way to the outbound instead of being resolved
	// locally first — which is what makes domain-based routing work for
	// applications that would otherwise hand the stack a bare IP. Since 1.12
	// fakeip is an entry in the server list rather than a dns.fakeip block.
	fake := b.vpn && b.s.FakeDNS
	if fake {
		servers = append(servers, map[string]interface{}{
			"type":        "fakeip",
			"tag":         "dns-fake",
			"inet4_range": "198.18.0.0/15",
			"inet6_range": "fc00::/18",
		})
		dnsRules = append(dnsRules, map[string]interface{}{"inbound": "tun-in", "server": "dns-fake"})
	}

	// `independent_cache` is gone in 1.14 (1.16 removes the option): the shared
	// cache is the only behaviour left, so the key is simply not written.
	dns := map[string]interface{}{
		"servers": servers,
		"rules":   dnsRules,
	}
	// The route's default resolver names this one, unless the reader brings a DNS
	// object of their own (below), which replaces the servers wholesale.
	b.defaultResolverServer = "dns-direct"
	// The default address family of an answer. sing-box returns A *and* AAAA unless
	// something says otherwise, and on a network with no IPv6 the AAAA half is a promise
	// the machine cannot keep — the caller then dials an address the routing table has no
	// path to, which is the "unreachable network" line in the core log. So ipv4_only when
	// the measurement above says there is no IPv6, and the field left alone when there is,
	// where both families stay available and the reader gets the v6 half of what they
	// asked for. A per-rule strategy from the settings still wins: it is written on the
	// rule rather than here, and a reader's own DNS object replaces this map entirely.
	if !b.forTest && !b.ipv6Usable() {
		dns["strategy"] = "ipv4_only"
	}
	if b.s.Routing().UseDNSObject && b.s.Routing().DNSObject != "" {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(b.s.Routing().DNSObject), &obj) == nil {
			dns = obj
			// The reader's own DNS object replaces the servers this builder
			// wrote, so the route's default resolver has to name a server that
			// is in it. "dns-direct" is a tag of ours, and the core answered
			// "default domain resolver not found: dns-direct" at startup —
			// refusing the whole config over a tag that was never there.
			b.defaultResolverServer = firstDNSTag(obj)
		}
	}
	return dns
}

// firstDNSTag returns the tag of the first server in a DNS object, or "" when it
// carries none.
func firstDNSTag(dns map[string]interface{}) string {
	servers, _ := dns["servers"].([]interface{})
	for _, s := range servers {
		m, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		if tag, ok := m["tag"].(string); ok && tag != "" {
			return tag
		}
	}
	return ""
}

// tunInbound builds the tun inbound used in VPN mode.
//
// Addresses match NekoRay's (db/ConfigBuilder.cpp: inet4_address 172.19.0.1/28
// and the optional fdfe:dcba:9876::1/126), written in the form sing-box 1.10
// expects.
const (
	vpnTunIPv4 = "172.19.0.1/28"
	vpnTunIPv6 = "fdfe:dcba:9876::1/126"
)

// vpnTunMinIPv6MTU is the smallest MTU an interface carrying an IPv6 address can be
// given.
//
// IPv6 requires a link MTU of at least 1280, and Windows enforces that where the MTU is
// first handed to the stack: SetIpInterfaceEntry on the interface's IPv6 row rejects
// anything smaller with ERROR_INVALID_PARAMETER. The tun inbound carries one MTU for both
// address families, so a smaller value turns into
//
//	start inbound/tun[tun-in]: configure tun interface: set ipv6 options: The parameter is incorrect.
//
// and the whole inbound fails to start — no adapter, no routes, no TUN mode. The IPv4
// row takes 1000 without complaint, so the failure appears only once IPv6 is switched on,
// which is what makes it read as a driver fault rather than as a setting that cannot be
// honoured.
const vpnTunMinIPv6MTU = 1280

// TunMTU returns the MTU the tun inbound is actually built with: the configured one,
// except where the IPv6 address the same inbound declares makes that value impossible
// (see vpnTunMinIPv6MTU).
//
// The reader's number cannot be honoured in that one case, so the smallest one that can
// is used instead of failing the whole mode; the settings card applies the same floor, so
// the number shown there is the number the core is given.
func TunMTU(s *setting.Setting) int {
	mtu := s.VPNMTU
	if mtu <= 0 {
		// A settings file written before the field existed, or one edited by hand: the
		// core's own default is what such a file means.
		mtu = 9000
	}
	if s.VPNIPv6 && mtu < vpnTunMinIPv6MTU {
		return vpnTunMinIPv6MTU
	}
	return mtu
}

// ipv6Usable reports whether the config may hand out IPv6 at all: the measurement when
// automatic detection is on, and always yes when it is off.
//
// The switch on the settings card decides which of the two answers. Detection off means
// the reader has taken the question back — nothing here measures and nothing is
// substituted: no ipv4_only strategy, no IPv6-literal resolver swapped for another, the
// TUN address list exactly as the TUN switch says. Detection on means the measurement
// has the last word, because it is the only one that knows whether this machine's
// network can carry what an AAAA record would hand out.
func (b *builder) ipv6Usable() bool {
	return !b.s.IPv6AutoDetect || netx.HasIPv6()
}

// tunIPv6 reports whether the tun adapter should be given an IPv6 address: the reader's
// switch, and — when automatic detection is on — a network that can actually carry it.
//
// The switch alone answers one half: whether v6 is wanted here. The other half is the
// network's, and it is asked only when detection is on (see ipv6Usable), so a reader who
// turned detection off gets the address their switch asks for whether or not the
// measurement would have agreed, and a reader who left it on gets neither an adapter
// IPv6 address they cannot use nor a v6 half of an answer that will not connect.
//
// The MTU floor in TunMTU stays tied to the switch rather than to this function: the
// settings card draws its minimum from the switch too, and a floor that followed the
// network would show one number and give the core another.
func tunIPv6(s *setting.Setting) bool {
	if !s.VPNIPv6 {
		return false
	}
	if !s.IPv6AutoDetect {
		return true
	}
	return netx.HasIPv6()
}

// TunAdapterName is the adapter's network connection name — what Windows shows in
// 网络连接. It is the program's name rather than a slug, because the reader has to
// recognise it as belonging to this program, where "neko-tun" said only that it was
// a tunnel.
//
// Exported because the name is how the app finds the adapter again once it is up:
// turning the capture off and on is a matter of taking and releasing its routes,
// and both need to address the interface the core built (see internal/tunfast).
//
// The macOS name is utun9: the kernel names utun devices itself and sing-box takes
// the number it is given, so there is nothing there to name.
func TunAdapterName() string {
	if runtime.GOOS == "darwin" {
		return "utun9"
	}
	return "EagleTun"
}

// tunInbound writes the TUN inbound. interface_name is what Windows shows in 网络连接
// as the adapter's connection name, so it is the program's name rather than a slug:
// the reader recognises "EagleTun" as belonging to this program, where "neko-tun"
// said only that it was a tunnel.
//
// autoRoute is passed in rather than fixed here because the two callers want
// opposite things. The single-process shape (BuildVPNConfig) has no one else to
// install routes, so it asks the core to do it. The split shape (BuildTunFront)
// does not: its adapter outlives the mode switch, so the routes have to be
// something the app can put up and take down without touching the device — and a
// core that wrote them at start-up would put the machine behind a tunnel with
// nothing listening on the far side of it for as long as the warm-up lasts.
func (b *builder) tunInbound(autoRoute bool) map[string]interface{} {
	s := b.s
	tunName := TunAdapterName()
	stacks := []string{"gvisor", "system", "mixed"}
	idx := s.VPNImplementation
	if idx < 0 || idx >= len(stacks) {
		idx = 0
	}
	in := map[string]interface{}{
		"tag":                      "tun-in",
		"type":                     "tun",
		"interface_name":           tunName,
		"auto_route":               autoRoute,
		"endpoint_independent_nat": true,
		"mtu":                      TunMTU(s),
		"stack":                    stacks[idx],
		"strict_route":             s.VPNStrictRoute,
		// sing-box 1.10 merged inet4_address / inet6_address into a single
		// `address` list, and the old pair is gone in 1.12 — the merged form is
		// what the bundled 1.14 core wants.
		"address": []string{vpnTunIPv4},
	}
	if tunIPv6(s) {
		in["address"] = []string{vpnTunIPv4, vpnTunIPv6}
	}
	b.noteSniffing("tun-in")
	return in
}

// BuildTunFront produces the configuration for the standalone TUN front-end process: a
// tun inbound and one socks outbound pointed at the main core's inbound.
//
// It is deliberately the smallest config in this package. The front-end's whole job is to
// hold the adapter and forward everything; every decision about where a connection goes
// is made by the main core behind that socks hop. And it is deliberately fixed — it
// depends on exactly one thing, the main core's inbound port, which is what lets the
// front-end stay up while nodes, profiles and outbound modes change around it. (Compare
// BuildVPNConfig, whose output carries the node itself and therefore has to be rebuilt —
// and whose process restart takes the adapter down with it.)
func BuildTunFront(s *setting.Setting) map[string]interface{} {
	b := &builder{s: s}
	port := s.InboundSocksPort
	if port <= 0 {
		port = 2080
	}
	host := strings.TrimSpace(s.InboundAddress)
	// Addresses like 0.0.0.0 are for other machines; the front-end process is this
	// machine, and the loopback is the one path to it that cannot be routed away.
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	out := map[string]interface{}{
		"type":        "socks",
		"tag":         "proxy",
		"server":      host,
		"server_port": port,
	}
	// The credential rides with the outbound. The main core's mixed inbound carries
	// `users` whenever the reader turned authentication on (see buildSingBox), and a
	// socks client that offers nothing is refused at the handshake — which in this
	// shape means every packet the adapter captures dies on the hop: TUN reads as on,
	// and the machine has no internet at all.
	if a := s.InboundAuth; a != nil && a.NeedAuth() {
		out["username"] = a.Username
		out["password"] = a.Password
	}
	return map[string]interface{}{
		"log": map[string]interface{}{
			"level":     s.LogLevel,
			"timestamp": true,
		},
		// A dns section whose server is never dialled, and that is the point. The machine's
		// names arrive here: the adapter's resolver is an on-link peer address (see
		// tunfast.SelfDNS), so every query enters this process and its DNS module answers
		// it. With no dns section that module uses the LOCAL transport — the system's own
		// resolver, which this process reaches outside its own routes — and the machine was
		// therefore resolving through the ISP's plain-text DNS while the tunnel stood there
		// looking healthy: forged answers for blocked names (www.google.com answering with
		// a Facebook address is the shape of it), a full-fidelity leak of every name asked
		// for, and none of the profile's own DNS policy involved.
		//
		// An explicit server with a detour takes the hop instead: the query is handed to
		// the main core over the socks hop, whose own route intercepts it by port and
		// answers it from the profile — hijack-dns, the split, the resolvers, all of it
		// (see buildRoute and internal/dnsstub).
		//
		// The detour is not decoration. Without it the DNS module dials its server with
		// this process's own egress — the physical interface, outside the tunnel it
		// serves — and the measurement is there: the query left unproxied for an address
		// that answers nothing from here, no line was logged by the DNS module at all,
		// and every name on the machine timed out while the adapter looked healthy.
		"dns": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{
					"type":   "udp",
					"server": tunFrontDNSDestination,
					"detour": "proxy",
				},
			},
		},
		"inbounds":  []interface{}{b.tunInbound(false)},
		"outbounds": []interface{}{out},
		"route": map[string]interface{}{
			"final":                 "proxy",
			"auto_detect_interface": true,
		},
	}
}

// tunFrontDNSDestination is the address the front-end's DNS queries are addressed to, and
// it is a destination rather than a resolver: the hops it travels are the detour above
// (this process → the main core) and the core's own hijack by port (→ the core's DNS
// module), so no resolver at this address is ever asked. A literal rather than the reader's
// own resolver so that a core which is down cannot turn this into a query sent somewhere
// real: with no core there is no hop, the query fails, and the machine gets no answer
// rather than a plain-text one from the ISP — which is the honest shape for a TUN whose
// engine is not running.
const tunFrontDNSDestination = "8.8.8.8"

// noteSniffing records that an inbound wants its connections sniffed, and how.
// These flags used to be written onto the inbound itself, which sing-box 1.13
// no longer accepts; buildRoute emits them as route actions instead.
func (b *builder) noteSniffing(tag string) {
	if b.s.Routing().SniffingMode == setting.SniffingDisable {
		return
	}
	b.sniffInbounds = append(b.sniffInbounds, tag)
	if b.s.Routing().SniffingMode == setting.SniffingForDestination {
		b.sniffForDestination = true
	}
	// the inbound's own domain_strategy became a `resolve` action with the same
	// set of values
	if b.resolveStrategy == "" && setting.IsValidStrategy(b.s.Routing().DomainStrategy) {
		b.resolveStrategy = b.s.Routing().DomainStrategy
	}
}

func (b *builder) buildRoute(tagProxy string) map[string]interface{} {
	// The rules that have to come first of all: sniffing and resolution. Every
	// rule below them already sees a sniffed domain and a resolved destination,
	// and the reader's own rules go here too — after the sniff, not before it
	// (see the note where they are put together).
	var head []interface{}
	var rules []interface{}

	// Sniffing and domain resolution lived on the inbound until 1.13; they are
	// route actions now, and they go first so every rule below already sees a
	// sniffed domain and a resolved destination.
	if len(b.sniffInbounds) > 0 {
		// No override flag: the route-action form of sniffing does not take one, and a
		// config carrying it does not decode at all — the core exits on startup with
		// `route.rules[0].override_destination: unknown field`, which reads to a reader as
		// "the node will not start" with nothing pointing at the cause. The intent it stood
		// for (sniff for the destination rather than only for routing) is carried by the
		// resolve action below.
		head = append(head, map[string]interface{}{
			"inbound": b.sniffInbounds,
			"action":  "sniff",
		})
	}
	// The resolve action, when a strategy is configured. The two sniffing modes reach it
	// differently: sniffing for routing resolves the destination, because the rules below
	// match on addresses as well as on domains; sniffing for the destination does not,
	// because the point of that mode is to hand the domain to the outbound and let the far
	// end resolve it. It is this that carries the intent now.
	if b.resolveStrategy != "" && !b.sniffForDestination {
		head = append(head, map[string]interface{}{
			"action":   "resolve",
			"strategy": b.resolveStrategy,
		})
	}
	if !b.forTest {
		// `dns-out` was a special outbound (removed in 1.13); intercepting DNS is the
		// hijack-dns action now.
		//
		// Matched by PORT, not by protocol, and that distinction decides whether the
		// machine's names are resolved by this config at all. `protocol: dns` is a
		// sniffer's answer, and the sniffer recognises plain DNS over UDP by its port —
		// but a rule that names the protocol was measured to let the front-end's queries
		// fall straight through: the TUN process hands them over as ordinary UDP packets
		// addressed to a resolver (see BuildTunFront), nothing intercepted them, and each
		// one left for the address it named — which for the machine's own resolver is an
		// on-link peer inside the tunnel, so the answer never came and every name on the
		// machine timed out while the adapter looked healthy.
		//
		// By port, the same query is intercepted and answered from this config's own dns
		// servers: the split, the resolvers and the app's forwarder all apply to it.
		// Verified by hand against the bundled core, with two sing-box instances standing
		// in for the two processes (the front-end's hop and this rule).
		rules = append(rules, map[string]interface{}{
			"port":   []interface{}{53},
			"action": "hijack-dns",
		})
	}

	rules = append(rules, b.chainRules...)

	addRuleRoute := func(list []string, isIP bool, outbound, action string) {
		rule := b.makeRule(list, isIP)
		if rule == nil {
			return
		}
		if outbound != "" {
			rule["outbound"] = outbound
		}
		if action != "" {
			rule["action"] = action
		}
		rules = append(rules, rule)
	}
	// Windows' own connectivity probe, on the machine's uplink in every mode.
	//
	// Every adapter is asked whether it has internet by resolving these names and
	// fetching one fixed page; the answer is what writes "无法连接到 Internet" under
	// the adapter in Settings and over the tray icon. Sent through a node, the probe
	// pays the node's first-packet time for a request Windows gives a couple of
	// seconds — and the probe is judged by the page's content, which a proxy that
	// answers with its own error page fails too. A working tunnel therefore reads as
	// a dead network. Ahead of the block layer as well: a block list that swallowed
	// the probe would make the machine report itself offline.
	if !b.forTest {
		rules = append(rules, map[string]interface{}{
			"domain_suffix": []interface{}{"msftconnecttest.com", "msftncsi.com"},
			"outbound":      "bypass",
		})
	}
	// The block layer: the profile's block lists refuse their matches in every mode,
	// ahead of whatever the mode decides about the survivors. It sits here — after the
	// structural rules, before the split itself — because a blocked connection is blocked
	// before any question of direction arises, and because this layer has to be one of
	// the things a non-rule mode keeps: dropping it there is why an ad list went dead the
	// moment the status bar said "proxy".
	//
	// Skipped for a test config, where the block lists would only add noise to what the
	// test measures.
	if !b.forTest {
		addRuleRoute(b.domainListBlock, false, "", "reject")
		addRuleRoute(b.ipListBlock, true, "", "reject")
	}
	// The status bar's switch is applied here rather than by rewriting the profile:
	// "direct" and "global" are not different routes, they are this route with the split
	// left out. What goes is the per-list split rules and the profile's own custom rules;
	// what stays is everything structural — sniffing and resolution (the rules below have
	// to see a domain), the DNS hijack, the chain rules a relayed profile needs, the
	// block layer above, the multicast/NetBIOS rejects (which are about not answering the
	// network rather than about choosing an outbound), and the VPN bypass rules further
	// down: "绕过这些进程" is a statement about a process, not about the split, and it
	// means the same thing in every mode.
	mode := b.s.EgressMode
	// Migration: "block" used to be a fourth mode — one rule that answered every
	// connection. Its real job, refusing the block lists ahead of whatever the mode
	// decides, is what the block layer above now does in every mode, so a settings file
	// still carrying the old value lands in the mode it always behaved as.
	if mode == "block" {
		mode = "rule"
	}
	split := mode == "" || mode == "rule"
	// The exit probe's hosts pinned to the proxy outbound, ahead of the split. The status
	// bar asks these hosts what address this machine is seen as, and under a
	// domestic-direct profile one that resolves into a domestic address — ip.3322.net
	// resolves to CN — is sent straight out by the geoip rule and answers with the
	// reader's own address, the one number the probe exists to rule out. Skipped outside
	// the rule mode, where direct and global already fix the direction for everything.
	if !b.forTest && split {
		var probeHosts []interface{}
		for _, h := range util.ProbeHosts() {
			probeHosts = append(probeHosts, h)
		}
		if len(probeHosts) > 0 {
			rules = append(rules, map[string]interface{}{
				"domain":   probeHosts,
				"outbound": tagProxy,
			})
		}
	}
	if split {
		addRuleRoute(b.domainListRemote, false, tagProxy, "")
		addRuleRoute(b.ipListRemote, true, tagProxy, "")
		addRuleRoute(b.domainListDirect, false, "bypass", "")
		addRuleRoute(b.ipListDirect, true, "bypass", "")
	}

	rules = append(rules,
		map[string]interface{}{"network": "udp", "port": []interface{}{135, 137, 138, 139, 5353}, "action": "reject"},
		map[string]interface{}{"ip_cidr": []interface{}{"224.0.0.0/3", "ff00::/8"}, "action": "reject"},
		map[string]interface{}{"source_ip_cidr": []interface{}{"224.0.0.0/3", "ff00::/8"}, "action": "reject"},
	)

	// VPN user rules (process / cidr based bypass)
	if b.vpn {
		matchOut := "bypass"
		if b.s.VPNRuleWhite {
			matchOut = "proxy"
		}
		if p := strings.TrimSpace(b.s.VPNRuleProcess); p != "" {
			rules = append(rules, map[string]interface{}{
				"outbound":     matchOut,
				"process_name": splitLinesIface(p),
			})
		}
		if c := strings.TrimSpace(b.s.VPNRuleCIDR); c != "" {
			rules = append(rules, map[string]interface{}{
				"outbound": matchOut,
				"ip_cidr":  splitLinesIface(c),
			})
		}
	}

	// The custom rules take priority over the split — but they cannot come
	// before the sniff.
	//
	// sing-box only knows the domain after the sniff action has run, and the
	// rules that stood in front of the sniff rule are passed over and never
	// tried again: a domain rule in front of it matched nothing at all. The
	// reader's own rules were therefore dead — every one of them — and the
	// symptom was "my rules do nothing", with the core's own log saying the
	// connection went to the fallback outbound. Measured against the bundled
	// core; see TestRuleOrderSniffVsCustom.
	var customRules []interface{}
	if !b.forTest && split {
		customRules = append(customRules, extractRules(b.s.Routing().Custom)...)
		customRules = append(customRules, extractRules(b.s.CustomRouteGlobal)...)
	}
	finalRules := make([]interface{}, 0, len(head)+len(customRules)+len(rules))
	finalRules = append(finalRules, head...)
	finalRules = append(finalRules, customRules...)
	finalRules = append(finalRules, rules...)

	// Always on, not only for the tun shapes. With a tun front-end in the picture, the
	// main core's own outbound connections — to nodes, to DNS upstreams — must bind to
	// the physical interface: unbound, the machine's routing table sends them into the
	// tun, the front-end forwards them back to this very core's inbound, and every
	// connection dies in that loop while the adapter sits there fully up. Without a
	// front-end, binding to the default interface is what the routing table would have
	// chosen anyway, so this costs the port-mode shape nothing.
	route := map[string]interface{}{
		"rules":                 finalRules,
		"auto_detect_interface": true,
	}
	if !b.forTest {
		final := b.s.Routing().DefOutbound
		// The other modes are one word each: everything out, or everything through.
		// The profile's own default is what "rule" means, and it is the one that stays
		// when the switch is left alone. A legacy "block" was folded into "rule" above,
		// and the final outbound is where the block layer's survivors end up.
		switch mode {
		case "direct":
			final = "bypass"
		case "global":
			final = "proxy"
		}
		// The profile's "proxy" is a word for the selector; a config that carries no
		// selector — a single node whose group was not found, an empty candidate list —
		// has only the plain chain under its own tag. Writing the word unchanged would
		// name an outbound that does not exist, and the core would reject the config at
		// startup with "default outbound not found: proxy" and restart in a cycle. The
		// chain's tag is what "proxy" means there.
		if final == "proxy" && tagProxy != "proxy" {
			final = tagProxy
		}
		// And a name that is no outbound at all is refused the same way. "block"
		// was nekoray's third fallback — the special block outbound — and
		// sing-box removed it in 1.12: the core answered "default outbound not
		// found: block" at startup and restarted in a cycle, so a profile
		// migrated from nekoray could not start at all. The block layer above
		// refuses the block lists in every mode, so what is left of "block" is
		// the proxy.
		if final == "block" {
			final = tagProxy
		}
		// A TUN whitelist run answers "bypass" for everything it matches, so it is
		// already a mode of its own; it keeps the last word.
		if b.vpn && b.s.VPNRuleWhite {
			final = "bypass"
		}
		route["final"] = final
	}
	// Domain resolution: this replaces both the old DNS rule
	// {"outbound":"any","server":"dns-direct"} and the per-outbound
	// domain_strategy field, neither of which 1.14 accepts any more.
	//
	// What family it hands out is the same question the DNS module answers (see
	// ipv6Usable): this resolver dials for every outbound — the node's own address
	// above all — and on a machine with no IPv6 route a v6 answer is a dial to an
	// address nothing reaches, which is the "unreachable network" line in the log. So
	// the reader's choice stands when there is one, and the empty default becomes
	// ipv4_only only where v6 cannot work; where it can — or where detection is off and
	// the reader has taken that question back — nothing is written and both families
	// stay on the table.
	resolver := map[string]interface{}{"server": b.defaultResolverServer}
	switch s := b.s.Routing().OutboundDomainStrategy; {
	case s != "" && setting.IsValidStrategy(s):
		resolver["strategy"] = s
	case !b.forTest && !b.ipv6Usable():
		resolver["strategy"] = "ipv4_only"
	}
	route["default_domain_resolver"] = resolver

	if len(b.ruleSets) > 0 {
		route["rule_set"] = b.ruleSetDefinitions()
	}
	return route
}

// ruleSetDefinitions renders the rule sets collected from geoip:/geosite: rule items.
// Both databases were removed from the core in 1.12; the rule-set form is the
// supported replacement.
//
// A copy kept in the settings' rule-set directory wins over the remote one. These come
// from raw.githubusercontent.com, which is unreachable on many networks, and a rule set
// that fails to download is not an error the core reports — the rule simply never
// matches, which looks exactly like a routing profile that does nothing. Dropping the
// .srs file in there (under the name the remote URL ends in, geosite-cn.srs and the
// like — see addRuleSet) is what makes these rules work offline.
func (b *builder) ruleSetDefinitions() []interface{} {
	tags := make([]string, 0, len(b.ruleSets))
	for tag := range b.ruleSets {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	dir := b.s.RuleSetDir()
	out := make([]interface{}, 0, len(tags))
	for _, tag := range tags {
		if path, ok := localRuleSet(dir, tag); ok {
			out = append(out, map[string]interface{}{
				"type":   "local",
				"tag":    tag,
				"format": "binary",
				"path":   path,
			})
			continue
		}
		out = append(out, map[string]interface{}{
			"type":   "remote",
			"tag":    tag,
			"format": "binary",
			"url":    b.ruleSets[tag],
		})
	}
	return out
}

// localRuleSet reports the path of a rule set kept on disk for this tag, if there is a
// usable one. The name is the tag plus .srs — the same file name the remote URL ends in
// — so a file fetched by hand drops in without being renamed. The path is made absolute
// because the core is not necessarily started in the settings directory.
func localRuleSet(dir, tag string) (string, bool) {
	path := filepath.Join(dir, tag+".srs")
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	if !looksLikeRuleSet(path) {
		return "", false
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path, true
}

// looksLikeRuleSet reports whether the file starts with the binary rule-set header —
// "SRS" and a version byte. A file without it is not a rule set: a .dat from another
// client, an HTML page saved by a browser, a download that stopped halfway. Handing one
// of those to the core keeps the core from starting at all, so it is treated as absent
// and the remote copy is tried instead.
func looksLikeRuleSet(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	return head[0] == 'S' && head[1] == 'R' && head[2] == 'S'
}

// LooksLikeRuleSet reports whether the file at path opens with the binary rule-set
// header. Exported for the one caller outside this package that has to tell a real
// download from a truncated one before putting it where the core will read it: a half-
// written file handed to the core stops it from starting at all (see localRuleSet).
func LooksLikeRuleSet(path string) bool { return looksLikeRuleSet(path) }

// RuleSets reports every rule set the settings' routing refers to, as tag → remote URL.
//
// The tags are registered while the rules are made (see addRuleSet), so the way to know
// the whole list is to make them — which is what this does, producing no config and
// touching nothing outside the builder. The settings page needs that list on its own:
// a tag whose remote cannot be reached is not an error the core reports, it is a rule
// that quietly never matches, and the only way to see that from the window is to hold
// this list next to the files that are actually on disk.
func RuleSets(s *setting.Setting) map[string]string {
	if s == nil {
		return map[string]string{}
	}
	// The two steps the config builder takes before the list exists, and nothing after
	// them: the profile's lists are parsed into the builder, then the rules are made from
	// them — and making a rule is what registers a rule set (see makeRule and addRuleSet).
	// The config those steps are a part of is thrown away here; only the registry is read.
	b := &builder{s: s}
	b.collectUserRules()
	_ = b.buildRoute("proxy")
	if b.ruleSets == nil {
		return map[string]string{}
	}
	return b.ruleSets
}

func splitLinesIface(s string) []interface{} {
	lines := util.SplitLinesSkipSharp(s, 0)
	out := make([]interface{}, 0, len(lines))
	for _, l := range lines {
		out = append(out, l)
	}
	return out
}

func extractRules(raw string) []interface{} {
	if raw == "" {
		return nil
	}
	var obj struct {
		Rules []interface{} `json:"rules"`
	}
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return nil
	}
	return obj.Rules
}

// makeRule builds a sing-box rule object from a nekoray rule list.
// Returns nil when the list yields no matchers.
//
// geoip:/geosite: items become references to remote rule sets: the core dropped
// both databases in 1.12, and 1.14 answers a geoip matcher with a fatal error,
// so a rule list that used them would otherwise take the whole config down.
func (b *builder) makeRule(list []string, isIP bool) map[string]interface{} {
	rule := map[string]interface{}{}
	var ipCIDR, ruleSet []interface{}
	var domainKeyword, domainSuffix, domainRegex, domainFull []interface{}

	for _, item := range list {
		if isIP {
			if name := strings.TrimPrefix(item, "geoip:"); name != item {
				// addRuleSet answers "" for an empty name ("geoip:", half a
				// keystroke or a truncated copy). Appending that blind put
				// rule_set: [""] in the config with no definition behind it,
				// and sing-box refuses a reference to a rule set that does not
				// exist — the whole config down over one empty item. An empty
				// reference carries no intent worth keeping, so it is dropped.
				if tag := b.addRuleSet("geoip", name); tag != "" {
					ruleSet = append(ruleSet, tag)
				}
			} else {
				ipCIDR = append(ipCIDR, item)
			}
			continue
		}
		switch {
		case strings.HasPrefix(item, "geosite:"):
			if tag := b.addRuleSet("geosite", strings.TrimPrefix(item, "geosite:")); tag != "" {
				ruleSet = append(ruleSet, tag)
			}
		case strings.HasPrefix(item, "full:"):
			domainFull = append(domainFull, strings.ToLower(strings.TrimPrefix(item, "full:")))
		case strings.HasPrefix(item, "domain:"):
			domainSuffix = append(domainSuffix, strings.ToLower(strings.TrimPrefix(item, "domain:")))
		case strings.HasPrefix(item, "regexp:"):
			domainRegex = append(domainRegex, strings.ToLower(strings.TrimPrefix(item, "regexp:")))
		case strings.HasPrefix(item, "keyword:"):
			domainKeyword = append(domainKeyword, strings.ToLower(strings.TrimPrefix(item, "keyword:")))
		default:
			domainSuffix = append(domainSuffix, strings.ToLower(item))
		}
	}

	if isIP {
		if len(ipCIDR) == 0 && len(ruleSet) == 0 {
			return nil
		}
		if len(ipCIDR) > 0 {
			rule["ip_cidr"] = ipCIDR
		}
		if len(ruleSet) > 0 {
			rule["rule_set"] = ruleSet
		}
		return rule
	}
	if len(domainKeyword) == 0 && len(domainSuffix) == 0 && len(domainRegex) == 0 && len(domainFull) == 0 && len(ruleSet) == 0 {
		return nil
	}
	if len(domainFull) > 0 {
		rule["domain"] = domainFull
	}
	if len(domainSuffix) > 0 {
		rule["domain_suffix"] = domainSuffix
	}
	if len(domainKeyword) > 0 {
		rule["domain_keyword"] = domainKeyword
	}
	if len(domainRegex) > 0 {
		rule["domain_regex"] = domainRegex
	}
	if len(ruleSet) > 0 {
		rule["rule_set"] = ruleSet
	}
	return rule
}

// addRuleSet registers a rule set for a geoip:/geosite: item and returns the tag a rule
// references it by. The core fetches and caches the remote form on first use; a file of
// the same name in the settings' rule-set directory is used instead when there is one —
// which is the only way these rules can work on a network that cannot reach
// raw.githubusercontent.com (see ruleSetDefinitions).
func (b *builder) addRuleSet(kind, name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "!"))
	if name == "" {
		return ""
	}
	tag := kind + "-" + name
	if b.ruleSets == nil {
		b.ruleSets = map[string]string{}
	}
	sources := RuleSetSources(tag)
	if len(sources) == 0 {
		return tag
	}
	b.ruleSets[tag] = sources[0]
	return tag
}

// PresetRuleSetTags are the rule sets this app's own presets are written against —
// the bypass profiles' direct lists name one network range and one domain list per
// country (see defaultRoutingWithPreset). They are listed in the rule-set card even
// while no profile refers to them: a rule set the card never shows is a rule set the
// reader cannot fetch in advance, and the presets are the one set the reader should
// not have to discover by first writing a rule that needs them.
func PresetRuleSetTags() []string {
	return []string{"geoip-cn", "geosite-cn", "geoip-ru", "geosite-category-ru", "geoip-us"}
}

// RuleSetSources lists where a rule set's fresh copy can be fetched from, in the order
// they are tried. The first is the source the core itself uses (see addRuleSet, who
// keeps it as the profile's URL); the second is a mirror of the same GitHub content —
// raw.githubusercontent.com is unreachable on many networks, and this is the one spare
// the settings side has when the settings side is the one downloading. A tag that is
// not one of the builder's own (a hand-imported file, say) has no source here at all.
// (The list it feeds is PresetRuleSetTags, which is why the mirror matters there.)
func RuleSetSources(tag string) []string {
	repo := ""
	switch {
	case strings.HasPrefix(tag, "geosite-"):
		repo = "sing-geosite"
	case strings.HasPrefix(tag, "geoip-"):
		repo = "sing-geoip"
	default:
		return nil
	}
	file := tag + ".srs"
	return []string{
		"https://raw.githubusercontent.com/SagerNet/" + repo + "/rule-set/" + file,
		"https://fastly.jsdelivr.net/gh/SagerNet/" + repo + "@rule-set/" + file,
	}
}

func splitComma(s string) []interface{} {
	parts := strings.Split(s, ",")
	out := make([]interface{}, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func before(s, sub string) string {
	if i := strings.Index(s, sub); i >= 0 {
		return s[:i]
	}
	return s
}

func after(s, sub string) string {
	if i := strings.Index(s, sub); i >= 0 {
		return s[i+len(sub):]
	}
	return s
}

var _ = bean.Bean(nil)
