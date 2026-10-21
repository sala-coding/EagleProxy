package core

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"eagle/internal/bean"
	"eagle/internal/db"
	"eagle/internal/setting"
)

// coreExePath returns the bundled core, or skips the test when it is absent.
func coreExePath(t *testing.T) string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	p := filepath.Join(root, "sing-box", "sing-box.exe")
	if runtime.GOOS != "windows" {
		p = filepath.Join(root, "sing-box", "sing-box")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no sing-box binary at %s", p)
	}
	return p
}

// freePort asks the OS for a port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startRealCore starts the bundled core on a profile of its own, and hands back
// the controller and the settings it runs with.
func startRealCore(t *testing.T) (*Controller, *setting.Setting, int) {
	t.Helper()
	dir := t.TempDir()
	mgr := db.NewManager(dir)
	if err := mgr.Load(); err != nil {
		t.Fatalf("manager load: %v", err)
	}
	g := &db.Group{ID: -1, FrontProxyID: -1, Name: "g"}
	if err := mgr.AddGroup(g); err != nil {
		t.Fatalf("add group: %v", err)
	}
	b := bean.New()
	b.Set("name", "n")
	b.Set("addr", "example.com")
	b.Set("port", 443)
	b.Set("id", "11111111-1111-1111-1111-111111111111")
	b.Set("aid", 0)
	b.Set("sec", "auto")
	b.Set("stream", map[string]interface{}{"net": "tcp", "sec": "tls", "sni": "example.com"})
	ent := &db.ProxyEntity{Type: "vmess", ID: -1, GID: g.ID, Bean: b}
	if err := mgr.AddProfile(ent, g.ID); err != nil {
		t.Fatalf("add profile: %v", err)
	}

	s := setting.Load(dir)
	s.InboundSocksPort = freePort(t)
	s.CoreBoxClashAPI = freePort(t)

	c := New(mgr, s, dir)
	c.SetCorePath(coreExePath(t))
	if err := c.Start(ent.ID); err != nil {
		t.Fatalf("Controller.Start: %v", err)
	}
	return c, s, ent.ID
}

// TestReviewInboundReadyFollowsTheCore pins the answer to "can this core take
// a query": true once the core's own inbound answers, false again once the core
// is gone.
//
// A running process is not the same thing, and the difference is a second and a
// half on every node switch: sing-box reads its config, opens its inbounds and
// only then prints "started". Everything the app arms on the strength of a
// start — the machine's resolvers, the TUN capture — goes through that inbound,
// and armed a second early they answered nothing until the core was really up:
// every name on the machine failed with SERVFAIL, and the traffic the capture
// had taken was refused.
func TestReviewInboundReadyFollowsTheCore(t *testing.T) {
	c, s, _ := startRealCore(t)
	defer func() { _ = c.Stop() }()

	deadline := time.Now().Add(20 * time.Second)
	for !c.InboundReady() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !c.InboundReady() {
		t.Fatalf("the core's inbound never answered on 127.0.0.1:%d", s.InboundSocksPort)
	}

	// It really is the core answering, not something left over.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(s.InboundSocksPort), 2*time.Second)
	if err != nil {
		t.Fatalf("the inbound does not accept connections: %v", err)
	}
	_ = conn.Close()

	if err := c.Stop(); err != nil {
		t.Fatalf("Controller.Stop: %v", err)
	}
	if c.InboundReady() {
		t.Errorf("the readiness outlived the core: the app would keep pointing the machine at a " +
			"core that is gone, and every name on it would fail")
	}
}

// TestReviewARequestedStopDoesNotReadAsACrash covers the line the supervisor
// writes when the core is reaped.
//
// Windows reports a killed process as an error exit, so a stop that the reader
// asked for was written as "core exited: exit status 1" — which reads like a
// crash of a core that was working a moment ago, right next to the "stopping
// core" line that says the opposite.
func TestReviewARequestedStopDoesNotReadAsACrash(t *testing.T) {
	c, _, _ := startRealCore(t)

	deadline := time.Now().Add(20 * time.Second)
	for !c.InboundReady() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if err := c.Stop(); err != nil {
		t.Fatalf("Controller.Stop: %v", err)
	}

	// The supervisor writes its line when the process is reaped, which is a
	// moment after the kill.
	deadline = time.Now().Add(5 * time.Second)
	var logs []string
	for time.Now().Before(deadline) {
		logs = c.Logs()
		if containsLine(logs, "core stopped") || containsLine(logs, "core exited") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, l := range logs {
		if strings.Contains(l, "core exited: exit status 1") {
			t.Errorf("a requested stop was written as a crash: %q", l)
		}
	}
	if !containsLine(logs, "core stopped") {
		t.Errorf("the requested stop left no line of its own; the log says:\n%s", strings.Join(logs, "\n"))
	}
}

// containsLine reports whether any of the lines holds the wanted text.
func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// TestReviewStopTreatsAnAlreadyExitedCoreAsStopped pins down what a stop means
// when the core has already left.
//
// On Windows, killing a process that has ended — but has not yet been reaped — 
// fails. That kill error used to be returned straight away, and the wait below
// it was skipped, so the caller read a stopped core as a failed stop: the
// system proxy was left pointing at a port nothing was listening on, and the
// machine had no internet until the app was restarted.
//
// The process here is a stub that exits at once, and the supervisor that reaps
// it runs a moment later — the same window the bug lived in.
func TestReviewStopTreatsAnAlreadyExitedCoreAsStopped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the stub core is a Windows command")
	}
	dir := t.TempDir()
	c := New(nil, setting.Load(dir), dir)

	cmd := exec.Command("cmd", "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the stub core: %v", err)
	}
	_ = cmd.Wait() // the process is gone; the supervisor has not run yet

	c.mu.Lock()
	c.cmd = cmd
	c.runningID = 7
	c.hotNodes = map[int]string{7: "proxy-7"}
	c.mu.Unlock()

	// The supervisor, a moment later: exactly what wait() does when it reaps
	// the process.
	reaped := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(reaped)
		time.Sleep(150 * time.Millisecond)
		c.wait(cmd, 7, exited)
	}()

	if err := c.Stop(); err != nil {
		t.Errorf("Stop reported a failure for a core that had already exited: %v "+
			"(the kill error was returned straight away, the wait below it was skipped, and the "+
			"system proxy was left pointing at a port nothing was listening on)", err)
	}
	<-reaped

	// The config went with the process: a stale table let a hot switch name a
	// node of a config that is no longer running — and the switch is a PUT to
	// the clash API port, which another process may well have been holding.
	c.mu.Lock()
	hot := len(c.hotNodes)
	c.mu.Unlock()
	if hot != 0 {
		t.Errorf("the hot-switch table still holds %d entr(ies) after the core left", hot)
	}
}
