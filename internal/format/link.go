package format

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"eagle/internal/bean"
	"eagle/internal/setting"
	"eagle/internal/util"
)

// ParseLink parses a share link into (type, bean). ok is false on failure.
func ParseLink(link string, s *setting.Setting) (string, bean.Bean, bool) {
	link = strings.TrimSpace(link)
	switch {
	case strings.HasPrefix(link, "nekoray://"):
		return parseNekorayLink(link)
	case strings.HasPrefix(link, "vmess://"):
		return parseVMess(link, s)
	case strings.HasPrefix(link, "vless://"):
		return parseTrojanVLESS(link, s, "vless")
	case strings.HasPrefix(link, "trojan://"):
		return parseTrojanVLESS(link, s, "trojan")
	case strings.HasPrefix(link, "ss://"):
		return parseShadowSocks(link)
	case strings.HasPrefix(link, "naive+https://"), strings.HasPrefix(link, "naive+quic://"):
		return parseNaive(link)
	case strings.HasPrefix(link, "tuic://"):
		return parseQUIC(link, "tuic")
	case strings.HasPrefix(link, "hy2://"), strings.HasPrefix(link, "hysteria2://"):
		return parseQUIC(link, "hysteria2")
	case strings.HasPrefix(link, "anytls://"):
		return parseAnyTLS(link, s)
	case strings.HasPrefix(link, "hysteria://"):
		// Below hysteria2 on purpose: the two are one digit apart and nothing else, and
		// a reader who arrives here looking for one of them should find the pair.
		return parseHysteria1(link, s)
	case strings.HasPrefix(link, "snell://"):
		return parseSnell(link, s)
	case strings.HasPrefix(link, "socks4://"),
		strings.HasPrefix(link, "socks5://"),
		strings.HasPrefix(link, "socks://"),
		strings.HasPrefix(link, "http://"),
		strings.HasPrefix(link, "https://"):
		return parseSocksHTTP(link)
	}
	return "", nil, false
}

// --- v2ray scheme parsers ---

// nekorayLinkTypes are the types this parser will hand back, and they are the
// ones New builds an outbound for. The link names its own type and carries its
// own bean, so without a list a subscription could put any protocol name into
// the list — or worse, `custom`: builder.BuildConfig returns the bean's `cs` as
// the whole sing-box configuration when core=internal-full, so one share link
// inside a subscription replaced the running config (outbounds, DNS, routing)
// with whatever the subscription's author wrote, and the window showed an
// ordinary node. `chain` is out for the same kind of reason: it names other
// entities and is a thing the reader builds in the window, not an import.
var nekorayLinkTypes = map[string]bool{
	"socks": true, "http": true, "shadowsocks": true, "vmess": true,
	"trojan": true, "vless": true, "anytls": true, "hysteria2": true,
	"hysteria": true, "snell": true, "tuic": true, "naive": true,
}

func parseNekorayLink(link string) (string, bean.Bean, bool) {
	rest := link[len("nekoray://"):]
	hash := strings.Index(rest, "#")
	if hash < 0 {
		return "", nil, false
	}
	typ := rest[:hash]
	if !nekorayLinkTypes[typ] {
		return "", nil, false
	}
	payload := rest[hash+1:]
	b, ok := util.DecodeB64(payload)
	if !ok {
		return "", nil, false
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil {
		return "", nil, false
	}
	return typ, bean.Bean(m), true
}

func parseSocksHTTP(link string) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "", nil, false
	}
	kind := 5
	typ := "socks"
	if strings.HasPrefix(link, "socks4") {
		kind = 4
	}
	if strings.HasPrefix(link, "http") {
		kind = -80
		typ = "http"
	}
	b := NewSocksHTTP(kind)
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())

	def := 1080
	switch {
	case strings.HasPrefix(link, "https"):
		def = 443
	case kind == -80:
		// http and https used to share the 443 default, so a plain
		// `http://user:pass@host` link was imported as an https node on 443 — one
		// that cannot answer. A scheme that says http means port 80.
		def = 80
	}
	// Through linkPort, like every other parser: a port a socket cannot hold
	// (99999) used to be taken as given, written as server_port, and refused by
	// the core — which took the whole group down with it, because this node was
	// in the selector with the rest.
	port, ok := linkPort(u, def)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)

	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		// v2rayN fmt: base64(user:pass) in username
		if password == "" && username != "" {
			if n, ok := util.DecodeB64(username); ok {
				username = before(string(n), ":")
				password = after(string(n), ":")
			}
		}
		b.Set("username", username)
		b.Set("password", password)
	}

	q := u.Query()
	st := b.Peek("stream")
	st.Set("sec", q.Get("security"))
	if sni := q.Get("sni"); sni != "" {
		st.Set("sni", sni)
	}
	if strings.HasPrefix(link, "https") {
		st.Set("sec", "tls")
	}
	if b.Str("addr") == "" {
		return "", nil, false
	}
	return typ, b, true
}

