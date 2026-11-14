package sub

import (
	"encoding/json"
	"strconv"
	"strings"

	"eagle/internal/bean"
	"eagle/internal/db"
	"eagle/internal/format"
)

// singboxIgnored is the outbound types of a sing-box config that are structure
// rather than node: the selector groups and urltest bundles the node list is
// wrapped in, and the always-there direct/block/dns outbounds the route block
// references. They are passed over silently — not nodes the file carried that
// this build refused, but the parts every sing-box config is made of.
var singboxIgnored = map[string]bool{
	"selector": true, "urltest": true,
	"direct": true, "block": true, "dns": true, "route": true,
	"special": true, "legacy": true,
}

// ParseSingBoxConfig walks a sing-box configuration's outbounds and turns
// every node it can speak into an entity — the reverse walk of what
// configbuilder writes, field by field. Everything but the outbounds (dns,
// route, inbounds) is ignored on the way in: the app builds its own config
// around the nodes it imports. The skipped list names the outbounds that
// arrived but could not land — a protocol this build does not carry, or one
// whose transport it cannot speak — in the same "name (type)" shape the
// subscription reader skips in.
func ParseSingBoxConfig(content []byte, gid int) ([]*db.ProxyEntity, []string, error) {
	var root struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(content, &root); err != nil {
		return nil, nil, err
	}

	var ents []*db.ProxyEntity
	var skipped []string
	for _, ob := range root.Outbounds {
		typ := yStr(ob, "type")
		tag := yStr(ob, "tag")
		if typ == "" || singboxIgnored[typ] {
			continue
		}
		ent, why := singboxOutbound(ob, typ, tag, gid)
		if ent == nil {
			// nil with no reason is a protocol this build does not carry (ssh,
			// wireguard, shadowtls, snell as a sing-box outbound...): named like
			// every other skip, the reader expecting a node is told which one.
			skipped = append(skipped, skipLabel(tag, typ, why))
			continue
		}
		ents = append(ents, ent)
	}
	return ents, skipped, nil
}

// skipLabel is one skipped outbound as it reaches the report: the tag it went
// by, the type it said it was, and — when the reason is one this import can
// name — what was wrong with it.
func skipLabel(tag, typ, why string) string {
	if strings.TrimSpace(tag) == "" {
		tag = "(未命名)"
	}
	if why != "" {
		return tag + " (" + typ + ": " + why + ")"
	}
	return tag + " (" + typ + ")"
}

