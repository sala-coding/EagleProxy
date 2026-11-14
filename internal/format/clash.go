package format

import (
	"fmt"
	"strconv"
	"strings"

	"eagle/internal/bean"
)

// ToClashProxy renders one bean as a Clash proxy entry — an item of the
// "proxies" list in a Clash configuration. It is the reading internal/sub's
// updateClash writes back in, field for field, so a node exported here and
// imported there comes home the same node. A protocol the Clash spelling has
// no name for answers nil, and the caller leaves it out of the file — naming
// it, so the export says what it could not take rather than losing it quietly.

func ToClashProxy(typ string, b bean.Bean) map[string]interface{} {
	st := b.Peek("stream")
	out := map[string]interface{}{
		"name":   b.Str("name"),
		"server": b.Str("addr"),
		"port":   b.Int("port"),
	}

	// tlsCommon writes the stream's TLS the way the v2ray-family protocols read
	// it: the security flag, the identity fields, and reality when it is one.
	tlsCommon := func() {
		if st == nil {
			return
		}
		if st.Str("sec") == "tls" {
			out["tls"] = true
		}
		if sni := st.Str("sni"); sni != "" {
			out["servername"] = sni
		}
		if alpn := st.Str("alpn"); alpn != "" {
			out["alpn"] = strings.Split(alpn, ",")
		}
		if st.Bool("insecure") {
			out["skip-cert-verify"] = true
		}
		if fp := st.Str("utls"); fp != "" {
			out["client-fingerprint"] = fp
		}
		if pbk := st.Str("pbk"); pbk != "" {
			out["reality-opts"] = map[string]interface{}{
				"public-key": pbk,
				"short-id":   st.Str("sid"),
			}
		}
	}
	// The transports, each written only when the stream is the one it spells.
	// The websocket's early data rides the same two fields updateClash reads.
	wsOpts := func() {
		if st == nil || st.Str("net") != "ws" {
			return
		}
		ws := map[string]interface{}{"path": st.Str("path")}
		if h := st.Str("host"); h != "" {
			ws["headers"] = map[string]interface{}{"Host": h}
		}
		if ed := st.Int("ed_len"); ed > 0 {
			ws["max-early-data"] = ed
			if n := st.Str("ed_name"); n != "" {
				ws["early-data-header-name"] = n
			}
		}
		out["network"] = "ws"
		out["ws-opts"] = ws
	}
	grpcOpts := func() {
		if st == nil || st.Str("net") != "grpc" {
			return
		}
		out["network"] = "grpc"
		out["grpc-opts"] = map[string]interface{}{"grpc-service-name": st.Str("path")}
	}
	h2Opts := func() {
		// The bean says "http" where Clash says "h2" — the one word the two
		// spellings hold in different languages (see updateClash's vmess).
		if st == nil || st.Str("net") != "http" {
			return
		}
		h2 := map[string]interface{}{"path": st.Str("path")}
		if h := st.Str("host"); h != "" {
			h2["host"] = []interface{}{h}
		}
		out["network"] = "h2"
		out["h2-opts"] = h2
	}
	httpOpts := func() {
		if st == nil || st.Str("net") != "tcp" || st.Str("h_type") != "http" {
			return
		}
		o := map[string]interface{}{"path": []interface{}{st.Str("path")}}
		if h := st.Str("host"); h != "" {
			o["headers"] = map[string]interface{}{"Host": []interface{}{h}}
		}
		out["http-opts"] = o
	}
	smux := func() {
		if st != nil && st.Int("mux_s") != 0 {
			out["smux"] = map[string]interface{}{"enabled": true}
		}
	}
	// The hop ranges, the way clash writes and updateClash reads them: verbatim,
	// dash ranges joined by colons.
	ports := func() {
		if p := b.Str("hopPort"); p != "" {
			out["ports"] = p
		}
	}
	speeds := func() {
		if u := b.Int("uploadMbps"); u > 0 {
			out["up"] = fmt.Sprintf("%d Mbps", u)
		}
		if d := b.Int("downloadMbps"); d > 0 {
			out["down"] = fmt.Sprintf("%d Mbps", d)
		}
	}
	quicTLS := func() {
		if sni := b.Str("sni"); sni != "" {
			out["sni"] = sni
		}
		if alpn := b.Str("alpn"); alpn != "" {
			out["alpn"] = strings.Split(alpn, ",")
		}
		if b.Bool("allowInsecure") {
			out["skip-cert-verify"] = true
		}
		if ca := b.Str("caText"); ca != "" {
			out["ca-str"] = ca
		}
	}

	switch typ {
	case "shadowsocks":
		out["type"] = "ss"
		out["cipher"] = b.Str("method")
		out["password"] = b.Str("pass")
		if uot := b.Int("uot"); uot != 0 {
			out["udp-over-tcp"] = true
			out["udp-over-tcp-version"] = uot
		}
		// The plugin travels joined the way the share links spell it —
		// "obfs-local;obfs=http;obfs-host=x" — and is split back into the two
		// panel shapes updateClash writes in. A spelling this side did not
		// write is left off rather than guessed at.
		if plug := b.Str("plugin"); plug != "" {
			parts := strings.Split(plug, ";")
			switch parts[0] {
			case "obfs-local":
				out["plugin"] = "obfs"
				opts := map[string]interface{}{}
				for _, kv := range parts[1:] {
					if v, ok := strings.CutPrefix(kv, "obfs="); ok {
						opts["mode"] = v
					}
					if v, ok := strings.CutPrefix(kv, "obfs-host="); ok {
						opts["host"] = v
					}
				}
				out["plugin-opts"] = opts
			case "v2ray-plugin":
				out["plugin"] = "v2ray-plugin"
				opts := map[string]interface{}{}
				for _, kv := range parts[1:] {
					if v, ok := strings.CutPrefix(kv, "mode="); ok && v != "websocket" {
						opts["mode"] = v
					}
					if kv == "tls" {
						opts["tls"] = true
					}
					if v, ok := strings.CutPrefix(kv, "host="); ok {
						opts["host"] = v
					}
					if v, ok := strings.CutPrefix(kv, "path="); ok {
						opts["path"] = v
					}
				}
				out["plugin-opts"] = opts
			}
		}
		smux()

	case "socks":
		if b.Int("v") == 4 {
			out["type"] = "socks4"
		} else {
			out["type"] = "socks5"
		}
		out["username"] = b.Str("username")
		out["password"] = b.Str("password")
		if st != nil {
			if st.Str("sec") == "tls" {
				out["tls"] = true
			}
			if st.Bool("insecure") {
				out["skip-cert-verify"] = true
			}
		}

	case "http":
		out["type"] = "http"
		out["username"] = b.Str("username")
		out["password"] = b.Str("password")
		if st != nil {
			if st.Str("sec") == "tls" {
				out["tls"] = true
			}
			if st.Bool("insecure") {
				out["skip-cert-verify"] = true
			}
		}

	case "vmess":
		out["type"] = "vmess"
		out["uuid"] = b.Str("id")
		out["alterId"] = b.Int("aid")
		out["cipher"] = b.Str("sec")
		tlsCommon()
		wsOpts()
		grpcOpts()
		h2Opts()
		httpOpts()
		smux()
		if st != nil {
			if pe := st.Str("pac_enc"); pe == "xudp" {
				out["xudp"] = true
			} else if pe == "packetaddr" {
				out["packet-addr"] = true
			}
		}

	case "trojan":
		out["type"] = "trojan"
		out["password"] = b.Str("pass")
		tlsCommon()
		wsOpts()
		grpcOpts()
		h2Opts()
		httpOpts()
		smux()

	case "vless":
		out["type"] = "vless"
		out["uuid"] = b.Str("pass")
		if f := b.Str("flow"); f != "" {
			out["flow"] = f
		}
		tlsCommon()
		wsOpts()
		grpcOpts()
		h2Opts()
		httpOpts()
		smux()
		if st != nil {
			if pe := st.Str("pac_enc"); pe == "packetaddr" {
				out["packet-addr"] = true
			} else if pe == "xudp" {
				out["xudp"] = true
			}
		}

	case "anytls":
		out["type"] = "anytls"
		out["password"] = b.Str("pass")
		tlsCommon()

	case "hysteria2":
		out["type"] = "hysteria2"
		out["password"] = b.Str("password")
		ports()
		speeds()
		if o := b.Str("obfsPassword"); o != "" {
			out["obfs-password"] = o
		}
		quicTLS()

	case "hysteria":
		out["type"] = "hysteria"
		if a := b.Str("auth"); a != "" {
			out["auth-str"] = a
		}
		if o := b.Str("obfs"); o != "" {
			out["obfs"] = o
		}
		if p := b.Str("protocol"); p != "" && p != "udp" {
			out["protocol"] = p
		}
		ports()
		speeds()
		quicTLS()

	case "tuic":
		out["type"] = "tuic"
		out["uuid"] = b.Str("uuid")
		out["password"] = b.Str("password")
		// The bean holds a duration and clash holds seconds — the conversion
		// updateClash makes coming in, made going out.
		if hb := strings.TrimSpace(b.Str("heartbeat")); strings.HasSuffix(hb, "s") {
			if n, err := strconv.Atoi(strings.TrimSuffix(hb, "s")); err == nil && n > 0 {
				out["heartbeat-interval"] = n
			}
		}
		if v := b.Str("udpRelayMode"); v != "" {
			out["udp-relay-mode"] = v
		}
		if v := b.Str("congestionControl"); v != "" {
			out["congestion-controller"] = v
		}
		if b.Bool("disableSni") {
			out["disable-sni"] = true
		}
		if b.Bool("zeroRttHandshake") {
			out["reduce-rtt"] = true
		}
		if b.Bool("uos") {
			out["udp-over-stream"] = true
		}
		quicTLS()

	case "snell":
		out["type"] = "snell"
		out["psk"] = b.Str("psk")
		if v := b.Int("version"); v > 0 {
			out["version"] = v
		}
		if m := b.Str("obfsMode"); m != "" {
			opts := map[string]interface{}{"mode": m}
			if h := b.Str("obfsHost"); h != "" {
				opts["host"] = h
			}
			out["obfs-opts"] = opts
		}

	default:
		return nil
	}
	return out
}
