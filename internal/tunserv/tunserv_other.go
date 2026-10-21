//go:build !windows

// Package tunserv is the resident TUN front-end owner. It is implemented on
// Windows only: the scheduled-task carrier, the highest-privilege run, and the
// adapter lifecycle the whole design turns on are all platform matters, and
// each other platform's bring-up lives in the code already specific to it.
// Call sites compile against the stub and branch on ServerAlive, which is
// always false here, so the server path is dead code rather than a second
// implementation to keep in step.
package tunserv

import (
	"fmt"
	"time"
)

// FrontPID mirrors the Windows record shape so shared call sites compile.
type FrontPID struct {
	PID    int
	Server bool
}

// ServerAlive is always false off Windows, which turns every call site into
// the legacy path it came from.
func ServerAlive(dataDir string) bool { return false }

// ServerRunsSameBuild is always false off Windows: there is no server here, so
// there is none from another build either.
func ServerRunsSameBuild(dataDir string) bool { return false }

// FrontPIDOf is nil off Windows: no pid record is written to compare.
func FrontPIDOf(dataDir string) *FrontPID { return nil }

// SendCommand fails rather than pretend a command went anywhere.
func SendCommand(dataDir string, cmd Command) error {
	return fmt.Errorf("tunserv: windows only")
}

// ReplaceCommand is the cancelling form on Windows; here it fails for the same
// reason SendCommand does, so the call site stays one shape to read.
func ReplaceCommand(dataDir string, cmd Command) error {
	return fmt.Errorf("tunserv: windows only")
}

// WriteAppHeartbeat has nobody to stamp for off Windows: there is no resident
// server reading it, so the call site stays one shape everywhere.
func WriteAppHeartbeat(dataDir string) error { return nil }

// ClearAppHeartbeat has nothing to remove here, for the same reason.
func ClearAppHeartbeat(dataDir string) {}

// AppHeartStale is always false off Windows: no server exists to act on it.
func AppHeartStale(dataDir string) bool { return false }

// ServerLogStamp has no log to stamp off Windows: there is no server writing one.
func ServerLogStamp(logDir string) time.Time { return time.Time{} }

// Command is the call-site shape; nothing consumes it off Windows.
type Command struct {
	Action string
	Config string
	Core   string
}

// Action names, mirroring the Windows constants.
const (
	ActionStart = "start"
	ActionStop  = "stop"
	ActionExit  = "exit"
)
