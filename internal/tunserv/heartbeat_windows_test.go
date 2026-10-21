//go:build windows

package tunserv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The stamp names the process that wrote it. That is what the server leans on to tell a
// sleeping machine from a dead app: the clock says "long silence" in both cases, and only
// the process itself can say which one it is.
func TestAppHeartbeatCarriesPid(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAppHeartbeat(dir); err != nil {
		t.Fatalf("WriteAppHeartbeat: %v", err)
	}
	data, err := os.ReadFile(appHeartPath(dir))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	first := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if first != strconv.Itoa(os.Getpid()) {
		t.Errorf("heartbeat pid = %q, want this process %d", first, os.Getpid())
	}
}

// The regression this exists for: a laptop that slept stops the beats for far longer than
// the budget while the app is alive. Read by the clock alone, the server would restore the
// resolvers and kill the front-end on every wake.
func TestAppHeartStaleLeansOnThePidNotTheClock(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAppHeartbeat(dir); err != nil {
		t.Fatalf("WriteAppHeartbeat: %v", err)
	}
	old := time.Now().Add(-10 * appHeartGoneAfter)
	if err := os.Chtimes(appHeartPath(dir), old, old); err != nil {
		t.Fatalf("age the stamp: %v", err)
	}
	if AppHeartStale(dir) {
		t.Fatal("a stale stamp with a live pid was read as a crash")
	}
}

// A pid that names nothing is the answer the clock could not give, without waiting for
// another budget to pass.
func TestAppHeartStaleForAPidThatIsGone(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "temp"), 0o755); err != nil {
		t.Fatalf("temp: %v", err)
	}
	// Large enough to be no process on any Windows build, and outside the range a small
	// machine could have about now.
	if err := os.WriteFile(appHeartPath(dir), []byte("2147483632\n2026-01-01 00:00:00.000"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !AppHeartStale(dir) {
		t.Fatal("a stamp naming no process answered alive")
	}
}

// No file is no evidence, and the safe reading of no evidence is to leave the machine alone.
func TestAppHeartStaleWithoutAStamp(t *testing.T) {
	if AppHeartStale(t.TempDir()) {
		t.Fatal("an absent heartbeat answered stale")
	}
}

// The server's log is the first sign a cold start produces — before its first beat — and
// the app reads its stamp to tell "still starting" from "started nothing".
func TestServerLogStampTracksTheLog(t *testing.T) {
	dir := t.TempDir()
	if stamp := ServerLogStamp(dir); !stamp.IsZero() {
		t.Errorf("stamp = %v with no log, want the zero time", stamp)
	}
	path := filepath.Join(dir, "tunserve.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("temp: %v", err)
	}
	if err := os.WriteFile(path, []byte("server starting\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if stamp := ServerLogStamp(dir); stamp.IsZero() {
		t.Error("stamp is zero after the server wrote a line")
	}
}