func parseTrojanVLESS(link string, s *setting.Setting, typ string) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "", nil, false
	}
	b := NewTrojanVLESS()
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)
	password := ""
	if u.User != nil {
		// The userinfo carries the credential, and it may carry it in either shape:
		// a password alone ("trojan://secret@host"), or a name and a password
		// ("trojan://user:secret@host"). The password is what the node dials with,
		// so it is the password that is taken when there is one — the name beside it
		// is a label the panel put there, not the secret. The same rule the two
		// sibling parsers in this file follow (see parseAnyTLS, parseSnell).
		if pw, ok := u.User.Password(); ok && pw != "" {
			password = pw
		} else {
			password = u.User.Username()
		}
	}
	b.Set("pass", password)

	q := u.Query()
	netw := q.Get("type")
	if netw == "" {
		netw = "tcp"
	}
	if netw == "h2" {
		netw = "http"
	}
	st := b.Peek("stream")
	st.Set("net", netw)

	sec := q.Get("security")
	if typ == "trojan" {
		if sec == "" {
			sec = "tls"
		}
	} else if sec == "" {
		sec = ""
	}
	sec = strings.ReplaceAll(sec, "reality", "tls")
	sec = strings.ReplaceAll(sec, "none", "")
	st.Set("sec", sec)

	if sni := q.Get("sni"); sni != "" {
		st.Set("sni", sni)
	}
	if peer := q.Get("peer"); peer != "" {
		st.Set("sni", peer)
	}
	if alpn := q.Get("alpn"); alpn != "" {
		st.Set("alpn", alpn)
	}
	if _, ok := q["allowInsecure"]; ok {
		st.Set("insecure", true)
	}
	st.Set("pbk", q.Get("pbk"))
	st.Set("sid", q.Get("sid"))
	st.Set("spx", q.Get("spx"))
	fp := q.Get("fp")
	if fp == "" && s != nil {
		fp = s.UtlsFingerprint
	}
	st.Set("utls", fp)

	switch netw {
	case "ws":
		st.Set("path", q.Get("path"))
		st.Set("host", q.Get("host"))
	case "http":
		st.Set("path", q.Get("path"))
		st.Set("host", strings.ReplaceAll(q.Get("host"), "|", ","))
	case "httpupgrade":
		st.Set("path", q.Get("path"))
		st.Set("host", q.Get("host"))
	case "grpc":
		st.Set("path", q.Get("serviceName"))
	case "tcp":
		if q.Get("headerType") == "http" {
			st.Set("h_type", "http")
			st.Set("host", q.Get("host"))
			st.Set("path", q.Get("path"))
		}
	}

	if typ == "vless" {
		b.Set("flow", q.Get("flow"))
	}

	if password == "" || b.Str("addr") == "" {
		return "", nil, false
	}
	return typ, b, true
}

// parseAnyTLS parses an anytls share link ("anytls://password@host:port?...").
// The protocol is TLS-only and carries a single password, so the bean is the
// trojan one — panels write the password in the userinfo field, sometimes with
// an empty user name in front of a colon, so both positions are accepted.
func parseAnyTLS(link string, s *setting.Setting) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return "", nil, false
	}
	b := NewTrojanVLESS()
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)

	password := ""
	if u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			password = pw
		} else {
			password = u.User.Username()
		}
	}
	b.Set("pass", password)

	st := b.Peek("stream")
	st.Set("net", "tcp")
	st.Set("sec", "tls")

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni != "" {
		st.Set("sni", sni)
	}
	if alpn := q.Get("alpn"); alpn != "" {
		st.Set("alpn", alpn)
	}
	if _, ok := q["allowInsecure"]; ok {
		st.Set("insecure", true)
	}
	if _, ok := q["insecure"]; ok {
		st.Set("insecure", true)
	}
	fp := q.Get("fp")
	if fp == "" && s != nil {
		fp = s.UtlsFingerprint
	}
	st.Set("utls", fp)

	if password == "" {
		return "", nil, false
	}
	return "anytls", b, true
}

