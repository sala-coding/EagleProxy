// Package speedtest measures latency / download speed of profiles.
//
// Every mode spins up a temporary sing-box instance with the profile's outbound
// and a local mixed inbound, then probes through it — the measurement is the
// node's end-to-end answer, not the machine's bare reachability to the node.
package speedtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
// What each mode produces: URLTest and FullTest yield a latency, FullTest and
// SpeedTest a speed — the frontend uses that to decide which column of a result
// to refresh. Mode 1 is retired: it was the TCP ping, a raw handshake to the
// node's port that a UDP-only node can never answer and that a TUN run dialed
// through the current node — the numbers read as measurements and were neither.
const (
	ModeURLTest   = 0
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

// log writes one line to the run's log panel when a logger is attached.
func (r *Runner) log(line string) {
	if r.logf != nil {
		r.logf(line)
	}
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
//
// The samples go out at once rather than one after another: serially they were the
// whole of a latency test's wall time — three round trips per node — and a group
// test with them in line read as a run that never went wide no matter what the
// scheduler above it held. The median is unchanged by firing together (each sample
// is its own connection), and the short burst is what a browser does too.
func probeLatency(ctx context.Context, proxyPort int, testURL string, timeout time.Duration) (int, error) {
	type sample struct {
		ms  int
		err error
	}
	out := make([]sample, latencySamples)
	var wg sync.WaitGroup
	for i := 0; i < latencySamples; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ms, err := urlTest(ctx, proxyPort, testURL, timeout)
			out[i] = sample{ms, err}
		}(i)
	}
	wg.Wait()
	samples := make([]int, 0, latencySamples)
	var lastErr error
	for _, s := range out {
		if s.err != nil {
			// A cancelled run is the caller's answer, not a missing sample: the
			// run was stopped, and whatever landed is not a measurement of anything.
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			lastErr = s.err
			continue
		}
		samples = append(samples, s.ms)
	}
	if len(samples) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("no successful probe")
		}
		return 0, lastErr
	}
	sort.Ints(samples)
	// A real median, not just the middle element. When one of the three fails the
	// count that survives is even, and the middle element of an even list is the
	// upper of the two middles — every dropped sample would tilt the answer toward
	// the slow side, which is the long tail the median was chosen to reject.
	mid := len(samples) / 2
	if len(samples)%2 == 0 {
		return (samples[mid-1] + samples[mid]) / 2, nil
	}
	return samples[mid], nil
}

// RunFullTest measures latency, both directions and the outbound IP. A direction
// is -1 when its measurement failed, so the list can show it as unavailable.
func (r *Runner) RunFullTest(ent *db.ProxyEntity) (int, float64, float64, string, error) {
	var latency int
	down, up := 0.0, 0.0
	var report strings.Builder
	_, err := r.withTestCore(ent, 40*time.Second, func(proxyPort int) error {
		ctx := r.base()
		ms, err := probeLatency(ctx, proxyPort, r.s.TestLatencyURL, 10*time.Second)
		if err != nil {
			return err
		}
		latency = ms
		fmt.Fprintf(&report, "延迟: %d ms\n", ms)

		rd, err := measureSpeed(ctx, proxyPort, r.s)
		if rd.server != "" {
			r.log("[test] " + rd.server)
		}
		down, up = speedColumns(rd)
		if rd.down > 0 {
			fmt.Fprintf(&report, "下载: %.2f MB/s\n", rd.down)
		} else {
			fmt.Fprintf(&report, "下载: 失败 (%v)\n", err)
		}
		if rd.up > 0 {
			fmt.Fprintf(&report, "上传: %.2f MB/s\n", rd.up)
		} else if rd.upErr != "" {
			fmt.Fprintf(&report, "上传: 失败 (%s)\n", rd.upErr)
		}
		// The address lookups are the last two steps and they carry their own
		// timeouts; a run that has been cancelled skips them rather than spending
		// another eighteen seconds on a report nobody is going to read.
		if err := ctx.Err(); err != nil {
			return err
		}
		// Both asks go out together and the report keeps the reading order: serially
		// the pair cost up to eighteen seconds of tail per node, which is what a
		// group test spent waiting after every download had already landed. One
		// goroutine each, answer written before the join — no lock needed, the
		// addresses are disjoint variables.
		var inIP, outIP string
		var wg sync.WaitGroup
		// The machine's own address: skipped in TUN mode, where every request is
		// taken by the core and "direct" would report the same address twice under
		// two names.
		if !r.s.VPNMode() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if ip, err := inboundIP(8 * time.Second); err == nil {
					inIP = ip
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ip, err := outboundIP(proxyPort, 10*time.Second); err == nil {
				outIP = ip
			}
		}()
		wg.Wait()
		if inIP != "" {
			fmt.Fprintf(&report, "入口 IP: %s\n", inIP)
		}
		if outIP != "" {
			fmt.Fprintf(&report, "出口 IP: %s\n", outIP)
		}
		return nil
	})
	return latency, down, up, strings.TrimSpace(report.String()), err
}

