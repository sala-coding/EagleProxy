package db

import (
	"fmt"
	"strings"

	"eagle/internal/bean"
)

// ProxyEntity mirrors nekoray's ProxyEntity JSON layout:
//
//	{"type","id","gid","yc","report","bean":{...},"traffic":{...}}
type ProxyEntity struct {
	Type    string `json:"type"`
	ID      int    `json:"id"`
	GID     int    `json:"gid"`
	Latency int    `json:"yc"`
	Report  string `json:"report"`
	// Speed is the last download-speed result in MB/s. 0 means never measured,
	// a negative value means the last measurement of it failed. It is kept
	// independently of Latency: each column shows the newest result of its own
	// metric.
	Speed   float64   `json:"speed"`
	Bean    bean.Bean `json:"bean"`
	Traffic bean.Bean `json:"traffic"`
}

// Ensure initialises nested objects if they are nil.
func (e *ProxyEntity) Ensure() {
	if e.Bean == nil {
		e.Bean = bean.New()
	}
	if e.Traffic == nil {
		e.Traffic = bean.New()
	}
}

// deepCopyValue copies the JSON-shaped values a Bean may carry. Values left as-is
// (primitives, strings, bools) are immutable anyway; maps, slices and nested
// containers are rebuilt so a clone shares nothing with its source.
func deepCopyValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, item := range x {
			out[k] = deepCopyValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = deepCopyValue(item)
		}
		return out
	case []int:
		return append([]int(nil), x...)
	case []string:
		return append([]string(nil), x...)
	default:
		return x
	}
}

func cloneBean(b bean.Bean) bean.Bean {
	if b == nil {
		return nil
	}
	out := bean.New()
	for k, v := range b {
		out[k] = deepCopyValue(v)
	}
	return out
}

// clone returns an independent deep copy of e. The manager hands clones to
// read-only consumers so their walk of the bean maps cannot race a writer.
func (e *ProxyEntity) clone() *ProxyEntity {
	if e == nil {
		return nil
	}
	out := *e
	out.Bean = cloneBean(e.Bean)
	out.Traffic = cloneBean(e.Traffic)
	return &out
}

// Clone is the exported deep copy (used by callers that need their own instance).
func (e *ProxyEntity) Clone() *ProxyEntity { return e.clone() }

// clone returns an independent copy of g, with its slices rebuilt.
func (g *Group) clone() *Group {
	if g == nil {
		return nil
	}
	out := *g
	out.Order = append([]int(nil), g.Order...)
	out.ColumnWidth = append([]int(nil), g.ColumnWidth...)
	return &out
}

// Name returns the configured display name.
func (e *ProxyEntity) Name() string { return e.Bean.Str("name") }

// ServerAddress returns the server host.
func (e *ProxyEntity) ServerAddress() string { return e.Bean.Str("addr") }

// ServerPort returns the server port.
func (e *ProxyEntity) ServerPort() int { return e.Bean.Int("port") }

// DisplayAddress renders "host:port".
func (e *ProxyEntity) DisplayAddress() string {
	addr := e.ServerAddress()
	port := e.ServerPort()
	if addr == "" && port == 0 {
		return ""
	}
	if strings.Contains(addr, ":") {
		addr = "[" + strings.Trim(addr, "[]") + "]"
	}
	return fmt.Sprintf("%s:%d", addr, port)
}

// DisplayName falls back to the address when no name is set.
func (e *ProxyEntity) DisplayName() string {
	if n := e.Name(); n != "" {
		return n
	}
	return e.DisplayAddress()
}

// DisplayType returns a human readable protocol label.
func (e *ProxyEntity) DisplayType() string {
	switch e.Type {
	case "socks":
		if e.Bean.Int("v") == -80 {
			return "HTTP"
		}
		return "Socks"
	case "http":
		return "HTTP"
	case "shadowsocks":
		return "Shadowsocks"
	case "vmess":
		return "VMess"
	case "trojan":
		return "Trojan"
	case "vless":
		return "VLESS"
	case "hysteria2":
		return "Hysteria2"
	case "hysteria":
		return "Hysteria"
	case "snell":
		return "Snell"
	case "anytls":
		return "AnyTLS"
	case "tuic":
		return "TUIC"
	case "naive":
		return "Naive"
	case "chain":
		return "Chain"
	case "custom":
		if e.Bean.Str("core") == "internal" {
			return "Custom"
		}
		return e.Bean.Str("core")
	default:
		return e.Type
	}
}

// ConnectionInfo summarises how the profile connects, for the list columns:
// the transport (tcp / ws / grpc / ...), a TLS label (empty when plain) and a
// tooltip with the parts that matter when a node refuses to work (SNI, Host,
// ALPN, uTLS fingerprint, certificate validation).
func (e *ProxyEntity) ConnectionInfo() (network, security, note string) {
	network = "tcp"
	stream := e.Bean.Peek("stream")
	if stream == nil {
		return network, "", ""
	}
	if n := strings.TrimSpace(stream.Str("net")); n != "" {
		network = n
	}

	var parts []string
	switch sec := strings.TrimSpace(stream.Str("sec")); sec {
	case "tls":
		security = "TLS"
	case "reality":
		security = "REALITY"
	}
	if sni := strings.TrimSpace(stream.Str("sni")); sni != "" {
		parts = append(parts, "SNI: "+sni)
	}
	if host := strings.TrimSpace(stream.Str("host")); host != "" {
		parts = append(parts, "Host: "+host)
	}
	if alpn := strings.TrimSpace(stream.Str("alpn")); alpn != "" {
		parts = append(parts, "ALPN: "+alpn)
	}
	if fp := strings.TrimSpace(stream.Str("utls")); fp != "" {
		parts = append(parts, "uTLS: "+fp)
	}
	if strings.TrimSpace(stream.Str("pbk")) != "" {
		parts = append(parts, "REALITY 公钥已设置")
	}
	if stream.Bool("insecure") {
		parts = append(parts, "跳过证书验证")
		if security != "" {
			security += " · insecure"
		}
	}
	return network, security, strings.Join(parts, " · ")
}

// DisplayCoreType returns the core backing this profile.
func (e *ProxyEntity) DisplayCoreType() string {
	switch e.Type {
	case "naive":
		return "naive"
	case "hysteria2":
		if e.Bean.Bool("forceExternal") {
			return "hysteria2"
		}
		return "sing-box"
	case "tuic":
		if e.Bean.Bool("forceExternal") {
			return "tuic"
		}
		return "sing-box"
	case "custom":
		core := e.Bean.Str("core")
		if core == "internal" || core == "internal-full" {
			return "sing-box"
		}
		if core == "" {
			return "custom"
		}
		return core
	default:
		return "sing-box"
	}
}

// Group mirrors nekoray's Group JSON layout.
type Group struct {
	ID                  int    `json:"id"`
	FrontProxyID        int    `json:"front_proxy_id"`
	Archive             bool   `json:"archive"`
	Name                string `json:"name"`
	Order               []int  `json:"order"`
	URL                 string `json:"url"`
	Info                string `json:"info"`
	SubLastUpdate       int64  `json:"lastup"`
	SubInterval         int    `json:"sub_interval"` // minutes between auto updates, 0 = off
	ManuallyColumnWidth bool   `json:"manually_column_width"`
	ColumnWidth         []int  `json:"column_width"`
}