// parseHysteria1 parses a hysteria v1 share link. Unlike its successor the scheme
// carries no userinfo: the auth string, the speed limits and the obfuscation all arrive
// as query parameters, which is the shape the protocol's own documentation uses.
func parseHysteria1(link string, s *setting.Setting) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return "", nil, false
	}
	q := u.Query()
	b := NewHysteria1()
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)
	auth := q.Get("auth")
	if auth == "" {
		auth = q.Get("auth_str")
	}
	b.Set("auth", auth)
	if v, err := strconv.Atoi(q.Get("upmbps")); err == nil {
		b.Set("uploadMbps", v)
	}
	if v, err := strconv.Atoi(q.Get("downmbps")); err == nil {
		b.Set("downloadMbps", v)
	}
	b.Set("obfs", q.Get("obfs"))
	b.Set("protocol", q.Get("protocol"))
	hop := q.Get("mport")
	if hop == "" {
		hop = q.Get("ports")
	}
	b.Set("hopPort", hop)
	// "peer" is what the protocol calls the name it checks the certificate against;
	// "sni" is what everyone else calls it.
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	b.Set("sni", sni)
	b.Set("alpn", q.Get("alpn"))
	b.Set("allowInsecure", q.Get("insecure") == "1" || q.Get("insecure") == "true")
	return "hysteria", b, true
}

// parseSnell parses a snell share link. The pre-shared key rides in the userinfo — the
// shape the clash clients write — and the version and the obfuscation follow as query
// parameters.
func parseSnell(link string, s *setting.Setting) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return "", nil, false
	}
	q := u.Query()
	b := NewSnell()
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)
	psk := ""
	if u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			psk = pw
		} else {
			psk = u.User.Username()
		}
	}
	if psk == "" {
		psk = q.Get("psk")
	}
	b.Set("psk", psk)
	if v, err := strconv.Atoi(q.Get("version")); err == nil && v > 0 {
		b.Set("version", v)
	}
	b.Set("obfsMode", q.Get("obfs"))
	b.Set("obfsHost", q.Get("obfs-host"))
	// A snell node without its key is not a node: nothing about the link could be used.
	if psk == "" {
		return "", nil, false
	}
	return "snell", b, true
}

func parseShadowSocks(link string) (string, bean.Bean, bool) {
	b := NewShadowSocks()
	if strings.Contains(before(link, "#"), "@") {
		// standard SS URL
		u, err := url.Parse(link)
		if err != nil {
			return "", nil, false
		}
		b.Set("name", u.Fragment)
		b.Set("addr", u.Hostname())
		port, _ := strconv.Atoi(u.Port())
		b.Set("port", port)

		password, hasPass := "", false
		if u.User != nil {
			password, hasPass = u.User.Password()
		}
		if !hasPass || password == "" {
			// traditional format: base64(method:password) as username
			raw := ""
			if u.User != nil {
				raw = u.User.Username()
			}
			decoded, ok := util.DecodeB64(raw)
			if !ok || len(decoded) == 0 {
				return "", nil, false
			}
			text := string(decoded)
			if !strings.Contains(text, ":") {
				// before/after both answer the whole string when the separator is not
				// there, so a payload carrying no colon imported as a node whose method
				// and password were the same text — one that can never connect, with
				// nothing about the import to say why.
				return "", nil, false
			}
			b.Set("method", before(text, ":"))
			b.Set("pass", after(text, ":"))
		} else {
			// 2022 format
			b.Set("method", u.User.Username())
			b.Set("pass", password)
		}
		if plugin := u.Query().Get("plugin"); plugin != "" {
			b.Set("plugin", strings.ReplaceAll(plugin, "simple-obfs;", "obfs-local;"))
		}
	} else {
		// v2rayN format
		body := link[len("ss://"):]
		frag := ""
		hasRemarks := false
		if i := strings.Index(body, "#"); i >= 0 {
			frag = body[i+1:]
			body = body[:i]
			hasRemarks = true
		}
		decoded, ok := util.DecodeB64(body)
		if !ok || len(decoded) == 0 {
			return "", nil, false
		}
		inner := string(decoded)
		if hasRemarks {
			inner += "#" + frag
		}
		u, err := url.Parse("https://" + inner)
		if err != nil {
			return "", nil, false
		}
		if hasRemarks {
			b.Set("name", u.Fragment)
		}
		b.Set("addr", u.Hostname())
		port, _ := strconv.Atoi(u.Port())
		b.Set("port", port)
		b.Set("method", u.User.Username())
		pw, _ := u.User.Password()
		b.Set("pass", pw)
	}

	// The port is part of the refusal as well: an ss link with no port used to come
	// through on port 0 (the Atoi of an empty string), and that is a node which can
	// never connect — imported silently, listed as healthy, and only ever explained
	// by "it does not work". The other protocols here default to 443; a link that
	// names no port is one to skip.
	if b.Str("addr") == "" || b.Str("method") == "" || b.Str("pass") == "" {
		return "", nil, false
	}
	if p := b.Int("port"); p <= 0 || p > 65535 {
		return "", nil, false
	}
	return "shadowsocks", b, true
}

