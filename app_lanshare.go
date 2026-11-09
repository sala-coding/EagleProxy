package main

import (
	"eagle/internal/core"
	"eagle/internal/util"
	"fmt"
	"net"
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
	a.applyMu.Unlock()
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
