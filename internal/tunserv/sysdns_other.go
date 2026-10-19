//go:build !windows

package tunserv

import "errors"

// State is what the app asks the elevated side to be doing. Declared here so the app's own
// bookkeeping compiles everywhere it builds; nothing acts on it off Windows, where there is
// no elevated side to ask.
type State struct {
	Adapter   bool `json:"adapter"`
	Resolvers bool `json:"resolvers"`
}

// WriteState reports that there is nobody to record this for.
func WriteState(dataDir string, st State) error { return errors.New("tunserv: windows only") }

// ReadState answers "nothing wanted", which is what a machine with no elevated side has.
func ReadState(dataDir string) State { return State{} }

// ReconcileResolvers does nothing: there are no interface resolvers being taken over here.
func ReconcileResolvers(dataDir string, want bool) {}

// RestoreResolvers does nothing, for the same reason.
func RestoreResolvers(dataDir string) {}

// OriginalResolvers has no record to read off Windows: nothing was taken over, so there is
// no "before" for the forwarder to bootstrap from.
func OriginalResolvers(dataDir string) []string { return nil }
