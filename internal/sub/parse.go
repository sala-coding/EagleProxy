package sub

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"eagle/internal/db"
	"eagle/internal/format"
	"eagle/internal/setting"
	"eagle/internal/util"
)

// readableBody is name.go's readable for content that may span lines: the
// line breaks a subscription list carries are text, every other control
// character and any invalid UTF-8 rune is not.
func readableBody(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, r := range string(b) {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r') {
			return false
		}
	}
	return true
}

// rawUpdater parses subscription content (links / Clash YAML / base64),
// mirroring nekoray's RawUpdater. The parsed entities are collected in
// `updated` and left to the caller, which lands the whole batch on disk in
// one pass (Manager.AddProfiles).
type rawUpdater struct {
	s        *setting.Setting
	gidAddTo int
	updated  []*db.ProxyEntity
	// What the subscription carried and this build cannot speak, so that the update can
	// say so. A node whose protocol is missing used to leave no trace at all: the count
	// came out short and nothing anywhere said why — which is how a node can be silently
	// absent for as long as the subscription is used.
	skipped []string
}

// skip records one node the subscription carried and this build could not take.
func (r *rawUpdater) skip(name, typ string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(未命名)"
	}
	if typ == "" {
		typ = "?"
	}
	r.skipped = append(r.skipped, name+" ("+typ+")")
}

func (r *rawUpdater) update(str string) { r.updateDepth(str, 0) }

func (r *rawUpdater) updateDepth(str string, depth int) {
	if depth > 8 {
		return
	}
	str = strings.TrimSpace(str)
	if str == "" {
		return
	}

	// base64 encoded subscription
	if decoded, ok := util.DecodeB64(str); ok && len(decoded) > 0 && string(decoded) != str {
		// Decode success is not decoded TEXT: any 4k-multiple of base64
		// alphabet characters decodes cleanly, so one ordinary short word in a
		// list ("internet", "direct12") came out as binary garbage, recursion
		// failed on it, and the report told the reader that N of their nodes
		// were unsupported — with the garbage itself printed as the node's
		// name. A decode that is not text gets neither the recursion nor a
		// skip entry: there was no node there to report (the same verdict
		// name.go's readable gives a name; the body variant below allows the
		// line breaks a subscription list carries).
		if !readableBody(decoded) {
			return
		}
		r.updateDepth(string(decoded), depth+1)
		return
	}

	// Clash
	if strings.Contains(str, "proxies:") {
		r.updateClash(str)
		return
	}

	// multi line
	if strings.Contains(str, "\n") {
		for _, line := range strings.Split(str, "\n") {
			r.updateDepth(strings.TrimSpace(line), depth)
		}
		return
	}

	r.updateLink(str)
}

// linkScheme is the part before "://" — what a link says it is.
func linkScheme(link string) string {
	if i := strings.Index(link, "://"); i > 0 {
		return link[:i]
	}
	return "?"
}

// linkLabel is what to call a link in a report: the name after the fragment when it
// carries one, and a trimmed head of the link itself when it does not.
func linkLabel(link string) string {
	label := link
	if i := strings.Index(link, "#"); i >= 0 && i+1 < len(link) {
		label = link[i+1:]
	}
	if label = strings.TrimSpace(label); len(label) > 40 {
		// Cut on rune boundaries: slicing at byte 40 splits a multi-byte character in
		// half for any subscription with a Chinese or emoji name, and that broken
		// half-character then travels into the report, the log file and the JSON the
		// window reads. (name.go's tidyName does this the rune-aware way.)
		if r := []rune(label); len(r) > 40 {
			label = string(r[:40]) + "…"
		}
	}
	if label == "" {
		label = linkScheme(link)
	}
	return label
}

// isCommentLine reports whether a line of a link list is a comment rather than
// a link. The markers are the ones panels use, and no supported share-link
// scheme begins with any of them.
func isCommentLine(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "//")
}

