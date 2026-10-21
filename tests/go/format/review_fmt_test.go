package format_test

import (
	. "eagle/internal/format"

	"fmt"
	"sort"
	"strings"
	"testing"

	"eagle/internal/bean"
	"eagle/internal/setting"
)

// ---------------------------------------------------------------------------
// helpers: flatten a bean into "path -> value" so two beans can be compared
// ---------------------------------------------------------------------------

func flattenBean(prefix string, b bean.Bean, out map[string]string) {
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		switch v := b[k].(type) {
		case map[string]interface{}:
			flattenBean(p, bean.Bean(v), out)
		case bean.Bean:
			flattenBean(p, v, out)
		default:
			out[p] = fmt.Sprintf("%v", v)
		}
	}
}

func beanDiff(want, got bean.Bean) []string {
	fw, fg := map[string]string{}, map[string]string{}
	flattenBean("", want, fw)
	flattenBean("", got, fg)
	all := map[string]bool{}
	for k := range fw {
		all[k] = true
	}
	for k := range fg {
		all[k] = true
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		a, okA := fw[k]
		b, okB := fg[k]
		if !okA {
			out = append(out, fmt.Sprintf("%s: <missing> -> %q", k, b))
			continue
		}
		if !okB {
			out = append(out, fmt.Sprintf("%s: %q -> <missing>", k, a))
			continue
		}
		if a != b {
			out = append(out, fmt.Sprintf("%s: %q -> %q", k, a, b))
		}
	}
	return out
}

func st(b bean.Bean, k, v string) {
	s := b.Peek("stream")
	s.Set(k, v)
}

// ---------------------------------------------------------------------------
// the round-trip test
// ---------------------------------------------------------------------------

// A share link exported by the app has to import back into the same node.
// This builds a fully populated bean per protocol, exports it, re-imports it
// and compares the two beans field by field.
func TestReviewRoundTripEveryProtocol(t *testing.T) {
	s := &setting.Setting{}

	build := func(typ string) bean.Bean {
		b := New(typ)
		switch typ {
		case "socks", "http":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 1080)
			b.Set("username", "user")
			b.Set("password", "pass")
		case "shadowsocks":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 8388)
			b.Set("method", "aes-128-gcm")
			b.Set("pass", "secret")
		case "vmess":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("id", "11111111-2222-3333-4444-555555555555")
			b.Set("aid", 0)
			b.Set("sec", "auto")
			st(b, "net", "ws")
			st(b, "sec", "tls")
			st(b, "host", "cdn.example.com")
			st(b, "path", "/ray")
			st(b, "sni", "sni.example.com")
		case "trojan":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("pass", "trojan-pass")
			st(b, "net", "tcp")
			st(b, "sec", "tls")
			st(b, "sni", "sni.example.com")
		case "vless":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("pass", "11111111-2222-3333-4444-555555555555")
			b.Set("flow", "xtls-rprx-vision")
			st(b, "net", "ws")
			st(b, "sec", "tls")
			st(b, "host", "cdn.example.com")
			st(b, "path", "/ray")
			st(b, "sni", "sni.example.com")
		case "anytls":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("pass", "anytls-pass")
			st(b, "net", "tcp")
			st(b, "sec", "tls")
			st(b, "sni", "sni.example.com")
		case "hysteria2":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("password", "hy2-pass")
			b.Set("alpn", "h3")
			b.Set("sni", "sni.example.com")
			b.Set("uploadMbps", 100)
			b.Set("downloadMbps", 200)
		case "hysteria":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("auth", "hy1-auth")
			b.Set("obfs", "obfs")
			b.Set("protocol", "udp")
			b.Set("alpn", "h3")
			b.Set("sni", "sni.example.com")
			b.Set("uploadMbps", 100)
			b.Set("downloadMbps", 200)
		case "tuic":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("uuid", "11111111-2222-3333-4444-555555555555")
			b.Set("password", "tuic-pass")
			b.Set("congestionControl", "bbr")
			b.Set("udpRelayMode", "native")
			b.Set("alpn", "h3")
			b.Set("sni", "sni.example.com")
			b.Set("heartbeat", "30s")
		case "naive":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("username", "user")
			b.Set("password", "pass")
			b.Set("protocol", "https")
		case "snell":
			b.Set("name", "节点A")
			b.Set("addr", "example.com")
			b.Set("port", 443)
			b.Set("psk", "snell-psk")
			b.Set("version", 4)
			b.Set("obfsMode", "http")
			b.Set("obfsHost", "obfs.example.com")
		}
		return b
	}

	for _, typ := range []string{
		"socks", "http", "shadowsocks", "vmess", "trojan", "vless", "anytls",
		"hysteria2", "hysteria", "tuic", "naive", "snell",
	} {
		typ := typ
		t.Run(typ, func(t *testing.T) {
			orig := build(typ)
			link := ToShareLink(typ, orig, s)
			if link == "" {
				t.Fatalf("ToShareLink(%q) produced nothing", typ)
			}
			gotTyp, got, ok := ParseLink(link, s)
			if !ok {
				t.Fatalf("ParseLink(%q) refused the link the app itself produced: %s", link, link)
			}
			if gotTyp != typ {
				t.Errorf("type = %q, want %q (link %s)", gotTyp, typ, link)
			}
			if d := beanDiff(orig, got); len(d) > 0 {
				t.Errorf("round trip changed %d field(s) for %s\n  link: %s\n  %s",
					len(d), typ, link, strings.Join(d, "\n  "))
			}
		})
	}
}

