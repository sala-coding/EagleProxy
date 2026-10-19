//go:build windows

package tunserv

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/tunfast"
)

// State is what the app asks the elevated side to be doing, read from a file on every tick.
//
// A file rather than a command in the slot, because this is a state and not an event: the
// server may start long after the app decided, the app may be restarted while the state
// stands, and a machine left with its resolvers pointed at a socket nobody owns has to be
// put back by whoever notices — which is this process, on its next tick or on its way out.
type State struct {
	// Adapter asks for the TUN adapter to be kept ready. Set while TUN mode is on: a server
	// that prepared an adapter for a mode the reader is not in would be a network device
	// appearing and disappearing on their list for nothing, and (with the app's own idle
	// reclaim taking it apart again) doing that every ten seconds.
	Adapter bool `json:"adapter"`
	// Resolvers asks for the machine's own resolvers to be pointed at the app's forwarder.
	// Set while a node is running, in either mode, and cleared when it stops.
	Resolvers bool `json:"resolvers"`
}

const (
	stateName = "tunserv-state.json"
	undoName  = "tunserv-resolvers-undo.json"
)

func statePath(dataDir string) string { return filepath.Join(dataDir, "temp", stateName) }
func undoPath(dataDir string) string  { return filepath.Join(dataDir, "temp", undoName) }

// WriteState records what the app wants. Written atomically — to a neighbour, then renamed —
// because the server reads it on a 250 ms tick and a reader that caught half a file would
// act on a guess about whether to change the machine's resolvers.
func WriteState(dataDir string, st State) error {
	if dataDir == "" {
		return os.ErrInvalid
	}
	dir := filepath.Join(dataDir, "temp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := statePath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, statePath(dataDir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadState answers what the app asked for, and "nothing" when there is no file or it will
// not parse. Nothing is also the safe answer: it leaves the machine's resolvers alone.
func ReadState(dataDir string) State {
	var st State
	data, err := os.ReadFile(statePath(dataDir))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

// sysMu serialises the two places that touch the machine's resolvers — the loop's tick and
// the exit path — so that a restore is never cut in half by a reconcile.
var sysMu sync.Mutex

// lastApplyTry paces the re-apply in ReconcileResolvers, and applyIncomplete remembers
// that there is anything to re-apply at all. Guarded by sysMu, like the machine's
// resolvers they stand for.
var (
	lastApplyTry    time.Time
	applyIncomplete bool
)

// ReconcileResolvers brings the machine's own resolvers in line with what the app asked for,
// and does nothing when they already are.
//
// The undo record is written before anything is changed, never after: a crash between the
// two has to leave a way back, and the way back is a file rather than a memory — this
// process may be killed and started again by a scheduled task at any moment.
func ReconcileResolvers(dataDir string, want bool) {
	sysMu.Lock()
	defer sysMu.Unlock()
	taken := undoExists(dataDir)
	switch {
	case want && taken:
		// Half-applied is a state the two conditions above cannot see: the snapshot was
		// written, one interface refused its write (a re-enumerating adapter, a Wi-Fi
		// switch), and every later pass then did nothing at all — that interface stayed
		// on the ISP resolver, still being asked first, for the rest of the session.
		// Re-applying is the same list written again, so the retry is idempotent; it is
		// paced because this function runs on the server's one-second tick, and skipped
		// entirely while the last apply was clean.
		if !applyIncomplete || time.Since(lastApplyTry) < 30*time.Second {
			return
		}
		lastApplyTry = time.Now()
		snapshot, err := readUndo(dataDir)
		if err != nil {
			log("resolvers: cannot read the record to re-apply: %v", err)
			return
		}
		if err := tunfast.PrependResolvers(snapshot, netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")); err != nil {
			log("resolvers: re-apply: %v", err)
			return
		}
		applyIncomplete = false
		log("resolvers: re-applied to %d interface(s)", len(snapshot))
	case want && !taken:
		snapshot, err := tunfast.SystemResolvers(configbuilder.TunAdapterName())
		if err != nil {
			log("resolvers: nothing to point (leaving them alone): %v", err)
			return
		}
		if err := writeUndo(dataDir, snapshot); err != nil {
			// Without the way back, the change does not happen at all. A leak is a smaller
			// thing than a machine whose resolver list nobody can restore.
			log("resolvers: cannot record what to restore, leaving them alone: %v", err)
			return
		}
		if err := tunfast.PrependResolvers(snapshot, netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")); err != nil {
			// At least one interface kept the resolver it had. The record stays (it is
			// the way back for the ones that took the write), and the retry above is
			// what finishes the rest.
			applyIncomplete = true
			log("resolvers: apply: %v", err)
			return
		}
		applyIncomplete = false
		lastApplyTry = time.Now()
		log("resolvers: taken over (%d interface(s); the app's forwarder answers on 53)", len(snapshot))
	case !want && taken:
		restoreLocked(dataDir, "the node stopped")
		// Whether it succeeded or not is restoreLocked's to report; what is certain is
		// that nothing is waiting to be re-applied while the takeover is off.
		applyIncomplete = false
	}
}

// OriginalResolvers answers the machine's own resolvers as they were before the takeover:
// the addresses the interfaces carried in the undo record. The app's forwarder uses them as
// a bootstrap for upstreams written as host names (see dnsstub.Wires.Bootstrap) — a DoH
// resolver named by host name cannot be dialled while the system resolver points at the
// forwarder itself, because resolving that name would come back as a query to forward.
//
// Empty when nothing was taken over, which is exactly when the system resolver is still the
// right thing to ask.
func OriginalResolvers(dataDir string) []string {
	snapshot, err := readUndo(dataDir)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(snapshot))
	for _, e := range snapshot {
		for _, list := range [][]string{e.V4, e.V6} {
			for _, s := range list {
				addr, perr := netip.ParseAddr(s)
				if perr != nil || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
					// Loopback is the takeover or another local resolver; a link-local
					// address needs a scope this record does not carry. Neither is a
					// bootstrap this process can ask without going in a circle.
					continue
				}
				key := addr.String()
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out
}

// RestoreResolvers puts the machine's resolvers back, for the exit path. Safe to call when
// nothing was taken over — that is the ordinary case at the end of a session in system-proxy
// mode.
func RestoreResolvers(dataDir string) {
	sysMu.Lock()
	defer sysMu.Unlock()
	if undoExists(dataDir) {
		restoreLocked(dataDir, "the server is stopping")
	}
}

func restoreLocked(dataDir, why string) {
	snapshot, err := readUndo(dataDir)
	if err != nil {
		log("resolvers: cannot read what to restore (%s): %v", why, err)
		return
	}
	if err := tunfast.RestoreResolvers(snapshot); err != nil {
		// The file stays: the next process to notice will try again, and deleting it now
		// would turn a failed restore into a forgotten one.
		log("resolvers: restore failed (%s): %v", why, err)
		return
	}
	_ = os.Remove(undoPath(dataDir))
	log("resolvers: restored (%s; %d interface(s))", why, len(snapshot))
}

func undoExists(dataDir string) bool {
	_, err := os.Stat(undoPath(dataDir))
	return err == nil
}

func writeUndo(dataDir string, snapshot []tunfast.AdapterResolvers) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	dir := filepath.Join(dataDir, "temp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := undoPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, undoPath(dataDir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readUndo(dataDir string) ([]tunfast.AdapterResolvers, error) {
	data, err := os.ReadFile(undoPath(dataDir))
	if err != nil {
		return nil, err
	}
	var snapshot []tunfast.AdapterResolvers
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}