func (r *rawUpdater) updateLink(str string) {
	// A comment line is not a node. Panels put them at the top of a link list
	// ("# my subscription") and they are not links by any scheme — reporting one
	// as an unsupported node tells the reader that a node of theirs was skipped,
	// when nothing was skipped at all.
	if isCommentLine(str) {
		return
	}
	typ, b, ok := format.ParseLink(str, r.s)
	if !ok {
		// A link this build cannot turn into a profile: a scheme it does not know, or
		// one that arrived without the parts a profile needs. Either way it is a node
		// the reader is expecting to see, so it gets named in the report.
		r.skip(linkLabel(str), linkScheme(str))
		return
	}
	needFix := true
	switch {
	case strings.HasPrefix(str, "nekoray://"),
		strings.HasPrefix(str, "naive+"),
		strings.HasPrefix(str, "hysteria2://"),
		strings.HasPrefix(str, "hy2://"),
		strings.HasPrefix(str, "tuic://"):
		needFix = false
	}

	ent := &db.ProxyEntity{Type: typ, ID: -1, GID: r.gidAddTo, Bean: b}
	ent.Ensure()
	if needFix {
		fixEnt(ent)
	}
	r.updated = append(r.updated, ent)
}

// fixEnt mirrors RawUpdater_FixEnt.
func fixEnt(ent *db.ProxyEntity) {
	if ent == nil {
		return
	}
	stream := ent.Bean.Peek("stream")
	if stream == nil {
		return
	}
	switch stream.Str("sec") {
	case "none", "0", "false":
		stream.Set("sec", "")
	case "1", "true":
		stream.Set("sec", "tls")
	}
	if stream.Str("sec") == "tls" && util.IsIPAddress(ent.ServerAddress()) &&
		stream.Str("host") != "" && stream.Str("sni") == "" {
		stream.Set("sni", stream.Str("host"))
	}
}