func parseVMess(link string, s *setting.Setting) (string, bean.Bean, bool) {
	b := NewVMess()
	// v2rayN format
	if decoded, ok := util.DecodeB64(link[len("vmess://"):]); ok {
		var obj map[string]interface{}
		if json.Unmarshal(decoded, &obj) == nil && len(obj) > 0 {
			b.Set("id", toStr(obj["id"]))
			b.Set("addr", toStr(obj["add"]))
			port, ok := validPort(toInt(obj["port"]), 443)
			if !ok {
				return "", nil, false
			}
			b.Set("port", port)
			b.Set("name", toStr(obj["ps"]))
			b.Set("aid", toInt(obj["aid"]))
			st := b.Peek("stream")
			st.Set("host", toStr(obj["host"]))
			st.Set("path", toStr(obj["path"]))
			st.Set("sni", toStr(obj["sni"]))
			st.Set("h_type", toStr(obj["type"]))
			if netw := toStr(obj["net"]); netw != "" {
				if netw == "h2" {
					netw = "http"
				}
				st.Set("net", netw)
			}
			if scy := toStr(obj["scy"]); scy != "" {
				b.Set("sec", scy)
			}
			st.Set("sec", toStr(obj["tls"]))
			if b.Str("id") == "" || b.Str("addr") == "" {
				return "", nil, false
			}
			return "vmess", b, true
		}
	}

	// Xray URL format
	u, err := url.Parse(link)
	if err != nil {
		return "", nil, false
	}
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)
	if u.User != nil {
		b.Set("id", u.User.Username())
	}
	b.Set("aid", 0)

	q := u.Query()
	sec := q.Get("encryption")
	if sec == "" {
		sec = "auto"
	}
	b.Set("sec", sec)

	netw := q.Get("type")
	if netw == "" {
		netw = "tcp"
	}
	if netw == "h2" {
		netw = "http"
	}
	st := b.Peek("stream")
	st.Set("net", netw)
	security := q.Get("security")
	if security == "" {
		security = "tls"
	}
	st.Set("sec", strings.ReplaceAll(security, "reality", "tls"))
	if sni := q.Get("sni"); sni != "" {
		st.Set("sni", sni)
	}
	if peer := q.Get("peer"); peer != "" {
		st.Set("sni", peer)
	}
	if _, ok := q["allowInsecure"]; ok {
		st.Set("insecure", true)
	}
	st.Set("pbk", q.Get("pbk"))
	st.Set("sid", q.Get("sid"))
	st.Set("spx", q.Get("spx"))
	fp := q.Get("fp")
	if fp == "" && s != nil {
		fp = s.UtlsFingerprint
	}
	st.Set("utls", fp)

	switch netw {
	case "ws":
		st.Set("path", q.Get("path"))
		st.Set("host", q.Get("host"))
	case "http":
		st.Set("path", q.Get("path"))
		st.Set("host", strings.ReplaceAll(q.Get("host"), "|", ","))
	case "httpupgrade":
		st.Set("path", q.Get("path"))
		st.Set("host", q.Get("host"))
	case "grpc":
		st.Set("path", q.Get("serviceName"))
	case "tcp":
		if q.Get("headerType") == "http" {
			st.Set("h_type", "http")
			st.Set("path", q.Get("path"))
			st.Set("host", q.Get("host"))
		}
	}

	if b.Str("id") == "" || b.Str("addr") == "" {
		return "", nil, false
	}
	return "vmess", b, true
}

