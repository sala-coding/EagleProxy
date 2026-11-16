package main

import (
	"eagle/internal/core"
	"eagle/internal/util"
	"eagle/internal/vpn"
	"fmt"
	"net"
	"os/exec"
	"time"
)

// LANShare reports whether the mixed inbound is open to the LAN, as the titlebar's
// capsule reads it.
func (a *App) LANShare() bool {
	s := a.curSetting()
	return s != nil && s.LANShare
}

// LANState is the answer a press gets: the flag as the machine now has it, the
// endpoint a phone dials, and what a probe of that endpoint found. The words the
// reader sees are built from these in the window (see lanNotice in App.vue), so
// this side answers in facts rather than in one language's sentences — the same
// reason the errors below are keys (lan.err.*) rather than prose.
type LANState struct {
	On        bool   `json:"on"`
	Running   bool   `json:"running"`
	Endpoint  string `json:"endpoint"`
	Listening bool   `json:"listening"`
	Auth      bool   `json:"auth"`
}

// SetLANShare toggles LAN sharing: with it on, the mixed inbound listens on every
// interface (0.0.0.0) instead of the loopback only, so a phone on the same Wi-Fi can
// be pointed at this machine's LAN address and the inbound port.
//
// The flag lives on the setting and the listen address is read by the config builder
// at build time, so a change reaches the port only through a rebuilt config: the
// running core is restarted when there is one. Order matters — the flag is written
// first, so the rebuilt core comes back listening the way the switch now says, and a
// failed restart leaves the flag on with the error to answer for it (the next start
// picks it up either way).
//
// Taken in the same order the mode switches keep (startStopMu outermost, applyMu
// inside — see SetVPNMode): the restart walks the same stop+start path a mode switch
// does, and both guards are what keep a settings save or a hotkey from landing
// between the flag's write and the restart.
func (a *App) SetLANShare(on bool) (*LANState, error) {
	a.startStopMu.Lock()
	defer a.startStopMu.Unlock()
	a.applyMu.Lock()
	cur := a.curSetting()
	if cur == nil {
		a.applyMu.Unlock()
		return nil, fmt.Errorf("settings not loaded")
	}
	if cur.LANShare == on {
		a.applyMu.Unlock()
		st := a.lanState()
		return &st, nil
	}
	// The switch IS a listener on the inbound port. With no usable port the
	// builder writes no inbound at all, so the flag alone would claim a phone
	// could dial in while nothing was ever opened — refused rather than stored.
	if on && !util.IsValidPort(cur.InboundSocksPort) {
		a.applyMu.Unlock()
		return nil, fmt.Errorf("lan.err.noPort")
	}
	next := cur.Clone()
	next.LANShare = on
	if err := a.applySettingLocked(next); err != nil {
		a.applyMu.Unlock()
		return nil, err
	}
	port := next.InboundSocksPort
	a.applyMu.Unlock()
	// The port is only half of "reachable from the LAN": Windows Firewall decides
	// the other half, and on a network it calls Public — which is what a home
	// Wi-Fi is by default — an unsolicited inbound packet is dropped unless a rule
	// names it. Nothing in this app used to add one, so the switch reported a
	// listener on every interface while every phone on the network timed out. The
	// rule is named by port, not by the core's path: the path changes with every
	// update, and a rule pointing at yesterday's binary would fail silently
	// exactly the way this did. Its own goroutine because adding one needs the
	// administrator token, and a prompt inside the locks above would hold both
	// mode switches until it is answered.
	if on {
		go a.ensureLANFirewallRule(port)
	} else {
		// 关闭时把这条放行撤掉：留着它等于把一个本程序已不再监听的端口长期对局域网敞开，
		// 日后该端口被别的程序占用时，外部可以直接连进来而不再经过防火墙的确认。
		// 端口改过的话，旧端口那条也一并撤（规则按端口命名，见 lanFirewallRuleName）。
		ports := []int{port}
		if cur.InboundSocksPort != port {
			ports = append(ports, cur.InboundSocksPort)
		}
		for _, p := range ports {
			go a.removeLANFirewallRule(p)
		}
	}
	// No core up: the flag is the whole of the work — the next start builds its
	// config from it. The state says the node is not running, and the window says
	// the switch takes effect when it is.
	id := a.RunningProfile()
	if id == core.StoppedID {
		st := a.lanState()
		return &st, nil
	}
	// The locked forms, not RestartProfile: this seat already holds startStopMu,
	// and the wrapper locks the same non-reentrant mutex — the call that deadlocked
	// the binding, leaving the window's switch busy for the rest of the session.
	// The stop+start here is what RestartProfile itself does under the lock.
	if err := a.stopForSwitchLocked(); err != nil {
		return nil, fmt.Errorf("lan.err.stop: %s", err)
	}
	if err := a.startProfileLocked(id); err != nil {
		return nil, fmt.Errorf("lan.err.start: %s", err)
	}
	st := a.lanState()
	// One ASCII line for the file: the notices themselves are the window's, and
	// this is the record a bug report can be read against.
	a.logf(fmt.Sprintf("[app] lan_share=%v endpoint=%q listening=%v", st.On, st.Endpoint, st.Listening))
	return &st, nil
}

