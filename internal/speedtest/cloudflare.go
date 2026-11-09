package speedtest

// cloudflare is the second engine: Cloudflare's own speed endpoints, several
// connections, bounded payloads repeated until the window closes.
//
// It is here because ndt7 needs a route to M-Lab, and a node that has none would
// otherwise have no number at all. Cloudflare's edge is reachable from far more
// networks, and its two endpoints measure the same two directions.
//
// Several connections rather than one: the endpoint serves no more than a few
// megabytes per request (a larger `bytes` is refused), and a single stream would
// spend the window waiting out one round trip per chunk.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// cfDownFormat asks for an exact byte count. The endpoint refuses more than a
	// few megabytes at once, so the chunk stays small and the test repeats it.
	cfDownFormat = "https://speed.cloudflare.com/__down?bytes=%d"
	cfUpURL      = "https://speed.cloudflare.com/__up"
	cfChunk      = 5 << 20
	// cfWorkers is how many connections share the window: enough to fill a fast
	// link past what one stream can carry, few enough not to read as a flood.
	cfWorkers = 4
	// cfMinFloor is the least a window has to move before its number means anything.
	cfMinFloor = 1 << 20
)

// cfClient keeps connections alive: the test is a sequence of requests, and a
// fresh TCP/TLS handshake per chunk would be measured as part of the link.
func cfClient(proxy *url.URL) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxy),
			MaxConnsPerHost:     cfWorkers,
			MaxIdleConnsPerHost: cfWorkers,
			IdleConnTimeout:     30 * time.Second,
		},
	}
}

// cloudflareMeasure runs both directions against Cloudflare, each for one window.
func cloudflareMeasure(ctx context.Context, proxy *url.URL, dur time.Duration, upload bool) (speedReading, error) {
	client := cfClient(proxy)
	down, err := cfDownload(ctx, client, dur)
	if err != nil {
		return speedReading{}, err
	}
	rd := speedReading{down: down, server: "speed.cloudflare.com"}
	if !upload {
		return rd, nil
	}
	// Same rule as ndt7: a direction that fails says nothing about the one already
	// measured, so it leaves its reason in the report instead of failing the run.
	if up, err := cfUpload(ctx, client, dur); err != nil {
		rd.upErr = err.Error()
	} else {
		rd.up = up
	}
	return rd, nil
}

// cfDownload repeats one bounded download on every worker until the window closes,
// counting what arrived. The bytes of a request cut off by the window still count —
// the average is over the window, and dropping them would price the tail at zero.
func cfDownload(ctx context.Context, client *http.Client, dur time.Duration) (float64, error) {
	target := fmt.Sprintf(cfDownFormat, cfChunk)
	start := time.Now()
	end := start.Add(dur)
	var total atomic.Int64
	errs := make(chan error, cfWorkers)
	var wg sync.WaitGroup
	for i := 0; i < cfWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(end) {
				n, err := cfGet(ctx, client, target, end)
				if n > 0 {
					total.Add(n)
				}
				if err != nil {
					if ctx.Err() == nil {
						errs <- err
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}
	if total.Load() < cfMinFloor {
		if firstErr != nil {
			return 0, firstErr
		}
		return 0, fmt.Errorf("cloudflare download got %d bytes", total.Load())
	}
	return float64(total.Load()) / time.Since(start).Seconds() / 1024 / 1024, nil
}

// cfGet performs one download and returns the bytes it delivered.
func cfGet(ctx context.Context, client *http.Client, target string, end time.Time) (int64, error) {
	rctx, cancel := context.WithDeadline(ctx, end)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var n int64
	buf := make([]byte, 64<<10)
	for {
		k, err := resp.Body.Read(buf)
		n += int64(k)
		if err != nil {
			// A full request ends in EOF, a cut-off one in the window's deadline;
			// both are the end of this request, not a failure of the test.
			return n, nil
		}
	}
}

// cfUpload streams on every worker until the window closes. The body is generated
// rather than buffered, so what the transport took is the only count available: the
// socket buffer holds a few hundred kilobytes of it at the end, the same shape of
// error ndt7's upload already carries.
func cfUpload(ctx context.Context, client *http.Client, dur time.Duration) (float64, error) {
	start := time.Now()
	end := start.Add(dur)
	var total atomic.Int64
	errs := make(chan error, cfWorkers)
	var wg sync.WaitGroup
	for i := 0; i < cfWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := &cfBody{end: end}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfUpURL, body)
			if err != nil {
				errs <- err
				return
			}
			resp, err := client.Do(req)
			// Whatever the transport took is part of the measurement even when the
			// request ended badly: the window is what is being averaged over.
			total.Add(body.n)
			if err != nil {
				if ctx.Err() == nil {
					errs <- err
				}
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				errs <- fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errs)
	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}
	if total.Load() < cfMinFloor {
		if firstErr != nil {
			return 0, firstErr
		}
		return 0, fmt.Errorf("cloudflare upload sent %d bytes", total.Load())
	}
	return float64(total.Load()) / time.Since(start).Seconds() / 1024 / 1024, nil
}

// cfBody feeds zeros until the window closes, counting what was taken from it.
type cfBody struct {
	end time.Time
	n   int64
}

func (b *cfBody) Read(p []byte) (int, error) {
	if !time.Now().Before(b.end) {
		return 0, io.EOF
	}
	for i := range p {
		p[i] = 0
	}
	b.n += int64(len(p))
	return len(p), nil
}