// RunSpeedTest measures only the two speeds (and the outbound IP), skipping the
// latency probe so a slow link is not charged for it first. Both values are MB/s.
func (r *Runner) RunSpeedTest(ent *db.ProxyEntity) (float64, float64, string, error) {
	var down, up float64
	var report strings.Builder
	_, err := r.withTestCore(ent, 12*time.Second, func(proxyPort int) error {
		ctx := r.base()
		rd, err := measureSpeed(ctx, proxyPort, r.s)
		if rd.server != "" {
			r.log("[test] " + rd.server)
		}
		if err != nil {
			fmt.Fprintf(&report, "下载: 失败 (%v)\n", err)
			return err
		}
		down, up = speedColumns(rd)
		fmt.Fprintf(&report, "下载: %.2f MB/s\n", rd.down)
		if rd.up > 0 {
			fmt.Fprintf(&report, "上传: %.2f MB/s\n", rd.up)
		} else if rd.upErr != "" {
			fmt.Fprintf(&report, "上传: 失败 (%s)\n", rd.upErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Same pair, same shape as the full test's (see there): both asks at once,
		// the report keeping the reading order.
		var inIP, outIP string
		var wg sync.WaitGroup
		if !r.s.VPNMode() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if ip, err := inboundIP(8 * time.Second); err == nil {
					inIP = ip
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ip, err := outboundIP(proxyPort, 10*time.Second); err == nil {
				outIP = ip
			}
		}()
		wg.Wait()
		if inIP != "" {
			fmt.Fprintf(&report, "入口 IP: %s\n", inIP)
		}
		if outIP != "" {
			fmt.Fprintf(&report, "出口 IP: %s\n", outIP)
		}
		return nil
	})
	return down, up, strings.TrimSpace(report.String()), err
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
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxyURL(proxyPort)),
			DisableKeepAlives: true,
		},
	}
}

// urlTest times one cold request through the test core's node and reports the
// time to the answer's head.
//
// The head is the whole measurement. client.Do comes back when the status line
// and headers have arrived, and everything after them is somebody's page rather
// than an answer to "what does this node cost" — timing the body turned the
// latency column into a second speed column for every test URL that is not the
// default 204, where the body is empty by design. Taking any status at all had
// the other half of the same flaw: a portal or a hijacking middlebox answering
// 302 into its login page made the node read as a fast, working connection,
// because redirect-following ends in a 200 and the old code read none of it. So
// redirects are not followed and a non-2xx is an error, the same insistence
// downloadSpeed already has.
func urlTest(ctx context.Context, proxyPort int, testURL string, timeout time.Duration) (int, error) {
	client := proxyClient(proxyPort, timeout)
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	ms := int(time.Since(start).Milliseconds())
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ms <= 0 {
		ms = 1
	}
	return ms, nil
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
