// Package speedtest measures latency / download speed of profiles.
//
// UrlTest and FullTest spin up a temporary sing-box instance with the profile's
// outbound and a local mixed inbound, then probe through it. TcpPing talks to
// the server directly and needs no core.
package speedtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"eagle/internal/configbuilder"
	"eagle/internal/core"
	"eagle/internal/db"
	"eagle/internal/setting"
	"eagle/internal/util"
)

// Test modes (kept stable for the frontend).
//
// What each mode produces: URLTest/FullTest measure through a temporary core,
// TCPPing only does a raw TCP handshake. URLTest and TCPPing yield a latency,
// FullTest yields latency + speed, SpeedTest yields a speed only — the frontend
// uses that to decide which column of a result to refresh.
const (
	ModeURLTest   = 0
	ModeTCPPing   = 1
	ModeFullTest  = 2
	ModeSpeedTest = 3
)

// proxyTag is the outbound tag used by the config builder for the main hop.
const proxyTag = "proxy"

// hasOutboundTag reports whether the config contains an outbound with the tag.
func hasOutboundTag(cfg map[string]interface{}, tag string) bool {
	list, ok := cfg["outbounds"].([]interface{})
	if !ok {
		return false
	}
	for _, o := range list {
		if m, ok := o.(map[string]interface{}); ok && m["tag"] == tag {
			return true
		}
	}
	return false
}

// Runner executes speed tests against profiles.
type Runner struct {
	mgr       *db.Manager
	s         *setting.Setting
	corePath  string
	configDir string
	logf      func(string)
	// ctx is the run's context, and cancelling it is what the stop button really
	// reaches. The probes below are the only long work in this package — a full
	// test is three latency samples, a download of up to tens of megabytes and two
	// address lookups — and without a context of its own the cancel could only mark
	// the run as stopped: every worker already in flight ran on to its own timeout
	// first (about three minutes for a full test), which is what the reader met as a
	// stop button that did nothing until much later.
	ctx context.Context
}

// New creates a Runner.
func New(mgr *db.Manager, s *setting.Setting, corePath, configDir string) *Runner {
	return &Runner{mgr: mgr, s: s, corePath: corePath, configDir: configDir}
}

// SetLogger forwards the temporary core log to the UI log panel.
func (r *Runner) SetLogger(f func(string)) { r.logf = f }

// SetContext hands the runner the run's context. A runner without one is not
// cancelled by anything, which is what the zero value means.
func (r *Runner) SetContext(ctx context.Context) { r.ctx = ctx }

// base is the context every probe runs under: a runner built without one still has
// to run.
func (r *Runner) base() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// reportCoreOutput pushes the temporary core output to the logger so the reason
// a test failed is visible in the UI instead of only in the toast.
func (r *Runner) reportCoreOutput(ent *db.ProxyEntity, output string) {
	if r.logf == nil {
		return
	}
	r.logf(fmt.Sprintf(">>> [test] %s %s", ent.DisplayType(), ent.DisplayName()))
	for _, line := range strings.Split(tail(output, 4000), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			r.logf("[test] " + line)
		}
	}
}