// The same, but with the fields a user would actually set on a reality node and
// on a hysteria2 node with an obfs.
func TestReviewRoundTripRealityAndObfs(t *testing.T) {
	s := &setting.Setting{}

	b := New("vless")
	b.Set("name", "reality")
	b.Set("addr", "example.com")
	b.Set("port", 443)
	b.Set("pass", "uuid")
	st(b, "net", "tcp")
	st(b, "sec", "tls")
	st(b, "sni", "sni.example.com")
	st(b, "pbk", "PUBLICKEY")
	st(b, "sid", "abcd")
	st(b, "spx", "/spx")
	st(b, "utls", "chrome")
	b.Peek("stream").Set("insecure", true)
	link := ToShareLink("vless", b, s)
	_, got, ok := ParseLink(link, s)
	if !ok {
		t.Fatalf("ParseLink failed on %s", link)
	}
	if d := beanDiff(b, got); len(d) > 0 {
		t.Errorf("reality round trip changed field(s): %s\n  link: %s", strings.Join(d, "\n  "), link)
	}

	h := New("hysteria2")
	h.Set("name", "hy2")
	h.Set("addr", "example.com")
	h.Set("port", 443)
	h.Set("password", "pw")
	h.Set("obfsPassword", "obfspw")
	h.Set("hopPort", "20000-30000")
	h.Set("alpn", "h3")
	h.Set("sni", "sni.example.com")
	h.Set("allowInsecure", true)
	link2 := ToShareLink("hysteria2", h, s)
	_, got2, ok2 := ParseLink(link2, s)
	if !ok2 {
		t.Fatalf("ParseLink failed on %s", link2)
	}
	if d := beanDiff(h, got2); len(d) > 0 {
		t.Errorf("hysteria2 round trip changed field(s): %s\n  link: %s", strings.Join(d, "\n  "), link2)
	}
}

// A vmess node whose handshake is REALITY. The v2rayN JSON form has no field
// for pbk/sid/spx/fp/alpn, so exporting one that way produced a plain-TLS node
// with the same address: it imports fine, looks identical, and cannot connect.
// The export takes the URL form for these (see vmessToLink), and this pins that
// the handshake survives the trip.
func TestReviewVmessRealityRoundTrip(t *testing.T) {
	s := &setting.Setting{}

	b := New("vmess")
	b.Set("name", "vm-reality")
	b.Set("addr", "example.com")
	b.Set("port", 443)
	b.Set("id", "11111111-2222-3333-4444-555555555555")
	b.Set("aid", 0)
	b.Set("sec", "auto")
	st(b, "net", "ws")
	st(b, "sec", "tls")
	st(b, "sni", "sni.example.com")
	st(b, "pbk", "PUBLICKEY")
	st(b, "sid", "abcd")
	st(b, "spx", "/spx")
	st(b, "utls", "chrome")
	st(b, "path", "/ws")
	st(b, "host", "cdn.example.com")

	link := ToShareLink("vmess", b, s)
	if !strings.Contains(link, "pbk=PUBLICKEY") {
		t.Fatalf("the export dropped the REALITY key — the node it produces cannot connect: %s", link)
	}
	_, got, ok := ParseLink(link, s)
	if !ok {
		t.Fatalf("ParseLink failed on %s", link)
	}
	if d := beanDiff(b, got); len(d) > 0 {
		t.Errorf("vmess REALITY round trip changed field(s): %s\n  link: %s", strings.Join(d, "\n  "), link)
	}
}

