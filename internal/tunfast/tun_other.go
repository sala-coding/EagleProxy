//go:build !windows

package tunfast

import (
	"errors"
	"fmt"
)

// ErrUnsupported reports the bring-up being attempted on a platform where
// wintun does not exist. Options and Interface above are shared so a call
// site compiles unchanged; the Windows side is the only implementation,
// because each system's bring-up lives in the code that is already
// platform-specific.
var ErrUnsupported = errors.New("tunfast: only implemented on windows")

// Open is the cross-platform entry point. On every system without wintun
// it reports ErrUnsupported rather than pretending to open something.
func Open(options Options) (Interface, error) {
	return nil, ErrUnsupported
}

// ErrNotFound is the same answer as on Windows: no adapter by that name.
var ErrNotFound = errors.New("adapter not found")

// Prepare reports ErrUnsupported: there is no adapter to prepare on a system
// without wintun, and pretending otherwise would be a lie a caller could act on.
func Prepare(name string) error { return ErrUnsupported }

// Link is the stand-in for the Windows type of the same name, so call sites in
// shared code compile. Nothing here can be taken or released, and saying so is
// the honest answer: routes are a Windows-only concern in this app.
type Link struct{}

// EgressIndex reports ErrUnsupported: there is no tunnel to step over here, and the
// caller's fallback (no binding) is the same answer a machine with one route gives.
func EgressIndex(tunnelName string) (uint32, error) {
	return 0, ErrUnsupported
}

// Find reports that there is no adapter to find, which on these systems is the
// truth rather than a platform gap.
func Find(name string) (*Link, error) {
	return nil, fmt.Errorf("tunfast: adapter %q: %w", name, ErrNotFound)
}

// Take reports ErrUnsupported.
func (l *Link) Take() error { return ErrUnsupported }

// Release reports ErrUnsupported.
func (l *Link) Release() error { return ErrUnsupported }
