package main

import (
	"context"
	"eagle/internal/autostart"
	"eagle/internal/core"
	"eagle/internal/dnstest"
	"eagle/internal/dnsstub"
	"eagle/internal/setting"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// autostartName is the Run key's value name. It was NekoBox and is Eagle, with the
// old value carried over once (see migrateAutostart) — a registry key left behind
// would start the program a second time at every logon.
const autostartName = "Eagle"

const legacyAutostartName = "NekoBox"

// migrateAutostart moves a run-at-login entry from the old value name to the new one:
// it enables the new name only if the old one was set, then removes the old value, so
// the setting the reader chose survives the rename and nothing is left to fire twice.
//
// Deliberately one-shot and irreversible: the old value is deleted rather than left
// for a rollback build to find, because a leftover entry is exactly what would make
// the next logon launch two copies.
func migrateAutostart() {
	on, err := autostart.Enabled(legacyAutostartName)
	if err != nil || !on {
		return
	}
	if err := autostart.Enable(autostartName, autostart.ExePath()); err != nil {
		return
	}
	_ = autostart.Disable(legacyAutostartName)
}

// GetSettings returns the settings object for the raw editor.
//
// The struct tags carry `omitempty`, so marshalling the value on its own drops
// every option the user has never touched — and an absent key cannot be edited
// at all, which is why the settings page looked like it was missing most of the
// configuration. The response therefore starts from a complete key set derived
// from the type (zero values) and overlays the current values on top, so every
// documented option is present and editable.
func (a *App) GetSettings() map[string]interface{} {
	if a.curSetting() == nil {
		return map[string]interface{}{}
	}
	out := settingKeys(reflect.TypeOf(*a.curSetting()))
	data, err := json.Marshal(a.curSetting())
	if err != nil {
		return out
	}
	var cur map[string]interface{}
	if err := json.Unmarshal(data, &cur); err != nil {
		return out
	}
	for k, v := range cur {
		out[k] = v
	}
	return out
}

// settingKeys lists every JSON field name of a settings struct with its zero
// value, recursing into nested structs and struct pointers so groups like
// extraCore arrive as an object rather than a null the editor could not write
// back. Unexported fields (routing, baseDir) have no JSON contract and are
// skipped.
func settingKeys(t reflect.Type) map[string]interface{} {
	out := map[string]interface{}{}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		switch {
		case f.Type.Kind() == reflect.Struct:
			out[name] = settingKeys(f.Type)
		case f.Type.Kind() == reflect.Ptr && f.Type.Elem().Kind() == reflect.Struct:
			out[name] = settingKeys(f.Type.Elem())
		case f.Type.Kind() == reflect.Slice:
			// an empty slice, not the nil zero: nil marshals to null, and the
			// editor would then treat the field as free text and fail to
			// unmarshal it back into a list
			out[name] = reflect.MakeSlice(f.Type, 0, 0).Interface()
		case f.Type.Kind() == reflect.Map:
			out[name] = reflect.MakeMap(f.Type).Interface()
		default:
			out[name] = reflect.Zero(f.Type).Interface()
		}
	}
	return out
}

// UpdateSettings merges modified settings and persists them.
func (a *App) UpdateSettings(in setting.Setting) error {
	// Replace is a read of the live setting — it clones it and decodes the
	// incoming fields onto that clone — so it belongs inside applyMu as much as
	// the save does: a tray press that landed between the two used to write its
	// field onto an object this one had already read, and the save below then
	// carried the old value back.
	a.applyMu.Lock()
	cur := a.curSetting()
	if cur == nil {
		a.applyMu.Unlock()
		return fmt.Errorf("settings not loaded")
	}
	next, err := cur.Replace(&in)
	if err == nil {
		err = a.applySettingLocked(next)
	}
	a.applyMu.Unlock()
	if err != nil {
		return err
	}
	a.applyHotkeys()
	return nil
}