// RunTCPPing measures a direct TCP connection latency (no core required).
func (r *Runner) RunTCPPing(ent *db.ProxyEntity) (int, error) {
	addr := ent.ServerAddress()
	port := ent.ServerPort()
	if addr == "" || port == 0 {
		return 0, fmt.Errorf("invalid server address")
	}
	start := time.Now()
	// DialContext rather than DialTimeout: a cancelled run stops waiting on a server
	// that is not answering instead of holding the socket for its full five seconds.
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(r.base(), "tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	ms := int(time.Since(start).Milliseconds())
	if ms <= 0 {
		ms = 1
	}
	return ms, nil
}

// RunURLTest measures latency through a temporary instance of the profile.
func (r *Runner) RunURLTest(ent *db.ProxyEntity) (int, error) {
	var latency int
	// The inner error must be a separate variable. Assigning to the outer one from
	// inside the callback would create a different variable and withTestCore's own
	// result would be silently discarded, leaving latency at 0 with no error.
	_, err := r.withTestCore(ent, 12*time.Second, func(proxyPort int) error {
		ms, err := probeLatency(r.base(), proxyPort, r.s.TestLatencyURL, 10*time.Second)
		if err != nil {
			return err
		}
		latency = ms
		return nil
	})
	return latency, err
}

// latencySamples is how many requests one measurement averages over. Three is
// the smallest number that still rejects an outlier: with two a single slow
// response is a coin flip, and with three the median is always a real
// observation rather than an average of an unlucky pair.
const latencySamples = 3

// probeLatency takes several samples through the same proxy and reports the
// median. One request is dominated by noise — TCP slow start, a lost packet
// costing a retransmit, a momentary queue on the node — so consecutive samples
// can differ by hundreds of milliseconds and the column becomes unreadable. The
// median is used rather than the mean because it ignores the long tail, which is
// exactly the part that makes a healthy node look bad by accident.
func probeLatency(ctx context.Context, proxyPort int, testURL string, timeout time.Duration) (int, error) {
	samples := make([]int, 0, latencySamples)
	var lastErr error
	for i := 0; i < latencySamples; i++ {
		if err := ctx.Err(); err != nil {
			// The run was stopped: the samples already taken are not a measurement
			// of anything, so they are not reported as one.
			return 0, err
		}
		ms, err := urlTest(ctx, proxyPort, testURL, timeout)
		if err != nil {
			lastErr = err
			continue
		}
		samples = append(samples, ms)
	}
	if len(samples) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("no successful probe")
		}
		return 0, lastErr
	}
	sort.Ints(samples)
	return samples[len(samples)/2], nil
}

// RunFullTest measures latency, download speed and the outbound IP. The speed is
// -1 when the download step failed, so the list can show it as unavailable.
func (r *Runner) RunFullTest(ent *db.ProxyEntity) (int, float64, string, error) {
	var latency int
	speed := 0.0
	var report strings.Builder
	_, err := r.withTestCore(ent, 40*time.Second, func(proxyPort int) error {
		ctx := r.base()
		ms, err := probeLatency(ctx, proxyPort, r.s.TestLatencyURL, 10*time.Second)
		if err != nil {
			return err
		}
		latency = ms
		fmt.Fprintf(&report, "延迟: %d ms\n", ms)

		timeout := time.Duration(r.s.TestDownloadTimeout) * time.Second
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		if s, err := downloadSpeedThrough(ctx, proxyPort, r.s.TestDownloadURL, timeout); err == nil {
			speed = s
			fmt.Fprintf(&report, "下载: %.2f MB/s\n", s)
		} else {
			speed = -1
			fmt.Fprintf(&report, "下载: 失败 (%v)\n", err)
		}
		// The address lookups are the last two steps and they carry their own
		// timeouts; a run that has been cancelled skips them rather than spending
		// another eighteen seconds on a report nobody is going to read.
		if err := ctx.Err(); err != nil {
			return err
		}
		// The machine's own address first, then the node's: read in the order they
		// happen, and it is the pair that answers "did the traffic actually leave
		// through the node". Skipped in TUN mode, where every request is taken by the
		// core and "direct" would report the same address twice under two names.
		if !r.s.VPNMode() {
			if ip, err := inboundIP(8 * time.Second); err == nil {
				fmt.Fprintf(&report, "入口 IP: %s\n", ip)
			}
		}
		if ip, err := outboundIP(proxyPort, 10*time.Second); err == nil {
			fmt.Fprintf(&report, "出口 IP: %s\n", ip)
		}
		return nil
	})
	return latency, speed, strings.TrimSpace(report.String()), err
}

