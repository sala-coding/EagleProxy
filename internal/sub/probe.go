package sub

import (
	"strings"

	"gopkg.in/yaml.v3"

	"eagle/internal/format"
	"eagle/internal/setting"
	"eagle/internal/util"
)

// probeCount walks exactly the dispatch the real parse takes (see updateDepth)
// — base64, then a Clash YAML, then one link per line — and returns two things
// about a candidate answer without ever touching the store: how many of the
// nodes it carries this build could actually import, and which shape the answer
// was read as. The count is the real one, not a guess: a YAML entry is counted
// only when format.New says the protocol has a bean on this side, and a link is
// counted only when ParseLink turns it into a profile — so a subscription that
// never carried anything importable counts as zero, exactly as the update would.
func probeWith(content string, s *setting.Setting) (count int, kind string) {
	return probeDepth(content, s, 0)
}

func probeDepth(str string, s *setting.Setting, depth int) (int, string) {
	if depth > 8 {
		return 0, ""
	}
	str = strings.TrimSpace(str)
	if str == "" {
		return 0, ""
	}

	// base64 encoded subscription — the count lives in what it decodes to, and
	// the shape follows the decoded body.
	if decoded, ok := util.DecodeB64(str); ok && len(decoded) > 0 && string(decoded) != str {
		// The same guard the real parse keeps (see parse.go): a decode that is
		// not text is no subscription — any 4k-multiple of base64-alphabet
		// characters decodes cleanly, and the parse refuses the binary garbage
		// outright rather than walking it. The probe walks exactly the dispatch
		// the parse takes, so the refusal is here too; without it the probe
		// could report a shape (and a deeper walk) for a body the update would
		// answer with "nothing there".
		if !readableBody(decoded) {
			return 0, ""
		}
		return probeDepth(string(decoded), s, depth+1)
	}

	// Clash configuration
	if strings.Contains(str, "proxies:") {
		return countClashProxies(str), "clash"
	}

	// multi line — a link list, or anything a panel splits into lines
	if strings.Contains(str, "\n") {
		total := 0
		kind := ""
		for _, line := range strings.Split(str, "\n") {
			n, k := probeDepth(strings.TrimSpace(line), s, depth)
			if n > 0 {
				total += n
				if kind == "" {
					kind = k
				}
			}
		}
		if total > 0 {
			if kind == "" {
				kind = "links"
			}
			return total, kind
		}
		return 0, ""
	}

	// single link — the only place a profiile either stands or doesn't
	if _, _, ok := format.ParseLink(str, s); ok {
		return 1, "links"
	}
	return 0, ""
}

// countClashProxies counts the importable entries of a Clash YAML's proxies
// list, with the same acceptance rule the real parse applies: shadowsocksR is
// not importable under any name, and a type with no bean on this side (mieru,
// juicity, …) is not either.
func countClashProxies(str string) int {
	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(str), &root); err != nil {
		return 0
	}
	proxies, _ := root["proxies"].([]interface{})
	n := 0
	for _, p := range proxies {
		proxy, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		typ := strings.ToLower(yStr(proxy, "type"))
		if typ == "ssr" {
			continue
		}
		switch typ {
		case "ss":
			typ = "shadowsocks"
		case "socks5":
			typ = "socks"
		}
		if format.New(typ).Int("_v") == -114514 {
			continue
		}
		n++
	}
	return n
}
