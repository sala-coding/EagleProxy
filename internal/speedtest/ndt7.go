package speedtest

// ndt7 is the source of a node's speed numbers.
//
// It used to be one fixed HTTP file: whatever that single host would give over a
// single connection, with no upload measurement at all. ndt7 is M-Lab's open
// measurement protocol — a WebSocket test against a server the locate service
// picks for this client, ten seconds per direction. Run through the test core it
// measures the node's path rather than the machine's, and it measures both
// directions.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"eagle/internal/setting"

	"github.com/gorilla/websocket"
)

const (
	// ndt7Protocol is the subprotocol the upgrade request must carry; a server
	// answers 4xx without it.
	ndt7Protocol = "net.measurementlab.ndt.v7"
	// ndt7LocateURL returns the ndt7 servers nearest to whoever asks — asked
	// through the node, so "nearest" is nearest to the node.
	ndt7LocateURL = "https://locate.measurementlab.net/v2/nearest/ndt/ndt7"
	// ndt7Duration is one direction. The protocol expects "up to ten seconds" and
	// the server closes the download at that point; a shorter test measures the
	// ramp-up rather than the link.
	ndt7Duration = 10 * time.Second
	// ndt7DefaultWindow is one direction's length when the setting says nothing (a
	// store written before it existed).
	ndt7DefaultWindow = 5 * time.Second
	// ndt7MinWindow is the shortest window that still says something about a link.
	ndt7MinWindow = 2 * time.Second
	// ndt7Grace is the backstop past ndt7Duration for a server that never closes.
	ndt7Grace = 3 * time.Second
	// ndt7Chunk is the smallest binary message the protocol allows, and the size
	// its own clients start with.
	ndt7Chunk = 1 << 13
	// ndt7Candidates bounds how many located servers a run will try: one that
	// answers no route is not the node's fault, three in a row is a finding.
	ndt7Candidates = 3
)

// speedColumns turns a reading into the two numbers a row stores: -1 for a
// direction that was measured and failed, 0 for one that was never reached (a
// download that failed takes the upload with it, and the row keeps the upload it
// had rather than inventing a failure for a test that never ran).
func speedColumns(rd speedReading) (down, up float64) {
	down = rd.down
	if down <= 0 {
		down = -1
	}
	switch {
	case rd.up > 0:
		up = rd.up
	case rd.upErr != "":
		up = -1
	}
	return down, up
}

// speedReading is one node's speed measurement.
type speedReading struct {
	down   float64 // MB/s
	up     float64 // MB/s, 0 when it was not measured
	upErr  string  // why the upload produced nothing, if it did not
	server string  // the ndt7 server's name, for the log
}

// measureSpeed measures the node's rates through the test core with the engine and
// the window the reader picked, one window per direction.
//
// Both engines measure a path rather than a file: ndt7 against M-Lab, cloudflare
// against Cloudflare's edge for nodes that have no route to M-Lab.
func measureSpeed(ctx context.Context, proxyPort int, s *setting.Setting) (speedReading, error) {
	window := time.Duration(s.TestWindow) * time.Second
	if window <= 0 {
		window = ndt7DefaultWindow
	}
	// The stored number is the reader's dial, not the protocol's: a shorter window
	// measures the handshake, a longer one is a window the server will not hold.
	if window < ndt7MinWindow {
		window = ndt7MinWindow
	}
	if window > ndt7Duration {
		window = ndt7Duration
	}
	if s.TestEngine == setting.SpeedEngineCloudflare {
		return cloudflareMeasure(ctx, proxyURL(proxyPort), window, s.TestUpload)
	}
	return ndt7Measure(ctx, proxyURL(proxyPort), window, s.TestUpload)
}

// ndt7Target is one located server and the two endpoints the test needs.
type ndt7Target struct {
	name     string
	download string
	upload   string
}

// ndt7Measure picks a server and runs both directions against it. A nil proxy
// dials directly, which is what the live test needs.
func ndt7Measure(ctx context.Context, proxy *url.URL, dur time.Duration, upload bool) (speedReading, error) {
	targets, err := ndt7Locate(ctx, proxy)
	if err != nil {
		return speedReading{}, err
	}
	if len(targets) == 0 {
		return speedReading{}, fmt.Errorf("no ndt7 server offered")
	}
	var lastErr error
	for _, t := range targets {
		down, err := ndt7Download(ctx, t.download, proxy, dur)
		if err != nil {
			if ctx.Err() != nil {
				return speedReading{}, ctx.Err()
			}
			lastErr = err
			continue
		}
		rd := speedReading{down: down, server: t.name}
		if !upload {
			return rd, nil
		}
		// The upload is a second test of the same server. A direction that fails
		// says nothing about the one already measured, so it does not fail the run
		// — it leaves the reason in the report instead.
		up, err := ndt7Upload(ctx, t.upload, proxy, dur)
		if err != nil {
			rd.upErr = err.Error()
		} else {
			rd.up = up
		}
		return rd, nil
	}
	return speedReading{}, lastErr
}