// singboxOutbound maps one sing-box outbound to an entity of the same shape
// configbuilder reads. An unsupported type answers (nil, ""); a known one that
// arrived without the parts a node needs answers (nil, reason).
func singboxOutbound(ob map[string]interface{}, typ, tag string, gid int) (*db.ProxyEntity, string) {
	server := yStr(ob, "server")
	port := yInt(ob, "server_port")
	tlsOb := asMap(ob["tls"])
	transport := asMap(ob["transport"])

	mk := func() *db.ProxyEntity {
		ent := &db.ProxyEntity{Type: typ, ID: -1, GID: gid, Bean: format.New(typ)}
		ent.Ensure()
		ent.Bean.Set("name", tag)
		ent.Bean.Set("addr", server)
		ent.Bean.Set("port", port)
		return ent
	}

	switch typ {
	case "shadowsocks":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("method", yStr(ob, "method"))
		b.Set("pass", yStr(ob, "password"))
		if uot := asMap(ob["udp_over_tcp"]); uot != nil && yBool(uot, "enabled") {
			v := yInt(uot, "version")
			if v == 0 {
				v = 2
			}
			b.Set("uot", v)
		}
		// The bean carries the plugin the way the share links spell it —
		// "obfs-local;obfs=http;obfs-host=x" — and configbuilder splits it on the
		// first semicolon. Joined back together here, so a config that names an
		// object for plugin_opts (v2ray-plugin's shape) still keeps it whole.
		if plug := yStr(ob, "plugin"); plug != "" {
			opts := yStr(ob, "plugin_opts")
			if o := asMap(ob["plugin_opts"]); o != nil {
				if data, err := json.Marshal(o); err == nil {
					opts = string(data)
				}
			}
			b.Set("plugin", plug+";"+opts)
		}
		singboxStreamTLS(b.Peek("stream"), tlsOb)
		if why := singboxStreamTransport(b.Peek("stream"), transport); why != "" {
			return nil, why
		}
		return ent, ""

	case "vmess":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("id", yStr(ob, "uuid"))
		b.Set("aid", yInt(ob, "alter_id"))
		if c := yStr(ob, "security"); c != "" {
			b.Set("sec", c)
		}
		singboxStreamTLS(b.Peek("stream"), tlsOb)
		if why := singboxStreamTransport(b.Peek("stream"), transport); why != "" {
			return nil, why
		}
		if pe := yStr(ob, "packet_encoding"); pe != "" {
			b.Peek("stream").Set("pac_enc", pe)
		}
		return ent, ""

	case "vless":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("pass", yStr(ob, "uuid"))
		b.Set("flow", yStr(ob, "flow"))
		st := b.Peek("stream")
		singboxStreamTLS(st, tlsOb)
		if why := singboxStreamTransport(st, transport); why != "" {
			return nil, why
		}
		if pe := yStr(ob, "packet_encoding"); pe != "" {
			st.Set("pac_enc", pe)
		}
		return ent, ""

	case "trojan":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("pass", yStr(ob, "password"))
		singboxStreamTLS(b.Peek("stream"), tlsOb)
		if why := singboxStreamTransport(b.Peek("stream"), transport); why != "" {
			return nil, why
		}
		return ent, ""

	case "anytls":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("pass", yStr(ob, "password"))
		// The protocol is TLS-only: the bean arrives with sec=tls preset (see
		// format.NewAnyTLS), and the tls block only adds the identity fields.
		singboxStreamTLS(b.Peek("stream"), tlsOb)
		return ent, ""

	case "hysteria2":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("password", yStr(ob, "password"))
		if obf := asMap(ob["obfs"]); obf != nil {
			b.Set("obfsPassword", yStr(obf, "password"))
		}
		b.Set("sni", yStr(tlsOb, "server_name"))
		b.Set("alpn", strings.Join(yStrList(tlsOb, "alpn"), ","))
		b.Set("allowInsecure", yBool(tlsOb, "insecure"))
		b.Set("caText", yStr(tlsOb, "certificate"))
		b.Set("disableSni", yBool(tlsOb, "disable_sni"))
		b.Set("uploadMbps", yInt(ob, "up_mbps"))
		b.Set("downloadMbps", yInt(ob, "down_mbps"))
		b.Set("hopPort", portRangesFromSingBox(yStrList(ob, "server_ports")))
		b.Set("hopInterval", durationSeconds(yStr(ob, "hop_interval")))
		return ent, ""

	case "hysteria":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("auth", yStr(ob, "auth_str"))
		b.Set("obfs", yStr(ob, "obfs"))
		b.Set("protocol", yStr(ob, "protocol"))
		b.Set("uploadMbps", yInt(ob, "up_mbps"))
		b.Set("downloadMbps", yInt(ob, "down_mbps"))
		b.Set("hopPort", portRangesFromSingBox(yStrList(ob, "server_ports")))
		b.Set("hopInterval", durationSeconds(yStr(ob, "hop_interval")))
		b.Set("sni", yStr(tlsOb, "server_name"))
		b.Set("alpn", strings.Join(yStrList(tlsOb, "alpn"), ","))
		b.Set("allowInsecure", yBool(tlsOb, "insecure"))
		b.Set("caText", yStr(tlsOb, "certificate"))
		b.Set("disableSni", yBool(tlsOb, "disable_sni"))
		return ent, ""

	case "tuic":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		b.Set("uuid", yStr(ob, "uuid"))
		b.Set("password", yStr(ob, "password"))
		if v := yStr(ob, "congestion_control"); v != "" {
			b.Set("congestionControl", v)
		}
		if v := yStr(ob, "udp_relay_mode"); v != "" {
			b.Set("udpRelayMode", v)
		}
		b.Set("zeroRttHandshake", yBool(ob, "zero_rtt_handshake"))
		if v := yStr(ob, "heartbeat"); v != "" {
			b.Set("heartbeat", v)
		}
		if yBool(ob, "udp_over_stream") {
			b.Set("uos", true)
		}
		b.Set("sni", yStr(tlsOb, "server_name"))
		b.Set("alpn", strings.Join(yStrList(tlsOb, "alpn"), ","))
		b.Set("allowInsecure", yBool(tlsOb, "insecure"))
		b.Set("caText", yStr(tlsOb, "certificate"))
		b.Set("disableSni", yBool(tlsOb, "disable_sni"))
		return ent, ""

	case "socks", "http":
		if server == "" || port <= 0 {
			return nil, "缺地址或端口"
		}
		ent := mk()
		b := ent.Bean
		if typ == "socks" && yInt(ob, "version") == 1 {
			b.Set("v", 4)
		}
		b.Set("username", yStr(ob, "username"))
		b.Set("password", yStr(ob, "password"))
		singboxStreamTLS(b.Peek("stream"), tlsOb)
		return ent, ""
	}
	return nil, ""
}

