package main

import (
	"time"

	"eagle/internal/core"
	"eagle/internal/dnsstub"
	"eagle/internal/tunserv"
)

// The machine's own resolvers, and who answers them.
//
// The split this app builds for DNS is worth nothing while the machine asks somebody else:
// Windows resolves through the resolvers its interfaces carry, asks every interface that has
// a way out, and takes the first answer that arrives. A router answering in five
// milliseconds beats a tunnel answering in a hundred every time — and the answer it wins
// with is a forged one for exactly the names this is all about (measured here:
// www.google.com answered with a Facebook address, which is what closed the browser's
// handshake mid-flight).
//
// So while a node is running, the interfaces are pointed at this app's forwarder, which
// hands the query to the core and lets the profile's own rules answer it — the same answer
// the tunnel's own resolver gives, now with nothing faster racing it. That is what makes
// 智能优选 and 智能切换 govern the machine's ordinary resolutions in BOTH modes: system
// proxy stops being the mode where "the browser resolves for itself".
//
// The write needs the token the resident server holds rather than this process, so the app
// asks (a state file) and the server applies (see internal/tunserv/sysdns_windows.go). What
// this file owns is the decision, and the decision is one line: a node is running, or it is
// not.

// systemDNSWanted answers whether the machine's names should be coming to this app's
// forwarder. A node has to be running — with no core there is nothing behind the listener,
// and a machine pointed at one would be waiting out a timeout per name — and the forwarder
// itself has to be up.
func (a *App) systemDNSWanted() bool {
	if !a.systemDNSEnabled() {
		return false
	}
	// The forwarder has to be answering on 53, not merely to exist: the machine is about
	// to be pointed at that port, and a state file that asks for it while the bind failed
	// (or is throttled) is every name waiting out a timeout before falling back. Asking
	// the server here means the retry is automatic — this decision is recomputed every
	// second, and the bind is retried every thirty.
	srv := dnsstub.Current()
	if srv == nil || !srv.SystemReady() {
		return false
	}
	// And the core has to be able to answer, because the system side's queries are
	// handed to it: the forwarder asks the core's own inbound, so that the core's rules
	// answer them. A takeover whose core is not up answers SERVFAIL for every name —
	// a machine with no names — and the case that bites is the ordinary one, a node
	// switch: the core is a second and a half from opening its inbound, and the
	// takeover was armed the moment the process existed.
	//
	// The core being *running* is not the same as the core answering, and this is the
	// second half of that: the process is there while it is still booting, and the
	// supervisor has not noticed the crash yet when the process is already gone.
	ctrl := a.curController()
	if ctrl == nil || !ctrl.InboundReady() {
		return false
	}
	return true
}

// systemDNSEnabled answers the other half: whether the forwarder should be holding port 53
// at all, before anything has been pointed at it.
//
// Split from systemDNSWanted because the two cannot be one decision without a deadlock:
// the port is bound by Configure, Configure is driven by this answer, and the takeover
// needs the port bound before it is true. Asking "is it serving" to decide "should it
// serve" answered no for ever.
func (a *App) systemDNSEnabled() bool {
	if a == nil || a.curSetting() == nil {
		return false
	}
	if a.RunningProfile() == core.StoppedID {
		return false
	}
	return dnsstub.Current() != nil
}

// nodeServing answers whether a node is up and able to answer.
//
// It is the fact the adapter's own resolver depends on: the address it names is answered
// by the main core's hijack-dns, so with no core behind it every query the machine sends it
// waits out a timeout. A node that is starting does not count — the core answers a
// second and a half after the process exists — and a node that is stopping does not count
// either, even before the process is gone.
func (a *App) nodeServing() bool {
	ctrl := a.curController()
	if ctrl == nil {
		return false
	}
	return ctrl.RunningID() != core.StoppedID && ctrl.InboundReady()
}