// updateClash parses a Clash configuration's "proxies" list.
func (r *rawUpdater) updateClash(str string) {
	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(str), &root); err != nil {
		return
	}
	proxies, _ := root["proxies"].([]interface{})
	for _, p := range proxies {
		proxy := asMap(p)
		if proxy == nil {
			continue
		}
		typ := strings.ToLower(yStr(proxy, "type"))
		typeClash := typ
		switch typ {
		case "ss":
			typ = "shadowsocks"
		case "socks5":
			typ = "socks"
		}

		if typeClash == "ssr" {
			// ShadowsocksR is not shadowsocks under another name — its protocol and
			// obfuscation settings live outside the cipher, and sing-box dropped the
			// whole protocol in 1.6. It used to be imported as shadowsocks, quietly, so
			// the node sat in the list looking ordinary and could never connect.
			r.skip(yStr(proxy, "name"), typeClash)
			continue
		}

		ent := &db.ProxyEntity{Type: typ, ID: -1, GID: r.gidAddTo, Bean: format.New(typ)}
		ent.Ensure()
		if ent.Bean.Int("_v") == -114514 {
			// No bean means no implementation on this side, which is the ordinary case
			// for whatever this build does not carry (mieru, juicity, tor, …).
			r.skip(yStr(proxy, "name"), typeClash)
			continue
		}
		needFix := false

		ent.Bean.Set("name", yStr(proxy, "name"))
		ent.Bean.Set("addr", yStr(proxy, "server"))
		ent.Bean.Set("port", yInt(proxy, "port"))

		switch {
		case typeClash == "ss" || typeClash == "ssr":
			st := ent.Bean.Peek("stream")
			ent.Bean.Set("method", strings.ReplaceAll(yStr(proxy, "cipher"), "dummy", "none"))
			ent.Bean.Set("pass", yStr(proxy, "password"))
			if yBool(proxy, "udp-over-tcp") {
				uot := yInt(proxy, "udp-over-tcp-version")
				if uot == 0 {
					uot = 2
				}
				ent.Bean.Set("uot", uot)
			}
			if _, ok := proxy["plugin"]; ok {
				if opts := asMap(proxy["plugin-opts"]); opts != nil {
					plugin := yStr(proxy, "plugin")
					var ssPlugin []string
					switch plugin {
					case "obfs":
						ssPlugin = append(ssPlugin, "obfs-local",
							"obfs="+yStr(opts, "mode"), "obfs-host="+yStr(opts, "host"))
					case "v2ray-plugin":
						mode, host, path := yStr(opts, "mode"), yStr(opts, "host"), yStr(opts, "path")
						ssPlugin = append(ssPlugin, "v2ray-plugin")
						if mode != "" && mode != "websocket" {
							ssPlugin = append(ssPlugin, "mode="+mode)
						}
						if yBool(opts, "tls") {
							ssPlugin = append(ssPlugin, "tls")
						}
						if host != "" {
							ssPlugin = append(ssPlugin, "host="+host)
						}
						if path != "" {
							ssPlugin = append(ssPlugin, "path="+path)
						}
					}
					ent.Bean.Set("plugin", strings.Join(ssPlugin, ";"))
				}
			}
			if yBool(yChild(proxy, "smux"), "enabled") {
				st.Set("mux_s", 1)
			}

		case typ == "socks" || typ == "http":
			st := ent.Bean.Peek("stream")
			ent.Bean.Set("username", yStr(proxy, "username"))
			ent.Bean.Set("password", yStr(proxy, "password"))
			if yBool(proxy, "tls") {
				st.Set("sec", "tls")
			}
			if yBool(proxy, "skip-cert-verify") {
				st.Set("insecure", true)
			}

		case typ == "trojan" || typ == "vless" || typ == "anytls":
			needFix = true
			st := ent.Bean.Peek("stream")
			if typ == "vless" {
				ent.Bean.Set("flow", yStr(proxy, "flow"))
				ent.Bean.Set("pass", yStr(proxy, "uuid"))
				if yBool(proxy, "packet-addr") {
					st.Set("pac_enc", "packetaddr")
				} else if yBool(proxy, "xudp") {
					st.Set("pac_enc", "xudp")
				}
			} else {
				ent.Bean.Set("pass", yStr(proxy, "password"))
			}
			st.Set("sec", "tls")
			netw := yStr(proxy, "network")
			if netw == "" {
				netw = "tcp"
			}
			st.Set("net", netw)
			st.Set("sni", firstOrSecond(yStr(proxy, "sni"), yStr(proxy, "servername")))
			st.Set("alpn", strings.Join(yStrList(proxy, "alpn"), ","))
			st.Set("insecure", yBool(proxy, "skip-cert-verify"))
			fp := yStr(proxy, "client-fingerprint")
			if fp == "" && r.s != nil {
				fp = r.s.UtlsFingerprint
			}
			st.Set("utls", fp)
			if yBool(yChild(proxy, "smux"), "enabled") {
				st.Set("mux_s", 1)
			}
			if ws := yChild(proxy, "ws-opts", "ws-opt"); ws != nil {
				if hdrs := asMap(ws["headers"]); hdrs != nil {
					for k, v := range hdrs {
						if strings.ToLower(k) == "host" {
							st.Set("host", yValStr(v))
						}
					}
				}
				st.Set("path", yStr(ws, "path"))
				st.Set("ed_len", yInt(ws, "max-early-data"))
				st.Set("ed_name", yStr(ws, "early-data-header-name"))
			}
			if grpc := yChild(proxy, "grpc-opts", "grpc-opt"); grpc != nil {
				st.Set("path", yStr(grpc, "grpc-service-name"))
			}
			if reality := yChild(proxy, "reality-opts"); reality != nil {
				st.Set("pbk", yStr(reality, "public-key"))
				st.Set("sid", yStr(reality, "short-id"))
			}

		case typ == "vmess":
			needFix = true
			st := ent.Bean.Peek("stream")
			ent.Bean.Set("id", yStr(proxy, "uuid"))
			ent.Bean.Set("aid", yInt(proxy, "alterId"))
			if c := yStr(proxy, "cipher"); c != "" {
				ent.Bean.Set("sec", c)
			}
			netw := strings.Replace(yStr(proxy, "network"), "h2", "http", 1)
			if netw == "" {
				netw = "tcp"
			}
			st.Set("net", netw)
			st.Set("sni", firstOrSecond(yStr(proxy, "sni"), yStr(proxy, "servername")))
			st.Set("alpn", strings.Join(yStrList(proxy, "alpn"), ","))
			if yBool(proxy, "tls") {
				st.Set("sec", "tls")
			}
			if yBool(proxy, "skip-cert-verify") {
				st.Set("insecure", true)
			}
			fp := yStr(proxy, "client-fingerprint")
			if fp == "" && r.s != nil {
				fp = r.s.UtlsFingerprint
			}
			st.Set("utls", fp)
			if yBool(yChild(proxy, "smux"), "enabled") {
				st.Set("mux_s", 1)
			}
			if yBool(proxy, "xudp") {
				st.Set("pac_enc", "xudp")
			}
			if yBool(proxy, "packet-addr") {
				st.Set("pac_enc", "packetaddr")
			}
			if ws := yChild(proxy, "ws-opts", "ws-opt"); ws != nil {
				if hdrs := asMap(ws["headers"]); hdrs != nil {
					for k, v := range hdrs {
						if strings.ToLower(k) == "host" {
							st.Set("host", yValStr(v))
						}
					}
				}
				path := yStr(ws, "path")
				st.Set("ed_len", yInt(ws, "max-early-data"))
				st.Set("ed_name", yStr(ws, "early-data-header-name"))
				if yStr(ws, "early-data-header-name") == "Sec-WebSocket-Protocol" {
					path += "?ed=" + yStr(ws, "max-early-data")
				}
				st.Set("path", path)
			}
			if grpc := yChild(proxy, "grpc-opts", "grpc-opt"); grpc != nil {
				st.Set("path", yStr(grpc, "grpc-service-name"))
			}
			if h2 := yChild(proxy, "h2-opts", "h2-opt"); h2 != nil {
				if hosts := yList(h2, "host"); len(hosts) > 0 {
					st.Set("host", yValStr(hosts[0]))
				}
				st.Set("path", yStr(h2, "path"))
			}
			if tcpHTTP := yChild(proxy, "http-opts", "http-opt"); tcpHTTP != nil {
				st.Set("net", "tcp")
				st.Set("h_type", "http")
				if hdrs := asMap(tcpHTTP["headers"]); hdrs != nil {
					for k, v := range hdrs {
						if strings.ToLower(k) == "host" {
							if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
								st.Set("host", yValStr(arr[0]))
							} else {
								st.Set("host", yValStr(v))
							}
							break
						}
					}
				}
				if paths := yList(tcpHTTP, "path"); len(paths) > 0 {
					st.Set("path", yValStr(paths[0]))
				}
			}

		case typ == "hysteria2":
			ent.Bean.Set("hopPort", yStr(proxy, "ports"))
			ent.Bean.Set("allowInsecure", yBool(proxy, "skip-cert-verify"))
			ent.Bean.Set("caText", yStr(proxy, "ca-str"))
			ent.Bean.Set("sni", yStr(proxy, "sni"))
			ent.Bean.Set("obfsPassword", yStr(proxy, "obfs-password"))
			ent.Bean.Set("password", yStr(proxy, "password"))
			ent.Bean.Set("uploadMbps", firstTokenInt(yStr(proxy, "up")))
			ent.Bean.Set("downloadMbps", firstTokenInt(yStr(proxy, "down")))

		case typ == "snell":
			ent.Bean.Set("psk", yStr(proxy, "psk"))
			if v := yInt(proxy, "version"); v > 0 {
				ent.Bean.Set("version", v)
			}
			if obfs := yChild(proxy, "obfs-opts", "obfs-opt"); obfs != nil {
				ent.Bean.Set("obfsMode", yStr(obfs, "mode"))
				ent.Bean.Set("obfsHost", yStr(obfs, "host"))
			}

		case typ == "hysteria":
			// The v1 spelling of the same idea: one auth string rather than v2's
			// password, speed limits as their own fields, and an obfuscation and a UDP
			// protocol that v2 does not have.
			ent.Bean.Set("auth", firstOrSecond(yStr(proxy, "auth-str"), yStr(proxy, "auth_str")))
			ent.Bean.Set("uploadMbps", firstTokenInt(yStr(proxy, "up")))
			ent.Bean.Set("downloadMbps", firstTokenInt(yStr(proxy, "down")))
			ent.Bean.Set("obfs", yStr(proxy, "obfs"))
			ent.Bean.Set("protocol", yStr(proxy, "protocol"))
			ent.Bean.Set("hopPort", yStr(proxy, "ports"))
			ent.Bean.Set("sni", yStr(proxy, "sni"))
			ent.Bean.Set("alpn", strings.Join(yStrList(proxy, "alpn"), ","))
			ent.Bean.Set("allowInsecure", yBool(proxy, "skip-cert-verify"))
			ent.Bean.Set("caText", yStr(proxy, "ca-str"))

		case typ == "tuic":
			ent.Bean.Set("uuid", yStr(proxy, "uuid"))
			ent.Bean.Set("password", yStr(proxy, "password"))
			if hi := yStr(proxy, "heartbeat-interval"); hi != "" {
				// Clash writes the keepalive in SECONDS and the bean holds a
				// duration. Turning 30 into "30ms" fired the keepalive three
				// hundred times faster than the panel asked for.
				if n, err := strconv.Atoi(strings.TrimSpace(hi)); err == nil {
					hi = strconv.Itoa(n) + "s"
				}
				ent.Bean.Set("heartbeat", hi)
			}
			if v := yStr(proxy, "udp-relay-mode"); v != "" {
				ent.Bean.Set("udpRelayMode", v)
			}
			if v := yStr(proxy, "congestion-controller"); v != "" {
				ent.Bean.Set("congestionControl", v)
			}
			ent.Bean.Set("disableSni", yBool(proxy, "disable-sni"))
			ent.Bean.Set("zeroRttHandshake", yBool(proxy, "reduce-rtt"))
			ent.Bean.Set("allowInsecure", yBool(proxy, "skip-cert-verify"))
			ent.Bean.Set("alpn", strings.Join(yStrList(proxy, "alpn"), ","))
			ent.Bean.Set("caText", yStr(proxy, "ca-str"))
			ent.Bean.Set("sni", yStr(proxy, "sni"))
			if yBool(proxy, "udp-over-stream") {
				ent.Bean.Set("uos", true)
			}
			if ip := yStr(proxy, "ip"); ip != "" {
				if ent.Bean.Str("sni") == "" {
					ent.Bean.Set("sni", ent.ServerAddress())
				}
				ent.Bean.Set("addr", ip)
			}

		default:
			// A type whose bean exists but has no branch here. Both ways of falling
			// through are worth naming in the update: what the reader needs to know is
			// which nodes did not arrive, not which line of this switch dropped them.
			r.skip(yStr(proxy, "name"), typeClash)
			continue
		}

		if needFix {
			fixEnt(ent)
		}
		r.updated = append(r.updated, ent)
	}
}