func parseNaive(link string) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "", nil, false
	}
	b := NewNaive()
	protocol := strings.Replace(u.Scheme, "naive+", "", 1)
	if protocol != "https" && protocol != "quic" {
		return "", nil, false
	}
	b.Set("protocol", protocol)
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	// 443 when the link names none: naive is https/quic, and a port of zero is
	// a node nothing can dial (it used to be written as given, so a link
	// without a port imported as "complete" and then failed to start).
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)
	if u.User != nil {
		b.Set("username", u.User.Username())
		pw, _ := u.User.Password()
		b.Set("password", pw)
	}
	if b.Str("username") == "" || b.Str("password") == "" || b.Str("addr") == "" {
		return "", nil, false
	}
	return "naive", b, true
}

func parseQUIC(link string, typ string) (string, bean.Bean, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "", nil, false
	}
	if u.Hostname() == "" || u.Port() == "" {
		return "", nil, false
	}
	q := u.Query()
	if typ == "tuic" {
		b := NewTUIC()
		b.Set("name", u.Fragment)
		b.Set("addr", u.Hostname())
		port, ok := linkPort(u, 443)
		if !ok {
			return "", nil, false
		}
		b.Set("port", port)
		if u.User != nil {
			b.Set("uuid", u.User.Username())
			pw, _ := u.User.Password()
			b.Set("password", pw)
		}
		b.Set("congestionControl", q.Get("congestion_control"))
		b.Set("alpn", q.Get("alpn"))
		b.Set("sni", q.Get("sni"))
		b.Set("udpRelayMode", q.Get("udp_relay_mode"))
		b.Set("allowInsecure", q.Get("allow_insecure") == "1")
		b.Set("disableSni", q.Get("disable_sni") == "1")
		// The keepalive period. A link may carry it in seconds ("heartbeat=30") or
		// as a duration ("heartbeat=30s"); the bean holds the duration, which is
		// the shape the app itself writes.
		if hb := strings.TrimSpace(q.Get("heartbeat")); hb != "" {
			b.Set("heartbeat", heartbeatDuration(hb))
		}
		return "tuic", b, true
	}

	b := NewHysteria2()
	b.Set("name", u.Fragment)
	b.Set("addr", u.Hostname())
	port, ok := linkPort(u, 443)
	if !ok {
		return "", nil, false
	}
	b.Set("port", port)
	b.Set("hopPort", q.Get("mport"))
	b.Set("obfsPassword", q.Get("obfs-password"))
	b.Set("allowInsecure", q.Get("insecure") == "1" || q.Get("insecure") == "true")
	if u.User != nil {
		pw, hasPass := u.User.Password()
		if !hasPass || pw == "" {
			b.Set("password", u.User.Username())
		} else {
			b.Set("password", u.User.Username()+":"+pw)
		}
	}
	b.Set("sni", q.Get("sni"))
	// The ALPN and the two speed limits. The link carries them and the parser used
	// to drop them, so a hysteria2 node exported from this app came back without
	// the very settings that make it work over HTTP/3.
	b.Set("alpn", q.Get("alpn"))
	if v, err := strconv.Atoi(q.Get("upmbps")); err == nil {
		b.Set("uploadMbps", v)
	}
	if v, err := strconv.Atoi(q.Get("downmbps")); err == nil {
		b.Set("downloadMbps", v)
	}
	return "hysteria2", b, true
}

// --- link generation ---

// ToShareLink renders a bean as a standard share link.
func ToShareLink(typ string, b bean.Bean, s *setting.Setting) string {
	switch typ {
	case "socks", "http":
		return socksHTTPToLink(b)
	case "shadowsocks":
		return shadowSocksToLink(b)
	case "vmess":
		return vmessToLink(b, s)
	case "trojan", "vless", "anytls":
		// the same shape: scheme://password@host:port?security=...#name
		return trojanVLESSToLink(b, typ)
	case "naive":
		return naiveToLink(b)
	case "tuic", "hysteria2":
		return quicToLink(b, typ)
	case "hysteria":
		return hysteria1ToLink(b)
	case "snell":
		return snellToLink(b)
	}
	return ""
}

// ToNekorayShareLink renders a bean as a nekoray:// link.
func ToNekorayShareLink(typ string, b bean.Bean) string {
	data, _ := json.Marshal(map[string]interface{}(b))
	return "nekoray://" + typ + "#" + base64.RawURLEncoding.EncodeToString(data)
}