// applySetting persists next and makes it the live settings object. The pointer
// is swapped rather than mutated in place: goroutines that already hold the old
// value (traffic loop, speed tests) keep reading a consistent snapshot.
func (a *App) applySetting(next *setting.Setting) error {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	return a.applySettingLocked(next)
}

// mutateSetting is the form for a caller that has something to change. The
// read, the copy, the save and the swap are one unit under applyMu — which is
// the half applySetting on its own cannot cover: a caller that copies the live
// setting first and saves afterwards reads outside the lock, and two of those
// landing together each carried the other's field back to what it had been
// (InheritRuntime protects only the two runtime mode flags). The closure runs
// on the copy, and nothing is written if it returns an error.
func (a *App) mutateSetting(fn func(*setting.Setting) error) error {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	cur := a.curSetting()
	if cur == nil {
		return fmt.Errorf("settings not loaded")
	}
	next := cur.Clone()
	if err := fn(next); err != nil {
		return err
	}
	return a.applySettingLocked(next)
}

// applySettingLocked persists next and makes it the live settings object. The
// caller holds applyMu.
func (a *App) applySettingLocked(next *setting.Setting) error {
	// The two runtime mode flags live on the setting object, and the object is
	// replaced right here. The copy next was made of can already be stale: a
	// tray or hotkey toggle writes the flag on whichever object is live at that
	// moment, and if that lands between the copy and the swap the flag is
	// dropped — the machine is in TUN mode while the app believes it is not, and
	// the exit path, the DNS takeover and the tray all read that belief. So
	// the flags are taken from the object that is live at the swap, not from
	// the copy.
	next.InheritRuntime(a.curSetting())
	if err := next.Save(); err != nil {
		return err
	}
	a.setSetting(next)
	if a.curController() != nil {
		a.curController().SetSetting(next)
	}
	// The forwarder follows the profile the core is not reading: another active routing
	// profile is another pair of resolvers, and this is one of the doors they come in by
	// (see app_dns.go).
	a.syncDNSStub()
	return nil
}

// ListRoutings returns available routing profile names.
func (a *App) ListRoutings() []string {
	if a.curSetting() == nil {
		return []string{}
	}
	return a.curSetting().ListRoutings()
}

// GetActiveRouting returns the active routing profile name.
func (a *App) GetActiveRouting() string {
	if a.curSetting() == nil {
		return ""
	}
	return a.curSetting().ActiveRouting
}

// GetRouting returns a routing profile by name.
func (a *App) GetRouting(name string) (*setting.Routing, error) {
	return setting.LoadRouting(a.curDataDir(), name)
}

// SaveRouting persists a routing profile.
//
// The forwarder is pointed at the saved resolvers straight away, and that is what makes the
// DNS card's smart pick live: the pick writes here, the core's config never changes (it
// names the forwarder's ports), and the chosen resolver is in force for the very next
// query. Nothing is restarted.
func (a *App) SaveRouting(name string, r setting.Routing) error {
	if a.curSetting() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := a.curSetting().SaveRouting(name, &r); err != nil {
		return err
	}
	a.syncDNSStub()
	return nil
}