// reconcileSystemDNS brings both halves in line with that decision, and is safe to call
// whenever anything about it may have changed: it is idempotent, and everything it does is
// read from live state rather than remembered.
//
//   - the forwarder holds port 53 exactly while the takeover is on (see dnsstub.setSystem);
//   - the resident server is asked to point the interfaces here, or to put them back;
//   - and it is asked to exist at all, because in system-proxy mode this may be the first
//     time anything needs the elevated side — running the installed task costs no prompt
//     (see ensureTunServer).
func (a *App) reconcileSystemDNS(force bool) {
	if a == nil || a.curDataDir() == "" {
		return
	}
	st := tunserv.State{
		Adapter:   a.curSetting() != nil && a.curSetting().VPNMode(),
		Resolvers: a.systemDNSWanted(),
		Node:      a.nodeServing(),
	}
	// Written only when it says something new. This runs on a one-second tick, and a file
	// rewritten every second is a file whose writes the server would sit and read for
	// nothing — the reconcile on the other side exists to detect a change, not to be fed
	// the same bytes.
	a.sysDNSMu.Lock()
	unchanged := a.sysDNSStateSet && a.sysDNSState == st
	if unchanged {
		// The file can move without this process. The resident server clears it when it
		// reads our heartbeat as stopped — a crash, a long freeze — and an in-memory "we
		// already wrote this" would then keep the takeover off for the rest of the
		// session with nothing in the log. Compared against the disk, the answer is about
		// the file rather than about memory.
		unchanged = tunserv.ReadState(a.curDataDir()) == st
	}
	first := !a.sysDNSStateSet
	a.sysDNSState, a.sysDNSStateSet = st, true
	a.sysDNSMu.Unlock()
	if !unchanged {
		if err := tunserv.WriteState(a.curDataDir(), st); err != nil {
			a.logf("[dns] 无法记录常驻服务状态: " + err.Error())
			return
		}
	}
	// The forwarder's half of the same decision, BEFORE the early return below
	// rather than after it: the return is the "nothing is up" case, and that is
	// exactly when Configure(System: false) has to reach dnsstub — it is the
	// only call that closes port 53 (setSystem). Node stopped under the system
	// proxy, st is all-false, the return fired here, and this call never ran:
	// the forwarder held 53 for the rest of the session while the interfaces
	// kept their own resolvers behind it — the reader's local resolver could
	// never bind the port again, against the very contract this side keeps
	// (dnsstub.go: "only while asked for"). Both halves stay in one call, as
	// the note on the old placement below asked for; only the position moved.
	a.syncDNSStub()
	if !st.Adapter && !st.Resolvers {
		return
	}
	// One /Run of the installed task, and it needs the throttle below rather than the tick:
	// with the task missing, ensureTunServer's own road to a working server is creating it
	// — which costs a UAC prompt — and asking that every second is a dialog the reader has
	// to keep dismissing. Thirty seconds is a retry a reader would recognise as one, and
	// the tick that follows an answer keeps the state honest either way.
	a.sysDNSMu.Lock()
	due := first || force || time.Since(a.sysDNSAsked) > 30*time.Second
	if due {
		a.sysDNSAsked = time.Now()
	}
	a.sysDNSMu.Unlock()
	if due {
		// No prompt while the task is installed, which is the ordinary case; and without
		// this, system-proxy mode would have no elevated side at all and the takeover would
		// be a state file nobody reads.
		a.ensureTunServer()
	}
}

// releaseSystemDNS tells the elevated side to put the machine's resolvers back, for the exit
// path. The server does the same on its own way out (see RunServer), so this is the
// courteous half: whichever of the two notices first, the reader's machine ends up as it
// was found.
func (a *App) releaseSystemDNS() {
	if a == nil || a.curDataDir() == "" {
		return
	}
	if err := tunserv.WriteState(a.curDataDir(), tunserv.State{}); err != nil {
		a.logf("[dns] 退出时无法记录常驻服务状态: " + err.Error())
	}
}

// appHeartbeatPeriod is how often this process tells the resident server it is alive. Two
// seconds, the period the server uses in the other direction (see tunserv.ServerAlive):
// the two ends of one handshake, read from opposite sides.
const appHeartbeatPeriod = 2 * time.Second

// appHeartbeatLoop keeps the stamp the server reads to tell "the app is between beats"
// from "the app is gone".
//
// Why the file and not a message: a crash skips every defer this process owns — the exit
// path that would have written the empty state never runs — and the state file on disk
// still says the machine's resolvers are taken over. The server outlives this process, so
// it is the only party left that can act, and the age of this stamp is the whole of what
// it needs to know (see server_windows.go's sysTicks branch, which restores the machine on
// a stale one).
func (a *App) appHeartbeatLoop() {
	for {
		if a.curDataDir() != "" {
			if err := tunserv.WriteAppHeartbeat(a.curDataDir()); err != nil {
				a.logf("[dns] 无法记录程序存活心跳: " + err.Error())
			}
		}
		if a.quitting.Load() {
			if a.curDataDir() != "" {
				tunserv.ClearAppHeartbeat(a.curDataDir())
			}
			return
		}
		time.Sleep(appHeartbeatPeriod)
	}
}