// lanFirewallRuleName is the rule this app owns for a shared port. One name per
// port, so two switches on two ports do not overwrite each other's rule.
func lanFirewallRuleName(port int) string { return fmt.Sprintf("Eagle LAN share %d", port) }

// ensureLANFirewallRule allows the shared inbound port in Windows Firewall,
// asking for the administrator token only when no such rule is there yet.
// Declining leaves the switch on and the machine as it was — the log says what
// is missing, which is more than the silent timeout this replaces.
func (a *App) ensureLANFirewallRule(port int) {
	if port <= 0 {
		return
	}
	name := lanFirewallRuleName(port)
	if lanRuleExists(name) {
		return
	}
	// Chained in one elevated call: delete-then-add keeps a re-enable from
	// stacking duplicate rules, and `>nul 2>&1` keeps a missing-rule delete from
	// failing the add that follows it.
	script := fmt.Sprintf(`netsh advfirewall firewall delete rule name="%s" >nul 2>&1 & netsh advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d profile=any`,
		name, name, port)
	if code, err := vpn.RunElevatedWait("cmd.exe", []string{"/c", script}, ""); err != nil || code != 0 {
		a.logf(fmt.Sprintf("[app] 局域网共享端口 %d 未放行，防火墙可能拦截局域网访问", port))
		return
	}
	a.logf(fmt.Sprintf("[app] 局域网共享已放行端口 %d", port))
}

// lanRuleExists asks the firewall whether the rule is already registered. The
// query needs no elevation — only the write does — so a switch pressed a second
// time does not prompt again.
func lanRuleExists(name string) bool {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+name)
	util.HideConsole(cmd)
	return cmd.Run() == nil
}

// removeLANFirewallRule takes the allow-rule back when sharing goes off. The next
// enable adds it again, which costs one more prompt — the trade a standing inbound
// allow for a port this app no longer serves is not worth making silently.
func (a *App) removeLANFirewallRule(port int) {
	if port <= 0 {
		return
	}
	name := lanFirewallRuleName(port)
	if !lanRuleExists(name) {
		return
	}
	script := fmt.Sprintf(`netsh advfirewall firewall delete rule name="%s"`, name)
	if code, err := vpn.RunElevatedWait("cmd.exe", []string{"/c", script}, ""); err != nil || code != 0 {
		a.logf(fmt.Sprintf("[app] 局域网共享端口 %d 的防火墙放行未能撤除，可手动删除规则「%s」", port, name))
		return
	}
	a.logf(fmt.Sprintf("[app] 局域网共享已撤除端口 %d 的防火墙放行", port))
}

// lanState reads the switch back: whether sharing is on, whether a core is up,
// whether the inbound has an account, and — when the first two hold — the endpoint
// a phone dials, plus whether that endpoint answered.
func (a *App) lanState() LANState {
	st := LANState{On: a.LANShare(), Running: a.RunningProfile() != core.StoppedID}
	if s := a.curSetting(); s != nil {
		st.Auth = s.InboundAuth != nil && s.InboundAuth.NeedAuth()
	}
	if st.On && st.Running {
		st.Endpoint = a.LANEndpoint()
		st.Listening = st.Endpoint != "" && lanListening(st.Endpoint)
	}
	return st
}

// lanListening dials the shared inbound on this machine's own LAN address. A
// connection made from here is the one check available: a firewall that drops
// outside packets still lets this one through, so a pass means the listener is
// up, not that the LAN can reach it.
//
// Retried, because a start that has answered is not a socket that is bound yet:
// the core has read its config and answered the API, and the listener may be a
// few hundred milliseconds behind that. One attempt here would report a working
// switch as a blocked port.
func lanListening(ep string) bool {
	for i := 0; i < 3; i++ {
		if conn, err := net.DialTimeout("tcp", ep, 500*time.Millisecond); err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
