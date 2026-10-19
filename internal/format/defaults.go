// Package format implements share-link parsing/generation and default bean
// construction, mirroring nekoray's fmt/Link2Bean.cpp and fmt/Bean2Link.cpp.
package format

import "eagle/internal/bean"

// Stream returns a new V2rayStreamSettings object with nekoray defaults.
func newStream() bean.Bean {
	return bean.Bean{
		"net":      "tcp",
		"sec":      "",
		"pac_enc":  "",
		"path":     "",
		"host":     "",
		"sni":      "",
		"alpn":     "",
		"cert":     "",
		"insecure": false,
		"h_type":   "",
		"ed_name":  "",
		"ed_len":   0,
		"utls":     "",
		"pbk":      "",
		"sid":      "",
		"spx":      "",
		"mux_s":    0,
	}
}

func baseBean(version int) bean.Bean {
	return bean.Bean{
		"_v":    version,
		"name":  "",
		"addr":  "127.0.0.1",
		"port":  1080,
		"c_cfg": "",
		"c_out": "",
	}
}

func withStream(b bean.Bean) bean.Bean {
	b.Set("stream", map[string]interface{}(newStream()))
	return b
}

// NewSocksHTTP creates a socks/http bean. kind: 4 (socks4), 5 (socks5), -80 (http).
func NewSocksHTTP(kind int) bean.Bean {
	b := baseBean(0)
	b.Set("v", kind)
	b.Set("username", "")
	b.Set("password", "")
	return withStream(b)
}

// NewShadowSocks creates a shadowsocks bean.
func NewShadowSocks() bean.Bean {
	b := baseBean(0)
	b.Set("method", "aes-128-gcm")
	b.Set("pass", "")
	b.Set("plugin", "")
	b.Set("uot", 0)
	return withStream(b)
}

// NewVMess creates a vmess bean.
func NewVMess() bean.Bean {
	b := baseBean(0)
	b.Set("id", "")
	b.Set("aid", 0)
	b.Set("sec", "auto")
	return withStream(b)
}

// NewTrojanVLESS creates a trojan/vless bean (protocol distinguished by type).
func NewTrojanVLESS() bean.Bean {
	b := baseBean(0)
	b.Set("pass", "")
	b.Set("flow", "")
	return withStream(b)
}

// NewHysteria2 creates a hysteria2 bean.
func NewHysteria2() bean.Bean {
	b := baseBean(0)
	b.Set("forceExternal", false)
	b.Set("obfsPassword", "")
	b.Set("uploadMbps", 0)
	b.Set("downloadMbps", 0)
	b.Set("streamReceiveWindow", 0)
	b.Set("connectionReceiveWindow", 0)
	b.Set("disableMtuDiscovery", false)
	b.Set("hopInterval", 10)
	b.Set("hopPort", "")
	b.Set("password", "")
	b.Set("allowInsecure", false)
	b.Set("sni", "")
	b.Set("alpn", "")
	b.Set("caText", "")
	b.Set("disableSni", false)
	return b
}

// NewSnell creates a snell bean: a pre-shared key, the protocol version, and the two
// halves of snell's own obfuscation — a mode, and the host that mode presents.
func NewSnell() bean.Bean {
	b := baseBean(0)
	b.Set("psk", "")
	b.Set("version", 4)
	b.Set("obfsMode", "")
	b.Set("obfsHost", "")
	return b
}

// NewHysteria1 creates a hysteria v1 bean. It is not the hysteria2 bean with a flag
// turned on: the two share their speed knobs and little else — v1 authenticates with a
// single auth string, and carries an obfuscation and a UDP protocol that v2 has no
// notion of — so they are two shapes rather than one with a switch.
func NewHysteria1() bean.Bean {
	b := baseBean(0)
	b.Set("forceExternal", false)
	b.Set("auth", "")
	b.Set("obfs", "")
	b.Set("protocol", "")
	b.Set("uploadMbps", 0)
	b.Set("downloadMbps", 0)
	b.Set("streamReceiveWindow", 0)
	b.Set("connectionReceiveWindow", 0)
	b.Set("disableMtuDiscovery", false)
	b.Set("hopInterval", 10)
	b.Set("hopPort", "")
	b.Set("allowInsecure", false)
	b.Set("sni", "")
	b.Set("alpn", "")
	b.Set("caText", "")
	b.Set("disableSni", false)
	return b
}

// NewTUIC creates a tuic bean.
func NewTUIC() bean.Bean {
	b := baseBean(0)
	b.Set("forceExternal", false)
	b.Set("uuid", "")
	b.Set("password", "")
	b.Set("congestionControl", "bbr")
	b.Set("udpRelayMode", "native")
	b.Set("zeroRttHandshake", false)
	b.Set("heartbeat", "10s")
	b.Set("uos", false)
	b.Set("allowInsecure", false)
	b.Set("sni", "")
	b.Set("alpn", "")
	b.Set("caText", "")
	b.Set("disableSni", false)
	return b
}

// NewNaive creates a naive bean.
func NewNaive() bean.Bean {
	b := baseBean(0)
	b.Set("username", "")
	b.Set("password", "")
	b.Set("protocol", "https")
	b.Set("extra_headers", "")
	b.Set("sni", "")
	b.Set("certificate", "")
	b.Set("insecure_concurrency", 0)
	b.Set("disable_log", false)
	return b
}

// NewCustom creates a custom bean.
func NewCustom() bean.Bean {
	b := baseBean(0)
	b.Set("core", "")
	b.Set("cmd", []interface{}{})
	b.Set("cs", "")
	b.Set("cs_suffix", "")
	b.Set("mapping_port", 0)
	b.Set("socks_port", 0)
	return b
}

// NewChain creates a chain bean.
func NewChain() bean.Bean {
	b := baseBean(0)
	b.Set("list", []interface{}{})
	return b
}

// New creates an empty bean for a protocol type (mirrors ProfileManager::NewProxyEntity).
func New(typ string) bean.Bean {
	switch typ {
	case "socks":
		return NewSocksHTTP(5)
	case "http":
		return NewSocksHTTP(-80)
	case "shadowsocks":
		return NewShadowSocks()
	case "vmess":
		return NewVMess()
	case "trojan", "vless", "anytls":
		// anytls reuses the trojan shape: a password plus TLS options on the stream
		return NewTrojanVLESS()
	case "hysteria2":
		return NewHysteria2()
	case "hysteria":
		return NewHysteria1()
	case "snell":
		return NewSnell()
	case "tuic":
		return NewTUIC()
	case "naive":
		return NewNaive()
	case "custom":
		return NewCustom()
	case "chain":
		return NewChain()
	default:
		b := baseBean(-114514)
		return b
	}
}