// RunSpeedTest measures only the download speed (and the outbound IP), skipping
// the latency probe so a slow link is not charged for it first. The returned
// value is in MB/s.
func (r *Runner) RunSpeedTest(ent *db.ProxyEntity) (float64, string, error) {
	var speed float64
	var report strings.Builder
	_, err := r.withTestCore(ent, 12*time.Second, func(proxyPort int) error {
		ctx := r.base()
		timeout := time.Duration(r.s.TestDownloadTimeout) * time.Second
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		s, err := downloadSpeedThrough(ctx, proxyPort, r.s.TestDownloadURL, timeout)
		if err != nil {
			fmt.Fprintf(&report, "下载: 失败 (%v)\n", err)
			return err
		}
		speed = s
		fmt.Fprintf(&report, "下载: %.2f MB/s\n", s)
		if err := ctx.Err(); err != nil {
			return err
		}
		if !r.s.VPNMode() {
			if ip, err := inboundIP(8 * time.Second); err == nil {
				fmt.Fprintf(&report, "入口 IP: %s\n", ip)
			}
		}
		if ip, err := outboundIP(proxyPort, 10*time.Second); err == nil {
			fmt.Fprintf(&report, "出口 IP: %s\n", ip)
		}
		return nil
	})
	return speed, strings.TrimSpace(report.String()), err
}

// withTestCore builds a temporary config, launches sing-box and invokes fn with
// the local proxy port.
func (r *Runner) withTestCore(ent *db.ProxyEntity, wait time.Duration, fn func(proxyPort int) error) (int, error) {
	res, err := configbuilder.BuildConfig(ent, r.mgr, r.s, true)
	if err != nil {
		return 0, err
	}

	port, err := freePort()
	if err != nil {
		return 0, err
	}
	defer releasePort(port)
	// Keep the inbounds built by the builder: external cores add "-mapping"
	// inbounds that the chain route rules reference, replacing the list would
	// leave those rules pointing at a missing inbound and sing-box rejects it.
	inbounds, _ := res.Config["inbounds"].([]interface{})
	inbounds = append(inbounds, map[string]interface{}{
		"type":        "mixed",
		"tag":         "test-in",
		"listen":      "127.0.0.1",
		"listen_port": port,
	})
	res.Config["inbounds"] = inbounds

	route, _ := res.Config["route"].(map[string]interface{})
	if route == nil {
		route = map[string]interface{}{}
		res.Config["route"] = route
	}
	// route traffic through the profile outbound when it exists (raw sing-box
	// configs may not have a "proxy" tag at all).
	if hasOutboundTag(res.Config, proxyTag) {
		route["final"] = proxyTag
	} else if _, ok := route["final"]; !ok {
		route["final"] = "direct"
	}

	// Verbose logging so a failed probe can be explained; it is only forwarded
	// to the UI when the test fails.
	res.Config["log"] = map[string]interface{}{"level": "debug"}

	exe, err := core.ResolveCore(r.corePath, r.configDir)
	if err != nil {
		return 0, err
	}

	tmpDir := filepath.Join(r.configDir, "temp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return 0, err
	}
	cfgPath := filepath.Join(tmpDir, fmt.Sprintf("test-%d-%d.json", ent.ID, time.Now().UnixNano()))
	data, err := json.MarshalIndent(res.Config, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return 0, err
	}
	defer os.Remove(cfgPath)

	cmd := exec.Command(exe, "run", "-c", cfgPath)
	cmd.Dir = r.configDir
	buf := &syncBuffer{}
	cmd.Stdout = buf
	cmd.Stderr = buf
	util.HideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start core: %w", err)
	}

	// Reap the process so a core that dies immediately (invalid config, missing
	// dependency) is reported at once instead of polling the port until the
	// whole timeout expires.
	exited := make(chan struct{})
	var exitErr error
	go func() {
		exitErr = cmd.Wait()
		close(exited)
	}()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-exited:
		case <-time.After(5 * time.Second): // never block the test on a stuck reaper
		}
	}()

	// external cores required by the profile
	var extCmds []*exec.Cmd
	for _, spec := range res.Externals {
		if spec.Program == "" {
			continue
		}
		// The path is the one the reader typed into the extra-core setting, and
		// it may be relative. It is taken against the folder the executable sits
		// in — the same rule the controller follows — and never against the
		// working directory: on Windows a relative program path is resolved by
		// CreateProcess against the PARENT's current directory, and cmd.Dir does
		// not take part in it. So a relative path started a core from somewhere
		// else, the node worked perfectly through the app, and every speed test
		// of it failed with "core did not become ready in time".
		ec := exec.Command(util.ResolveAppPath(spec.Program), spec.Args...)
		ec.Dir = r.configDir
		if len(spec.Env) > 0 {
			ec.Env = append(os.Environ(), spec.Env...)
		}
		util.HideConsole(ec)
		if err := ec.Start(); err != nil {
			// The error used to be dropped on the floor, so the test that failed
			// said nothing about the external core at all.
			r.logf(fmt.Sprintf("[test] external core %s failed to start: %v", spec.Tag, err))
			continue
		}
		extCmds = append(extCmds, ec)
	}
	defer func() {
		for _, e := range extCmds {
			if e.Process != nil {
				_ = e.Process.Kill()
				_, _ = e.Process.Wait()
			}
		}
	}()

	if err := waitPort(r.base(), port, wait, exited); err != nil {
		// sing-box colourises its log and echoes the whole temp path; strip both
		// so the message stays readable in the UI toast.
		msg := strings.ReplaceAll(tail(stripANSI(buf.String()), 400), cfgPath, filepath.Base(cfgPath))
		if err == errCoreExited {
			if exitErr != nil {
				return 0, fmt.Errorf("core exited (%v): %s", exitErr, msg)
			}
			return 0, fmt.Errorf("core exited: %s", msg)
		}
		return 0, fmt.Errorf("%v: %s", err, msg)
	}

	if err := fn(port); err != nil {
		// The probe failed. Give the core a moment to flush the log for this
		// connection, otherwise we only capture the inbound line. Cancelled runs skip
		// the pause: there is no failure to explain, and the drain the caller is
		// waiting on should not be spent on a report nobody asked for.
		select {
		case <-r.base().Done():
			return 0, r.base().Err()
		case <-time.After(300 * time.Millisecond):
		}
		output := stripANSI(buf.String())
		r.reportCoreOutput(ent, output)
		if hint := coreHint(output); hint != "" {
			return 0, fmt.Errorf("%v | core: %s", err, hint)
		}
		return 0, err
	}
	return port, nil
}