// ApplyRouting saves a routing profile and puts it to work at once, for the reader with
// a node running. sing-box has no hot reload of a routing config — its clash API rewrites
// ports and node choices, not rules — so the config is rebuilt against the saved file one
// way or another; what this buys is that the rebuild happens on the save, without the
// reader planting a restart themselves.
//
// The two modes rebuild differently, by what each one would otherwise cost:
//
//   - under the system proxy, the core alone goes down and back up: a second's gap in
//     the port it serves, and Windows never hears about it (the proxy entry keeps
//     pointing where it already points, and the restarted core is serving that port
//     again before the first failed request is even re-tried);
//   - under TUN, only the main core is rebuilt and the front-end is left standing — the
//     two-process split is exactly what makes this legal (the adapter belongs to the
//     front-end, the rules belong to the main core) — so a routing save does not put
//     a UAC prompt back in the reader's way and does not bounce the network adapter.
func (a *App) ApplyRouting(name string, r setting.Routing) error {
	if a.curSetting() == nil {
		return fmt.Errorf("settings not loaded")
	}
	// What the file held before this save. A save that moved nothing but the
	// two resolver addresses needs no rebuild at all: the core names the
	// forwarder's ports — which never change — and which resolver stands behind
	// a port is the forwarder's memory (syncDNSStub below). Without this, the
	// DNS card's own debounced write (600ms after the last keystroke) stopped
	// the core under a running node on every edit: a dropped download per
	// character, and a toast saying the core had been restarted for a change
	// that never needed it.
	prev, prevErr := setting.LoadRouting(a.curDataDir(), name)
	if err := a.curSetting().SaveRouting(name, &r); err != nil {
		return err
	}
	// The resolvers are the one part of this that does not need the rebuild below: the core
	// asks the forwarder, and the forwarder's upstream changed here and now (see
	// app_dns.go). The rest — rules, lists, the fallback outbound — is still the core's
	// config and still needs the restart.
	a.syncDNSStub()
	if prevErr == nil && prev != nil && sameRoutingExceptDNS(*prev, r) {
		// Only the resolvers moved, and the forwarder is already asking the new
		// ones: the core has nothing to re-read. Said in the log rather than as
		// a toast — the card writes on a 600ms debounce, and a message per
		// pause is noise over a change that has already taken effect.
		a.logf("[route] " + name + ": 仅解析器变化，转发器已即时切换（不重启核心）")
		return nil
	}
	running := a.RunningProfile()
	if running == core.StoppedID {
		// Nothing is carrying traffic: the file alone is the whole change, applied
		// whenever the next start reads it.
		return nil
	}
	// The rebuild below stops the core and starts it again on this id; an id a
	// subscription update has already taken out of the list would come back as
	// "profile N not found" with the tunnel down — the answer every other
	// rebuild path (start, restart, mode switch) refuses to produce, and this
	// one refuses the same way, before anything is torn down.
	if a.curMgr().SnapshotProfile(running) == nil {
		return fmt.Errorf("运行中的节点已不在列表中（可能已被订阅更新替换），请重新选择节点")
	}
	defer a.emitRunningState()
	return a.rebuildCoreForRouting(running)
}

// sameRoutingExceptDNS reports whether two profiles differ only in the two
// resolver addresses — the change a running core never has to see, because its
// config names the forwarder's ports rather than the resolvers.
//
// Only while the forwarder really is serving an address, though: the builder
// falls back to writing the reader's own address into the core's config when a
// side has no endpoint (no usable upstream, no listener, no path through the
// core — see dnsstub.Endpoints and upstreamProblem), and in that session the
// core DID read the address. Rewriting the file then, and returning "the
// forwarder switched", left the core on the old resolver with nothing in the
// log to say why — the very bug this shortcut was meant to avoid, mirrored.
// A side without an endpoint therefore forces the rebuild.
func sameRoutingExceptDNS(a, b setting.Routing) bool {
	remote, direct := dnsstub.Endpoints()
	if remote == "" || direct == "" {
		return false
	}
	a.RemoteDNS, b.RemoteDNS = "", ""
	a.DirectDNS, b.DirectDNS = "", ""
	// The smart-pick switches live in the same profile and the core never reads
	// them — they only decide whether the window tests resolvers and when it
	// writes a winner back. Flipping one is not a config change, but the card
	// writes through ApplyRouting like every other field, so without this the
	// switch stopped the core under a running node (a dropped download per
	// press, and a toast about a restart for something the core never saw).
	a.DNSSmartSelect, b.DNSSmartSelect = false, false
	a.DNSSmartSwitch, b.DNSSmartSwitch = false, false
	a.DNSSmartMinutes, b.DNSSmartMinutes = 0, 0
	return reflect.DeepEqual(a, b)
}

