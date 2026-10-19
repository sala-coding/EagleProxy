// The two questions this file answers — "what address am I seen as" and "which country
// is this address in" — are both asked of public services over the open internet, and
// both of them used to be asked of one service each. Neither survived contact with a
// real network: api.ipify.org answered 502 on one attempt and the address on the next,
// and ip-api.com answered nothing at all but a timeout. So each question now has a list,
// and the first service that answers wins.

package util

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ipEchoURLs are the services asked for this machine's address, in the order they are
// tried. The list leads with the ones that answered on the network where the others did
// not; being fast is not the point, being able to answer at all is.
var ipEchoURLs = []string{
	"https://ip.3322.net",
	"https://icanhazip.com",
	"https://api.ip.sb/ip",
	"https://ifconfig.me/ip",
	"http://api.ipify.org",
}

// ProbeHosts returns the bare host names of the address-echo services, in the order they
// are tried.
//
// It exists for the config builder, which pins these hosts to the proxy outbound in rule
// mode: a probe whose request is routed by the very rules it is measuring is not
// measuring the exit. Under a domestic-direct profile, a probe host that resolves into a
// domestic address is sent straight out by the geoip rule and answers with the reader's
// own address — the one number the probe exists to rule out. Keeping the list here means
// the pins and the probes cannot drift apart.
func ProbeHosts() []string {
	out := make([]string, 0, len(ipEchoURLs))
	for _, u := range ipEchoURLs {
		rest := strings.TrimPrefix(u, "https://")
		rest = strings.TrimPrefix(rest, "http://")
		if i := strings.Index(rest, "/"); i >= 0 {
			rest = rest[:i]
		}
		out = append(out, rest)
	}
	return out
}

// askFirst races the attempts and answers with the first usable value.
//
// In parallel rather than in turn, and that is the difference between a corner that fills
// in a second and one that waits out every timeout in the list: the moment this is asked —
// right after a start, when the tunnel is up but its outbound is not — most services cannot
// answer, and in the serial form each one held every service behind it for its whole
// timeout. The order of the list still decides ties, and a request that loses costs only
// what it had already sent, because the context below cancels it.
func askFirst(attempts []func(context.Context) (string, bool)) string {
	if len(attempts) == 0 {
		return ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	answers := make(chan string, len(attempts))
	for _, attempt := range attempts {
		attempt := attempt
		go func() {
			v, ok := attempt(ctx)
			if !ok {
				v = ""
			}
			answers <- v
		}()
	}
	for range attempts {
		if v := <-answers; v != "" {
			cancel()
			return v
		}
	}
	return ""
}

// AskIP returns the address the client is seen as, as reported by whichever echo service
// answers first, or "" when none of them does.
//
// The client carries its own timeout and, where it matters, its own proxy: asking through
// a proxy and asking around one are two different questions, and this only ever asks.
//
// The reply has to be the bare address. Picking one out of a page that carries more would
// be guessing, and a guess here is shown to the reader as a fact about where they are.
func AskIP(client *http.Client) string {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	attempts := make([]func(context.Context) (string, bool), 0, len(ipEchoURLs))
	for _, u := range ipEchoURLs {
		u := u
		attempts = append(attempts, func(ctx context.Context) (string, bool) {
			data, ok := fetch(ctx, client, u)
			if !ok {
				return "", false
			}
			ip := strings.TrimSpace(string(data))
			return ip, net.ParseIP(ip) != nil
		})
	}
	return askFirst(attempts)
}

// countrySources are the services asked which country an address is registered in, in
// the order they are tried. Each says how to read its own answer: two of them are JSON
// under a key and one is the bare code on a line, and a reader that guessed would be
// scraping a page rather than reading an API.
var countrySources = []struct {
	url   func(ip string) string
	parse func([]byte) string
}{
	{
		func(ip string) string { return "https://ipwho.is/" + ip + "?fields=country_code" },
		jsonField("country_code"),
	},
	{
		func(ip string) string { return "https://api.country.is/" + ip },
		jsonField("country"),
	},
	{
		func(ip string) string { return "https://ipinfo.io/" + ip + "/country" },
		plainCode,
	},
}

// AskCountry returns the two-letter country code an address is registered in, lower
// case, or "" when none of the services can say. The case is part of the answer rather
// than a detail of it: the flags are looked up by it (see fa/flag in the front end).
func AskCountry(client *http.Client, ip string) string {
	if ip == "" {
		return ""
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	attempts := make([]func(context.Context) (string, bool), 0, len(countrySources))
	for _, src := range countrySources {
		u := src.url(ip)
		parse := src.parse
		attempts = append(attempts, func(ctx context.Context) (string, bool) {
			data, ok := fetch(ctx, client, u)
			if !ok {
				return "", false
			}
			cc := parse(data)
			return cc, cc != ""
		})
	}
	return askFirst(attempts)
}

// fetch performs one request and returns its body, capped. The cap is what keeps a
// service that decides to answer with a page from being read into memory in full. The
// context is what lets a losing service be dropped the moment a winner answers.
func fetch(ctx context.Context, client *http.Client, url string) ([]byte, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	if err != nil {
		return nil, false
	}
	return data, true
}

func jsonField(key string) func([]byte) string {
	return func(data []byte) string {
		var m map[string]string
		if json.Unmarshal(data, &m) != nil {
			return ""
		}
		return normalizeCC(m[key])
	}
}

func plainCode(data []byte) string {
	return normalizeCC(string(data))
}

// normalizeCC keeps a two-letter code and drops everything else, including the "XX" some
// services answer with when they do not know — which is not a country, and a flag for it
// does not exist.
func normalizeCC(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 2 || s == "xx" {
		return ""
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return s
}