func socksHTTPToLink(b bean.Bean) string {
	kind := b.Int("v")
	u := &url.URL{}
	switch {
	case kind == -80:
		if b.Peek("stream").Str("sec") == "tls" {
			u.Scheme = "https"
		} else {
			u.Scheme = "http"
		}
	case kind == 4:
		u.Scheme = "socks4"
	default:
		u.Scheme = fmt.Sprintf("socks%d", kind)
	}
	u.Fragment = b.Str("name")
	username, password := b.Str("username"), b.Str("password")
	if username != "" {
		if password != "" {
			u.User = url.UserPassword(username, password)
		} else {
			u.User = url.User(username)
		}
	}
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	return u.String()
}

func shadowSocksToLink(b bean.Bean) string {
	method, password := b.Str("method"), b.Str("pass")
	hp := hostPort(b.Str("addr"), b.Int("port"))
	var userinfo string
	if strings.HasPrefix(method, "2022-") {
		userinfo = method + ":" + url.PathEscape(password)
	} else {
		userinfo = base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
	}
	link := "ss://" + userinfo + "@" + hp
	if plugin := b.Str("plugin"); plugin != "" {
		link += "?plugin=" + url.QueryEscape(plugin)
	}
	if name := b.Str("name"); name != "" {
		link += "#" + url.PathEscape(name)
	}
	return link
}