// rebuildCoreForRouting restarts the running node so a routing change takes
// effect: sing-box reads its config once, and the rules, lists and outbound
// fallback live in it.
//
// The same seat every other start and stop takes (see startStopMu): the tray, a
// hotkey and the crash supervisor all queue there. A routing change that
// stopped and started the core outside it could interleave with one of them —
// and the core then answered "a profile is already running" for a switch the
// reader never asked for.
func (a *App) rebuildCoreForRouting(running int) error {
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	// Stopping the core directly rather than through StopProfile: that one would strip
	// the Windows proxy entry under the system-proxy shape and tear the front-end down
	// under TUN, and neither belongs to a config change. The stop is expected (no crash
	// being invented from it) and the traffic counters are flushed, like any stop.
	a.markExpectedStop()
	a.flushTraffic()
	if err := a.curController().Stop(); err != nil {
		return err
	}
	if a.curSetting().VPNMode() {
		if err := a.startVPNExternal(running, nil); err != nil {
			return err
		}
		// Same memory the direct branch pays below: a rebuild under TUN is still a
		// start as far as "记住上次节点" is concerned.
		a.rememberStarted(running)
		a.notifyMessage("路由已更新（已重建核心，网卡未动）")
		return nil
	}
	if err := a.curController().Start(running); err != nil {
		return err
	}
	a.rememberStarted(running)
	a.notifyMessage("路由已更新（已自动重启核心）")
	return nil
}

// TestDNS measures how long each of these resolvers takes to answer. It is what the DNS
// card's smart pick is built on: the frontend hands over the candidates, keeps the fastest
// of each list and writes it into the field it belongs to. Measuring here rather than asking
// the core, because the core's own API answers about the resolver it is already configured
// with and cannot be asked about one it has never seen.
//
// viaProxy is the foreign side of that card. Those resolvers are reached through the running
// core — their server carries detour: proxy — so with nothing running there is no path to
// measure over, which is an error rather than an empty list: the caller has to know that
// side was skipped instead of quietly getting no answer.
func (a *App) TestDNS(servers []string, viaProxy bool) ([]dnstest.Result, error) {
	if a.curSetting() == nil {
		return nil, fmt.Errorf("settings not loaded")
	}
	opts := dnstest.Options{Timeout: 1500 * time.Millisecond, Parallel: 8, Attempts: 3}
	if viaProxy {
		if a.RunningProfile() == core.StoppedID {
			return nil, fmt.Errorf("没有正在运行的节点：这一侧的解析器要经它才能测")
		}
		// Through inboundProxyURL rather than a JoinHostPort built here: the
		// listen field is a listen address, and a wildcard in it — 0.0.0.0, ::,
		// [::] — is not a dial target ("every interface here", which reached
		// nothing on some systems and the wrong thing on others), while a port
		// out of range made the measurement fail without ever saying why. The
		// one builder every other consumer of this inbound goes through
		// normalizes both (see inboundProxyURL).
		u := a.inboundProxyURL()
		if u == nil {
			return nil, fmt.Errorf("本地入站端口未设置或不可用")
		}
		opts.Proxy = u
	} else {
		// The domestic side is measured the way it is really reached: straight
		// out of the machine's own interface, past the tunnel (see
		// dnstest.Options.InterfaceIndex). A probe that took the ordinary route
		// would be captured by the tunnel, handed to the core, and answered by
		// the core's own rules — a measurement of the wrong path, and one that
		// fails for reasons that have nothing to do with the resolver.
		//
		// The host name of a resolver is looked up with the machine's own
		// resolvers from before the takeover, for the same reason: asking the
		// machine's current resolver would be asking the forwarder that is
		// serving this very question.
		opts.InterfaceIndex = a.egressIndex()
		opts.Bootstrap = a.bootstrapResolvers()
	}
	ctx := a.curCtx()
	if ctx == nil {
		ctx = context.Background()
	}
	return dnstest.Run(ctx, servers, opts), nil
}

// CreateRouting creates a routing profile from a preset (0=Default, 1=bypass CN,
// 2=bypass RU, 3=bypass US).
func (a *App) CreateRouting(name string, preset int) error {
	if a.curSetting() == nil {
		return fmt.Errorf("settings not loaded")
	}
	return a.curSetting().SaveRouting(name, setting.PresetRouting(preset))
}