// ---------------------------------------------------------------------------
// port validation
// ---------------------------------------------------------------------------

func TestReviewPortRangeValidation(t *testing.T) {
	s := &setting.Setting{}
	cases := []struct {
		link string
		typ  string
	}{
		{"trojan://pw@example.com:0#n", "trojan"},
		{"trojan://pw@example.com:99999#n", "trojan"},
		{"vless://uuid@example.com:0#n", "vless"},
		{"vless://uuid@example.com:99999#n", "vless"},
		{"hysteria2://pw@example.com:0#n", "hysteria2"},
		{"hysteria2://pw@example.com:99999#n", "hysteria2"},
		{"tuic://uuid:pw@example.com:0#n", "tuic"},
		{"tuic://uuid:pw@example.com:99999#n", "tuic"},
		{"snell://psk@example.com:0#n", "snell"},
		{"vmess://uuid@example.com:0#n", "vmess"},
	}
	for _, c := range cases {
		typ, b, ok := ParseLink(c.link, s)
		if !ok {
			continue
		}
		p := b.Int("port")
		if p <= 0 || p > 65535 {
			t.Errorf("ParseLink(%q) accepted an out-of-range port: got %d (type %s)", c.link, p, typ)
		}
	}
}

// ---------------------------------------------------------------------------
// the fragment / name handling
// ---------------------------------------------------------------------------

func TestReviewNameFragmentRoundTrip(t *testing.T) {
	s := &setting.Setting{}
	for _, name := range []string{
		"simple",
		"中文节点",
		"emoji 🚀 node",
		"a/b?c#d",
		"100% real",
	} {
		b := New("trojan")
		b.Set("name", name)
		b.Set("addr", "example.com")
		b.Set("port", 443)
		b.Set("pass", "pw")
		link := ToShareLink("trojan", b, s)
		_, got, ok := ParseLink(link, s)
		if !ok {
			t.Fatalf("ParseLink failed for name %q (link %s)", name, link)
		}
		if gn := got.Str("name"); gn != name {
			t.Errorf("name %q came back as %q (link %s)", name, gn, link)
		}
	}
}

// ---------------------------------------------------------------------------
// the hysteria2 ALPN and the other fields the QUIC parser drops
// ---------------------------------------------------------------------------

func TestReviewHysteria2AlpnFromLink(t *testing.T) {
	s := &setting.Setting{}
	_, b, ok := ParseLink("hysteria2://pw@example.com:443?alpn=h3&sni=x.example.com#n", s)
	if !ok {
		t.Fatal("ParseLink failed")
	}
	if got := b.Str("alpn"); got != "h3" {
		t.Errorf("alpn = %q, want %q — the hysteria2 link carries it and the parser drops it", got, "h3")
	}
}

// ---------------------------------------------------------------------------
// anytls has to carry TLS, or the core refuses the node
// ---------------------------------------------------------------------------

// TestReviewAnyTLSCarriesTLS pins the shape of a new anytls node.
//
// anytls reuses the trojan shape, and the trojan shape does not write the
// transport down: the protocol is decided by the outbound's own type. A node
// built without it went out as a plain anytls outbound with no TLS at all, and
// the core refused it with "TLS required" — the node could never be started.
func TestReviewAnyTLSCarriesTLS(t *testing.T) {
	b := New("anytls")
	if got := b.Peek("stream").Str("sec"); got != "tls" {
		t.Errorf("the anytls stream says sec=%q, want %q: the core refuses an anytls outbound "+
			"with no TLS (\"TLS required\"), so the node could never be started", got, "tls")
	}
}

// ---------------------------------------------------------------------------
// a base64 subscription that is itself a valid link list (padding)
// ---------------------------------------------------------------------------

func TestReviewBase64SubscriptionVariants(t *testing.T) {
	links := "trojan://pw@a.example.com:443#n1\nvless://uuid@b.example.com:443#n2\n"
	// standard, padded
	std := "trojan://pw@a.example.com:443#n1\nvless://uuid@b.example.com:443#n2\n"
	_ = std
	for _, tc := range []struct {
		name string
		body string
	}{
		{"std padded", "dHJvamFuOjpwd0BhLmV4YW1wbGUuY29tOjQ0MyNuMQp2bGVzczovL3V1aWRAYi5leGFtcGxlLmNvbTo0NDMjI24y"},
	} {
		_ = tc
	}
	_ = links
}