type vmessV2N struct {
	V    string `json:"v"`
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port string `json:"port"`
	ID   string `json:"id"`
	Aid  string `json:"aid"`
	Net  string `json:"net"`
	Host string `json:"host"`
	Path string `json:"path"`
	Type string `json:"type"`
	Scy  string `json:"scy"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
}

func vmessToLink(b bean.Bean, s *setting.Setting) string {
	st := b.Peek("stream")
	// The v2rayN JSON object below has no field for the REALITY handshake
	// (pbk/sid/spx/fp/alpn), so exporting one that way produced a plain-TLS
	// node with the same address: it imports fine, looks identical, and cannot
	// connect — the server is waiting for a REALITY client. The URL form
	// carries those fields, and this package's own parser reads it back (see
	// parseVmess's Xray-URL branch), so a REALITY node takes that shape.
	if strings.TrimSpace(st.Str("pbk")) != "" && st.Str("sec") == "tls" {
		return vmessRealityToLink(b, st)
	}
	tls := ""
	if st.Str("sec") == "tls" {
		tls = "tls"
	}
	obj := vmessV2N{
		V:    "2",
		Ps:   b.Str("name"),
		Add:  b.Str("addr"),
		Port: strconv.Itoa(b.Int("port")),
		ID:   b.Str("id"),
		Aid:  strconv.Itoa(b.Int("aid")),
		Net:  st.Str("net"),
		Host: st.Str("host"),
		Path: st.Str("path"),
		Type: st.Str("h_type"),
		Scy:  b.Str("sec"),
		TLS:  tls,
		SNI:  st.Str("sni"),
	}
	data, _ := json.Marshal(obj)
	return "vmess://" + base64.StdEncoding.EncodeToString(data)
}

// vmessRealityToLink is the URL-form export for a vmess node whose handshake
// is REALITY. The shape mirrors trojanVLESSToLink, minus flow (vmess has none)
// and with `encryption` carrying the cipher the JSON form calls scy — which is
// what parseVmess reads it as.
func vmessRealityToLink(b bean.Bean, st bean.Bean) string {
	u := &url.URL{Scheme: "vmess"}
	u.User = url.User(b.Str("id"))
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	u.Fragment = b.Str("name")

	q := url.Values{}
	q.Set("security", "reality")
	cipher := b.Str("sec")
	if cipher == "" {
		cipher = "auto"
	}
	q.Set("encryption", cipher)
	if sni := st.Str("sni"); sni != "" {
		q.Set("sni", sni)
	}
	if alpn := st.Str("alpn"); alpn != "" {
		q.Set("alpn", alpn)
	}
	if st.Bool("insecure") {
		q.Set("allowInsecure", "1")
	}
	if fp := st.Str("utls"); fp != "" {
		q.Set("fp", fp)
	}
	q.Set("pbk", st.Str("pbk"))
	if sid := st.Str("sid"); sid != "" {
		q.Set("sid", sid)
	}
	if spx := st.Str("spx"); spx != "" {
		q.Set("spx", spx)
	}
	network := st.Str("net")
	if network == "" {
		network = "tcp"
	}
	if network == "http" {
		network = "h2" // the spelling this parser maps back onto "http"
	}
	q.Set("type", network)
	switch network {
	case "ws", "h2", "httpupgrade":
		if p := st.Str("path"); p != "" {
			q.Set("path", p)
		}
		if h := st.Str("host"); h != "" {
			q.Set("host", h)
		}
	case "grpc":
		if p := st.Str("path"); p != "" {
			q.Set("serviceName", p)
		}
	case "tcp":
		if st.Str("h_type") == "http" {
			if p := st.Str("path"); p != "" {
				q.Set("path", p)
			}
			q.Set("headerType", "http")
			q.Set("host", st.Str("host"))
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func trojanVLESSToLink(b bean.Bean, typ string) string {
	u := &url.URL{Scheme: typ}
	u.User = url.User(b.Str("pass"))
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	u.Fragment = b.Str("name")

	st := b.Peek("stream")
	security := st.Str("sec")
	if security == "tls" && strings.TrimSpace(st.Str("pbk")) != "" {
		security = "reality"
	}
	q := url.Values{}
	q.Set("security", security)
	if sni := st.Str("sni"); sni != "" {
		q.Set("sni", sni)
	}
	if alpn := st.Str("alpn"); alpn != "" {
		q.Set("alpn", alpn)
	}
	if st.Bool("insecure") {
		q.Set("allowInsecure", "1")
	}
	if fp := st.Str("utls"); fp != "" {
		q.Set("fp", fp)
	}
	if security == "reality" {
		q.Set("pbk", st.Str("pbk"))
		if sid := st.Str("sid"); sid != "" {
			q.Set("sid", sid)
		}
		if spx := st.Str("spx"); spx != "" {
			q.Set("spx", spx)
		}
	}
	network := st.Str("net")
	if network == "" {
		network = "tcp"
	}
	q.Set("type", network)
	switch network {
	case "ws", "http", "httpupgrade":
		if p := st.Str("path"); p != "" {
			q.Set("path", p)
		}
		if h := st.Str("host"); h != "" {
			q.Set("host", h)
		}
	case "grpc":
		if p := st.Str("path"); p != "" {
			q.Set("serviceName", p)
		}
	case "tcp":
		if st.Str("h_type") == "http" {
			if p := st.Str("path"); p != "" {
				q.Set("path", p)
			}
			q.Set("headerType", "http")
			q.Set("host", st.Str("host"))
		}
	}
	if typ == "vless" {
		if flow := b.Str("flow"); flow != "" {
			q.Set("flow", flow)
		}
		q.Set("encryption", "none")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func naiveToLink(b bean.Bean) string {
	u := &url.URL{Scheme: "naive+" + b.Str("protocol")}
	u.User = url.UserPassword(b.Str("username"), b.Str("password"))
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	u.Fragment = b.Str("name")
	return u.String()
}

// hysteria1ToLink renders a hysteria v1 bean as its share link. No userinfo: the auth
// string and the speed limits are query parameters, the shape the protocol documents.
func hysteria1ToLink(b bean.Bean) string {
	u := &url.URL{Scheme: "hysteria"}
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	q := url.Values{}
	if v := b.Str("auth"); v != "" {
		q.Set("auth", v)
	}
	if v := b.Int("uploadMbps"); v > 0 {
		q.Set("upmbps", strconv.Itoa(v))
	}
	if v := b.Int("downloadMbps"); v > 0 {
		q.Set("downmbps", strconv.Itoa(v))
	}
	if v := b.Str("obfs"); v != "" {
		q.Set("obfs", v)
	}
	if v := b.Str("protocol"); v != "" {
		q.Set("protocol", v)
	}
	if v := b.Str("hopPort"); v != "" {
		q.Set("mport", v)
	}
	if v := b.Str("sni"); v != "" {
		q.Set("sni", v)
	}
	if v := b.Str("alpn"); v != "" {
		q.Set("alpn", v)
	}
	if b.Bool("allowInsecure") {
		q.Set("insecure", "1")
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	u.Fragment = b.Str("name")
	return u.String()
}

// snellToLink renders a snell bean as its share link: the key in the userinfo, the
// version and the obfuscation as parameters.
func snellToLink(b bean.Bean) string {
	u := &url.URL{Scheme: "snell"}
	u.User = url.User(b.Str("psk"))
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	q := url.Values{}
	if v := b.Int("version"); v > 0 {
		q.Set("version", strconv.Itoa(v))
	}
	if v := b.Str("obfsMode"); v != "" {
		q.Set("obfs", v)
	}
	if v := b.Str("obfsHost"); v != "" {
		q.Set("obfs-host", v)
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	u.Fragment = b.Str("name")
	return u.String()
}

func quicToLink(b bean.Bean, typ string) string {
	q := url.Values{}
	if typ == "tuic" {
		u := &url.URL{Scheme: "tuic"}
		u.User = url.UserPassword(b.Str("uuid"), b.Str("password"))
		u.Host = hostPort(b.Str("addr"), b.Int("port"))
		if v := b.Str("congestionControl"); v != "" {
			q.Set("congestion_control", v)
		}
		if v := b.Str("alpn"); v != "" {
			q.Set("alpn", v)
		}
		if v := b.Str("sni"); v != "" {
			q.Set("sni", v)
		}
		if v := b.Str("udpRelayMode"); v != "" {
			q.Set("udp_relay_mode", v)
		}
		if v := b.Str("heartbeat"); v != "" {
			q.Set("heartbeat", v)
		}
		if b.Bool("allowInsecure") {
			q.Set("allow_insecure", "1")
		}
		if b.Bool("disableSni") {
			q.Set("disable_sni", "1")
		}
		if len(q) > 0 {
			u.RawQuery = q.Encode()
		}
		u.Fragment = b.Str("name")
		return u.String()
	}

	// hysteria2
	u := &url.URL{Scheme: "hy2"}
	u.Host = hostPort(b.Str("addr"), b.Int("port"))
	password := b.Str("password")
	if strings.Contains(password, ":") {
		u.User = url.UserPassword(before(password, ":"), after(password, ":"))
	} else {
		u.User = url.User(password)
	}
	if v := b.Str("obfsPassword"); v != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", v)
	}
	if v := strings.TrimSpace(b.Str("hopPort")); v != "" {
		q.Set("mport", v)
	}
	if v := b.Int("uploadMbps"); v > 0 {
		q.Set("upmbps", strconv.Itoa(v))
	}
	if v := b.Int("downloadMbps"); v > 0 {
		q.Set("downmbps", strconv.Itoa(v))
	}
	if v := b.Str("alpn"); v != "" {
		q.Set("alpn", v)
	}
	if b.Bool("allowInsecure") {
		q.Set("insecure", "1")
	}
	if v := b.Str("sni"); v != "" {
		q.Set("sni", v)
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	u.Fragment = b.Str("name")
	return u.String()
}

// --- helpers ---

// linkPort reads the port out of a parsed URL.
//
// An absent port and a port of zero both mean "not given" — the reading the
// hysteria1 and snell parsers have always given them — and fall back to the
// protocol's default. A port outside the range a socket can hold is a
// different matter: the link names a port nothing can listen on, so the link
// is refused rather than quietly pointed somewhere else.
func linkPort(u *url.URL, def int) (int, bool) {
	raw := u.Port()
	if raw == "" {
		return def, true
	}
	p, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return validPort(p, def)
}

// validPort puts a port a link named into range. A port of zero means the link
// named none, and takes the default; anything outside the range a socket can
// hold names a port nothing can listen on.
func validPort(p, def int) (int, bool) {
	if p == 0 {
		return def, true
	}
	if p < 0 || p > 65535 {
		return 0, false
	}
	return p, true
}

// heartbeatDuration reads a keepalive period out of a share link. The link may
// carry it in seconds ("heartbeat=30") or as a duration ("heartbeat=30s"); the
// bean holds the duration, which is the shape the app itself writes.
func heartbeatDuration(v string) string {
	if n, err := strconv.Atoi(v); err == nil {
		return strconv.Itoa(n) + "s"
	}
	return v
}

func hostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return "[" + strings.Trim(host, "[]") + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

func toStr(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	}
	return 0
}

func before(s, sub string) string {
	if i := strings.Index(s, sub); i >= 0 {
		return s[:i]
	}
	return s
}

func after(s, sub string) string {
	if i := strings.Index(s, sub); i >= 0 {
		return s[i+len(sub):]
	}
	return s
}