// singboxStreamTLS maps a stream-shaped tls block back into the stream bean —
// the fields configbuilder's applyStreamSettings writes. Typed in from a nil
// tls block: a plain node keeps the defaults it was created with.
func singboxStreamTLS(st bean.Bean, tls map[string]interface{}) {
	if st == nil || tls == nil {
		return
	}
	if yBool(tls, "enabled") {
		st.Set("sec", "tls")
	}
	st.Set("insecure", yBool(tls, "insecure"))
	st.Set("sni", yStr(tls, "server_name"))
	st.Set("cert", yStr(tls, "certificate"))
	st.Set("alpn", strings.Join(yStrList(tls, "alpn"), ","))
	if utls := asMap(tls["utls"]); utls != nil && yBool(utls, "enabled") {
		st.Set("utls", yStr(utls, "fingerprint"))
	}
	if reality := asMap(tls["reality"]); reality != nil && yBool(reality, "enabled") {
		st.Set("pbk", yStr(reality, "public_key"))
		st.Set("sid", yStr(reality, "short_id"))
	}
}

// singboxStreamTransport maps a transport block back into the stream bean. A
// transport type the build cannot speak answers a reason — the node is skipped
// whole, since half a transport is a node that connects to nothing.
func singboxStreamTransport(st bean.Bean, tr map[string]interface{}) string {
	if st == nil || tr == nil {
		return ""
	}
	switch net := yStr(tr, "type"); net {
	case "ws":
		st.Set("net", "ws")
		if hdrs := asMap(tr["headers"]); hdrs != nil {
			for k, v := range hdrs {
				if strings.ToLower(k) == "host" {
					st.Set("host", yValStr(v))
				}
			}
		}
		st.Set("path", yStr(tr, "path"))
		if ed := yInt(tr, "max_early_data"); ed > 0 {
			st.Set("ed_len", ed)
			st.Set("ed_name", yStr(tr, "early_data_header_name"))
		}
	case "http":
		st.Set("net", "http")
		st.Set("path", yStr(tr, "path"))
		st.Set("host", strings.Join(yStrList(tr, "host"), ","))
	case "grpc":
		st.Set("net", "grpc")
		st.Set("path", yStr(tr, "service_name"))
	case "httpupgrade":
		st.Set("net", "httpupgrade")
		st.Set("path", yStr(tr, "path"))
		st.Set("host", yStr(tr, "host"))
	default:
		return "transport " + net + " 不支持"
	}
	return ""
}

// portRangesFromSingBox turns "server_ports": ["51000:53000"] back into the
// bean's spelling, "51000-53000" — the colon and dash are each side's own
// separator for the same idea (see configbuilder's hopPortRanges).
func portRangesFromSingBox(ranges []string) string {
	var out []string
	for _, r := range ranges {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, strings.ReplaceAll(r, ":", "-"))
		}
	}
	return strings.Join(out, ",")
}

// durationSeconds reads the duration spelling sing-box writes hop_interval in
// ("10s") into the plain count of seconds the bean holds. An empty or
// unreadable value is zero: the bean's default applies, and sing-box's own
// default equals it.
func durationSeconds(v string) int {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "ms") {
		if n, err := strconv.Atoi(v[:len(v)-2]); err == nil {
			return n / 1000
		}
		return 0
	}
	if strings.HasSuffix(v, "s") {
		v = v[:len(v)-1]
	}
	n, _ := strconv.Atoi(v)
	return n
}
