//go:build windows

package tunfast

import (
	"errors"
	"fmt"

	"eagle/internal/tunfast/internal/wintun"
)

// Prepare makes sure the adapter device exists, and does nothing when it
// already does. It is the "have the environment ready" half of a fast TUN
// switch: creating a wintun adapter is a PnP device creation — seconds, and the
// one part of a start-up that no amount of API-level work makes cheaper. Doing
// it ahead of time, in a process that is already running and already elevated,
// is what takes those seconds off the reader's path.
//
// No session is started and no handle is kept. Wintun's session is exclusive —
// one process at a time — so a prepared adapter is deliberately left unowned;
// what survives is the PnP device, which outlives every handle to it (the same
// fact that leaves an adapter behind when a core is killed). The core the app
// starts later opens that device instead of creating one, which is the fast
// path: CreateAdapter answers ERROR_OBJECT_ALREADY_EXISTS and OpenAdapter picks
// the adapter up untouched.
//
// Addresses are not configured here either. That is the core's business, it
// does it in microseconds, and writing them twice is how two owners of one
// interface start disagreeing about it.
//
// The name decides the adapter's identity: the GUID is derived from it, so a
// prepared device is the very one a later core looks for, and a device left over
// from an earlier session is opened rather than re-created. Needs the
// administrator token, like every other adapter call.
func Prepare(name string) error {
	if name == "" {
		return errors.New("tunfast: adapter name is required")
	}
	adapter, err := wintun.CreateAdapter(name, tunnelType, generateGUIDByDeviceName(name))
	if err != nil {
		if !isAdapterAlreadyExists(err) {
			return fmt.Errorf("tunfast: prepare adapter %q: %w", name, err)
		}
		// Already there. Opening it is what tells "the device exists" apart from
		// "the device is there but unusable" — the broken leftover state that
		// makes every later start fail — so it is opened and closed again rather
		// than assumed. An adapter that answers this is one a core can start on.
		adapter, err = wintun.OpenAdapter(name)
		if err != nil {
			return fmt.Errorf("tunfast: prepare adapter %q: exists but cannot be opened: %w", name, err)
		}
	}
	return adapter.Close()
}