// coreHint picks the most useful line of core output: the last error-ish line,
// falling back to the last non-empty line.
func coreHint(output string) string {
	var last, lastErr string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		last = line
		l := strings.ToLower(line)
		if strings.Contains(l, "error") || strings.Contains(l, "fatal") ||
			strings.Contains(l, "failed") || strings.Contains(l, "reject") ||
			strings.Contains(l, "timeout") || strings.Contains(l, "refused") {
			lastErr = line
		}
	}
	if lastErr != "" {
		return tail(lastErr, 220)
	}
	return tail(last, 220)
}

// --- primitives ---

func proxyClient(proxyPort int, timeout time.Duration) *http.Client {
	proxyURL := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", proxyPort)}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxyURL),
			DisableKeepAlives: true,
		},
	}
}

func urlTest(ctx context.Context, proxyPort int, testURL string, timeout time.Duration) (int, error) {
	client := proxyClient(proxyPort, timeout)
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	ms := int(time.Since(start).Milliseconds())
	if ms <= 0 {
		ms = 1
	}
	return ms, nil
}

func downloadSpeed(ctx context.Context, proxyPort int, dlURL string, timeout time.Duration) (float64, error) {
	if dlURL == "" {
		return 0, fmt.Errorf("no download url")
	}
	client := proxyClient(proxyPort, timeout)
	start := time.Now()
	// The request carries the run's context, so a cancelled run stops reading a
	// payload of tens of megabytes instead of finishing it first: this is the step
	// that made the stop button look broken.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// A source that answers with a redirect page or a portal error is not a speed:
	// carrying on would count the few hundred bytes it sent and report 0.00 MB/s —
	// a number that reads as "the node is slow" when what happened is that the
	// source never sent a payload to measure.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start).Seconds()
	if err != nil || elapsed <= 0 || n < 100<<10 {
		return 0, fmt.Errorf("download failed (%d bytes)", n)
	}
	return float64(n) / elapsed / 1024 / 1024, nil
}

