// Package netx provides the HTTP client used for subscription fetching.
package netx

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"eagle/internal/setting"
)

// Response mirrors nekoray's NekoHTTPResponse.
type Response struct {
	Error  string
	Data   string
	Header http.Header
}

// UserAgents resolves the client identifiers a subscription is fetched with, in
// the order they should be tried.
//
// The setting holds them one per line. It is a list rather than a single value
// because of what a panel does with it: one that does not recognise the identifier
// answers without the account's quota header, and it answers *successfully* — the
// nodes arrive, the update looks fine, and the only thing missing is the traffic
// figure. Which identifier a given panel wants is not something a reader can be
// expected to know, so the fetcher walks the list instead.
//
// Blank lines and surrounding space are dropped, and so are repeats: a list that
// has been pasted twice should not cost two identical requests.
func UserAgents(s *setting.Setting) []string {
	raw := ""
	if s != nil {
		raw = s.UserAgent
	}
	if out := parseUserAgents(raw); len(out) > 0 {
		return out
	}
	// Nothing configured, or nothing but blank lines: the built-in list, which is
	// also what the settings form starts out holding.
	return setting.DefaultUserAgents()
}

func parseUserAgents(raw string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		ua := strings.TrimSpace(line)
		if ua == "" || seen[ua] {
			continue
		}
		seen[ua] = true
		out = append(out, ua)
	}
	return out
}

// UserAgent resolves the one identifier to name in a message: the first of the
// list, which is the one a fetch will try first.
func UserAgent(s *setting.Setting) string {
	if list := UserAgents(s); len(list) > 0 {
		return list[0]
	}
	return ""
}

// HttpGet performs a GET request with nekoray-compatible options
// (10s timeout, redirects, optional local proxy and TLS verification skip).
//
// The client identifier is a parameter rather than being read from the settings
// here: a subscription fetch walks the configured list, trying one identifier per
// request, and a function that resolved the list itself could only ever send the
// first of them.
func HttpGet(rawURL string, s *setting.Setting, startedID int, userAgent string) Response {
	transport := &http.Transport{
		// The idle timeout is what lets the connection the response body hands
		// back to the pool be closed again. Without it the pool keeps the
		// connection forever, and the two goroutines reading and writing it keep
		// the transport itself reachable — so every subscription fetch leaked a
		// socket and three goroutines, for good.
		IdleConnTimeout: 30 * time.Second,
	}
	// Registered before the body's Close below, so it runs after it: the
	// connection has to be back in the pool before the pool can be emptied.
	defer transport.CloseIdleConnections()

	if s != nil && s.SubUseProxy {
		if startedID < 0 {
			return Response{Error: "Request with proxy but no profile started."}
		}
		// sing-box mixed inbound: use HTTP proxy semantics like nekoray
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("127.0.0.1:%d", s.InboundSocksPort),
		}
		if s.InboundAuth != nil && s.InboundAuth.NeedAuth() {
			proxyURL.User = url.UserPassword(s.InboundAuth.Username, s.InboundAuth.Password)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	if s != nil && s.SubInsecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return Response{Error: err.Error()}
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return Response{Error: err.Error()}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{Error: err.Error(), Header: resp.Header}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{
			Error:  fmt.Sprintf("HTTP %d %s", resp.StatusCode, resp.Status),
			Data:   string(data),
			Header: resp.Header,
		}
	}
	return Response{Data: string(data), Header: resp.Header}
}