// --- yaml helpers ---

func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func yValStr(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func yStr(m map[string]interface{}, k string) string {
	if m == nil {
		return ""
	}
	v, ok := m[k]
	if !ok {
		return ""
	}
	return yValStr(v)
}

func yInt(m map[string]interface{}, k string) int {
	s := strings.TrimSpace(yStr(m, k))
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f)
	}
	return 0
}

func yBool(m map[string]interface{}, k string) bool {
	if m == nil {
		return false
	}
	v, ok := m[k]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case int:
		return t != 0
	case float64:
		return t != 0
	case string:
		return t == "true" || t == "1" || t == "yes"
	}
	return false
}

func yList(m map[string]interface{}, k string) []interface{} {
	if m == nil {
		return nil
	}
	if arr, ok := m[k].([]interface{}); ok {
		return arr
	}
	return nil
}

func yStrList(m map[string]interface{}, k string) []string {
	var out []string
	for _, x := range yList(m, k) {
		out = append(out, yValStr(x))
	}
	return out
}

func yChild(m map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		if m == nil {
			return nil
		}
		if v, ok := m[k]; ok {
			if mm, ok := v.(map[string]interface{}); ok {
				return mm
			}
		}
	}
	return nil
}

func firstOrSecond(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstTokenInt(s string) int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(fields[0])
	return n
}