// ndt7Locate asks the locate service for ndt7 servers, through the node.
func ndt7Locate(ctx context.Context, proxy *url.URL) ([]ndt7Target, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ndt7LocateURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("locate: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Results []struct {
			Machine string            `json:"machine"`
			URLs    map[string]string `json:"urls"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("locate: %w", err)
	}
	var out []ndt7Target
	for _, r := range body.Results {
		down, up := r.URLs["wss:///ndt/v7/download"], r.URLs["wss:///ndt/v7/upload"]
		if down == "" || up == "" {
			continue
		}
		out = append(out, ndt7Target{name: r.Machine, download: down, upload: up})
		if len(out) == ndt7Candidates {
			break
		}
	}
	return out, nil
}

// ndt7Download counts what the server pushes until it closes the test.
func ndt7Download(ctx context.Context, rawURL string, proxy *url.URL, dur time.Duration) (float64, error) {
	conn, err := ndt7Dial(ctx, rawURL, proxy)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	stop := ndt7Watch(ctx, conn, true)
	defer stop()

	// The server ends the test itself; the deadline is the backstop for one that
	// does not, and the stop button reaches the same read through ndt7Watch.
	_ = conn.SetReadDeadline(time.Now().Add(dur + ndt7Grace))
	start := time.Now()
	var total int64
	for {
		mt, rd, err := conn.NextReader()
		if err != nil {
			break
		}
		n, err := io.Copy(io.Discard, rd)
		if err != nil {
			break
		}
		if mt == websocket.BinaryMessage {
			total += n
		}
	}
	elapsed := time.Since(start).Seconds()
	// A close from the server is how the test ends, so the read error above is
	// expected: what decides whether there is a measurement is the bytes.
	if total < 100<<10 || elapsed <= 0 {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("ndt7 download read %d bytes", total)
	}
	return float64(total) / elapsed / 1024 / 1024, nil
}

// ndt7Upload pushes binary messages until the write deadline or a close.
func ndt7Upload(ctx context.Context, rawURL string, proxy *url.URL, dur time.Duration) (float64, error) {
	conn, err := ndt7Dial(ctx, rawURL, proxy)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	// One reader and one writer are allowed on a websocket. The reader is here
	// only to drain the server's measurements, whose unread send buffer would
	// otherwise stall our writes near the end of the test.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	stop := ndt7Watch(ctx, conn, false)
	defer stop()
	_ = conn.SetWriteDeadline(time.Now().Add(dur))
	chunk := make([]byte, ndt7Chunk)
	start := time.Now()
	var total int64
	for {
		if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
			break
		}
		total += int64(len(chunk))
	}
	elapsed := time.Since(start).Seconds()
	if total < 100<<10 || elapsed <= 0 {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("ndt7 upload wrote %d bytes", total)
	}
	return float64(total) / elapsed / 1024 / 1024, nil
}

// ndt7Dial opens the websocket through the test core's inbound. The URL comes
// from locate carrying a signed token, which is what makes the server accept
// this client, so it is used as given.
func ndt7Dial(ctx context.Context, rawURL string, proxy *url.URL) (*websocket.Conn, error) {
	d := websocket.Dialer{
		Proxy:            http.ProxyURL(proxy),
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{ndt7Protocol},
	}
	conn, resp, err := d.DialContext(ctx, rawURL, http.Header{"User-Agent": {"eagle-ndt7"}})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ndt7 handshake: %s", resp.Status)
		}
		return nil, err
	}
	return conn, nil
}

// ndt7Watch makes the stop button reach a websocket read or write: neither takes
// a context, so an expired deadline is what interrupts them.
func ndt7Watch(ctx context.Context, conn *websocket.Conn, read bool) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if read {
				_ = conn.SetReadDeadline(time.Now())
			} else {
				_ = conn.SetWriteDeadline(time.Now())
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

// proxyURL is the test core's local inbound.
func proxyURL(port int) *url.URL {
	return &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
}