// DeleteRouting removes a routing profile.
func (a *App) DeleteRouting(name string) error {
	if a.curSetting() == nil {
		return fmt.Errorf("settings not loaded")
	}
	return a.curSetting().DeleteRouting(name)
}

// SetActiveRouting switches the active routing profile.
func (a *App) SetActiveRouting(name string) error {
	// WithActiveRouting reads the live setting as well as the routing file, so
	// it is part of the same unit as the save (see mutateSetting): a save
	// landing between the two would write the previous profile back.
	a.applyMu.Lock()
	cur := a.curSetting()
	if cur == nil {
		a.applyMu.Unlock()
		return fmt.Errorf("settings not loaded")
	}
	next, err := cur.WithActiveRouting(name)
	if err == nil {
		err = a.applySettingLocked(next)
	}
	a.applyMu.Unlock()
	if err != nil {
		return err
	}
	// Announced the same way the tray's cycling announces it (see nextRouting): the
	// window's routing list and the status bar's outbound-mode button both read this
	// event, and a profile switched from the settings dialog has to reach the same
	// listeners as one switched from the hotkey.
	if a.curCtx() != nil {
		runtime.EventsEmit(a.curCtx(), "routing:changed", name)
	}
	return nil
}

// GetAutostart reports whether run-at-login is enabled.
func (a *App) GetAutostart() bool {
	migrateAutostart()
	on, _ := autostart.Enabled(autostartName)
	return on
}

// SetAutostart enables or disables run-at-login.
func (a *App) SetAutostart(on bool) error {
	if on {
		return autostart.Enable(autostartName, autostart.ExePath())
	}
	return autostart.Disable(autostartName)
}

// InboundPortStatus says whether the port the core listens on can be taken.
//
// It is asked from the settings page because the failure it prevents is a confusing one:
// a port held by another program shows up as "the node will not start", with nothing in
// the message pointing at the number that is actually the problem.
type InboundPortStatus struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	// Free is whether a listener could be opened on it just now.
	Free bool `json:"free"`
	// Ours is whether what holds it is this app's own core — the ordinary case while a
	// node is running, and a state rather than a fault.
	Ours  bool   `json:"ours"`
	Error string `json:"error"`
}

// CheckInboundPort probes the inbound pair the reader is looking at. It opens and
// immediately closes a listener, so the probe leaves nothing behind.
//
// The pair is handed in rather than read from the settings file, because the file is
// not what the panel is showing: its fields are written 650ms after the last
// keystroke, so a check pressed just after typing a number used to answer about the
// number that was there before — "available" about a port nobody had asked about,
// while the field named another. The answer carries the pair it is about, and the
// panel prints it, so the two cannot be confused for each other.
func (a *App) CheckInboundPort(address string, port int) InboundPortStatus {
	st := InboundPortStatus{}
	if a.curSetting() == nil {
		st.Error = "backend not initialised"
		return st
	}
	host := strings.TrimSpace(address)
	if host == "" {
		host = "127.0.0.1"
	}
	st.Address, st.Port = host, port
	if st.Port <= 0 || st.Port > 65535 {
		st.Error = "端口号无效"
		return st
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(st.Port)))
	if err == nil {
		_ = l.Close()
		st.Free = true
		return st
	}
	// Held by something. With a node up it is almost certainly this app's own core, and
	// reporting that as "in use" would send the reader looking for a program that is
	// their own.
	if a.RunningProfile() != core.StoppedID {
		st.Ours = true
		return st
	}
	// The tun front-end's pid is not consulted here: it holds an adapter, not the
	// inbound port — the port belongs to the main core, which RunningProfile already
	// answered for.
	st.Error = err.Error()
	return st
}

// FreeInboundPort returns a port nothing is listening on, so that the reader can be handed
// a number that will work rather than a message about one that will not.
func (a *App) FreeInboundPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