// downloadSpeedThrough measures against a list of sources in turn: the reader's own
// choice first, then built-in mirrors. One source refusing a datacentre IP, missing a
// port under one node or answering a portal page used to end the measurement — speed
// shown as 0.00 or lost to a failure that said nothing about the node. With the mirrors,
// a source that does not measure speaks in its error, and the next one still can.
func downloadSpeedThrough(ctx context.Context, proxyPort int, primary string, timeout time.Duration) (float64, error) {
	seen := map[string]bool{}
	var urls []string
	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	add(primary)
	add("http://cachefly.cachefly.net/10mb.test")
	add("https://speed.cloudflare.com/__down?bytes=25000000")
	var last error
	for _, u := range urls {
		if err := ctx.Err(); err != nil {
			// A cancelled run does not try the next source: the mirrors are there for
			// a source that cannot measure, not for a run nobody is waiting on.
			return 0, err
		}
		s, err := downloadSpeed(ctx, proxyPort, u, timeout)
		if err == nil {
			return s, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no usable source")
	}
	return 0, last
}

// inboundIP reports the address this machine reaches the internet as, asked without a
// proxy: the other half of the report's pair. The exit address is what the node
// presents to the world; this one is what the reader's own network sees, which is the
// address a service has to be told to allow.
//
// Its own Transport rather than the default one, which would honour HTTP_PROXY from the
// environment and answer with the proxy's address under the name "inbound".
func inboundIP(timeout time.Duration) (string, error) {
	// Its own Transport rather than the default one, which would honour HTTP_PROXY from
	// the environment and answer with the proxy's address under the name "inbound".
	client := &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{},
	}
	if ip := util.AskIP(client); ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("no address service answered")
}

func outboundIP(proxyPort int, timeout time.Duration) (string, error) {
	client := proxyClient(proxyPort, timeout)
	if ip := util.AskIP(client); ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("no address service answered")
}

var (
	portMu    sync.Mutex
	usedPorts = map[int]bool{}
)

// freePort asks the OS for an unused port and remembers it, so two concurrent
// tests never hand the same port to two cores at once.
func freePort() (int, error) {
	portMu.Lock()
	defer portMu.Unlock()
	for attempt := 0; attempt < 32; attempt++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		p := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if !usedPorts[p] {
			usedPorts[p] = true
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port available")
}

// releasePort forgets a port handed out by freePort.
func releasePort(p int) {
	portMu.Lock()
	delete(usedPorts, p)
	portMu.Unlock()
}

// errCoreExited is returned by waitPort when the core process ended before the
// test port became reachable.
var errCoreExited = fmt.Errorf("core exited")

// waitPort blocks until the core listens on port, the process exits, or the
// timeout elapses.
func waitPort(ctx context.Context, port int, timeout time.Duration, exited <-chan struct{}) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			// The run was stopped while the test core was still coming up: the core
			// is killed by the caller's own defer, and this returns at once rather
			// than waiting out a readiness that no longer matters.
			return ctx.Err()
		case <-exited:
			return errCoreExited
		default:
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("core did not become ready in time")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// tail returns the last n bytes of s, trimmed.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

// stripANSI removes terminal colour escape sequences from core output.
func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				j++ // consume the sequence terminator
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// syncBuffer is a concurrency safe bytes.Buffer: the core writes to it from
// os/exec's copying goroutines while the test reads it for error messages.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
