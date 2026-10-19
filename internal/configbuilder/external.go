package configbuilder

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"eagle/internal/db"
	"eagle/internal/util"
)

// ExternalSpec describes an external core process to launch.
type ExternalSpec struct {
	Tag          string
	Program      string
	Args         []string
	Env          []string
	ConfigExport string
}

func (b *builder) extraCorePath(id string) string {
	if b.s.ExtraCore == nil {
		return ""
	}
	return b.s.ExtraCore.Get(id)
}

func (b *builder) tempDir() string {
	return filepath.Join(b.s.BaseDir(), "temp")
}

// writeTemp writes data to a temp file and returns its path.
func (b *builder) writeTemp(prefix, suffix string, data []byte) (string, error) {
	dir := b.tempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s_%s%s", prefix, randString(10), suffix)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (b *builder) buildExternal(ent *db.ProxyEntity, mappingPort, socksPort, externalStat int) (*ExternalSpec, error) {
	switch ent.Type {
	case "naive":
		return b.buildExternalNaive(ent, mappingPort, socksPort, externalStat)
	case "hysteria2", "tuic":
		return b.buildExternalQUIC(ent, mappingPort, socksPort)
	case "custom":
		return b.buildExternalCustom(ent, mappingPort, socksPort)
	default:
		return nil, fmt.Errorf("external core not supported for type: %s", ent.Type)
	}
}

func (b *builder) buildExternalNaive(ent *db.ProxyEntity, mappingPort, socksPort, externalStat int) (*ExternalSpec, error) {
	bb := ent.Bean
	spec := &ExternalSpec{Program: b.extraCorePath("naive")}

	isDirect := externalStat == 2
	domainAddress := bb.Str("sni")
	if domainAddress == "" {
		domainAddress = ent.ServerAddress()
	}
	connectAddress := ent.ServerAddress()
	connectPort := ent.ServerPort()
	if !isDirect {
		connectAddress = "127.0.0.1"
		connectPort = mappingPort
	}

	protocol := bb.Str("protocol")
	if protocol == "" {
		protocol = "https"
	}
	proxyURL := protocol + "://"
	if user := bb.Str("username"); user != "" {
		proxyURL += user
		if pw := bb.Str("password"); pw != "" {
			proxyURL += ":" + pw
		}
		proxyURL += "@"
	}
	proxyURL += joinHostPort(domainAddress, connectPort)

	if !bb.Bool("disable_log") {
		spec.Args = append(spec.Args, "--log")
	}
	spec.Args = append(spec.Args, "--listen=socks://127.0.0.1:"+strconv.Itoa(socksPort))
	spec.Args = append(spec.Args, "--proxy="+proxyURL)
	if !sameHost(domainAddress, connectAddress) {
		spec.Args = append(spec.Args,
			"--host-resolver-rules=MAP "+util.WrapIPv6Host(domainAddress)+" "+util.WrapIPv6Host(connectAddress))
	}
	if n := bb.Int("insecure_concurrency"); n > 0 {
		spec.Args = append(spec.Args, "--insecure-concurrency="+strconv.Itoa(n))
	}
	if h := strings.TrimSpace(bb.Str("extra_headers")); h != "" {
		spec.Args = append(spec.Args, "--extra-headers="+h)
	}
	if cert := strings.TrimSpace(bb.Str("certificate")); cert != "" {
		path, err := b.writeTemp("naive", ".crt", []byte(cert))
		if err != nil {
			return nil, err
		}
		spec.Env = append(spec.Env, "SSL_CERT_FILE="+path)
	}

	spec.ConfigExport = strings.Join(append([]string{spec.Program}, spec.Args...), " ")
	return spec, nil
}

func (b *builder) buildExternalQUIC(ent *db.ProxyEntity, mappingPort, socksPort int) (*ExternalSpec, error) {
	bb := ent.Bean
	if ent.Type == "tuic" {
		spec := &ExternalSpec{Program: b.extraCorePath("tuic")}

		relay := map[string]interface{}{
			"uuid":               bb.Str("uuid"),
			"password":           bb.Str("password"),
			"udp_relay_mode":     bb.Str("udpRelayMode"),
			"congestion_control": bb.Str("congestionControl"),
			"zero_rtt_handshake": bb.Bool("zeroRttHandshake"),
			"disable_sni":        bb.Bool("disableSni"),
		}
		if hb := strings.TrimSpace(bb.Str("heartbeat")); hb != "" {
			relay["heartbeat"] = hb
		}
		if alpn := strings.TrimSpace(bb.Str("alpn")); alpn != "" {
			relay["alpn"] = splitComma(alpn)
		}
		if ca := strings.TrimSpace(bb.Str("caText")); ca != "" {
			path, err := b.writeTemp("tuic", ".crt", []byte(ca))
			if err != nil {
				return nil, err
			}
			relay["certificates"] = []interface{}{path}
		}
		if ent.ServerAddress() == bb.Str("sni") {
			relay["server"] = joinHostPort(ent.ServerAddress(), ent.ServerPort())
		} else {
			relay["server"] = joinHostPort(bb.Str("sni"), ent.ServerPort())
			relay["ip"] = ent.ServerAddress()
		}
		config := map[string]interface{}{
			"relay": relay,
			"local": map[string]interface{}{"server": "127.0.0.1:" + strconv.Itoa(socksPort)},
		}
		data, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		spec.ConfigExport = string(data)
		path, err := b.writeTemp("tuic", ".json", data)
		if err != nil {
			return nil, err
		}
		spec.Args = []string{"-c", path}
		return spec, nil
	}

	// hysteria2
	spec := &ExternalSpec{Program: b.extraCorePath("hysteria2")}

	server := ent.ServerAddress()
	if hop := strings.TrimSpace(bb.Str("hopPort")); hop != "" {
		server = util.WrapIPv6Host(server) + ":" + hop
	} else {
		server = util.WrapIPv6Host(server) + ":" + strconv.Itoa(ent.ServerPort())
	}

	config := map[string]interface{}{
		"transport": map[string]interface{}{
			"type": "udp",
			"udp":  map[string]interface{}{"hopInterval": strconv.Itoa(bb.Int("hopInterval")) + "s"},
		},
		"server": server,
		"socks5": map[string]interface{}{
			"listen":     "127.0.0.1:" + strconv.Itoa(socksPort),
			"disableUDP": false,
		},
		"auth":     bb.Str("password"),
		"fastOpen": true,
		"lazy":     true,
	}
	bandwidth := map[string]interface{}{}
	if up := bb.Int("uploadMbps"); up > 0 {
		bandwidth["up"] = strconv.Itoa(up) + " mbps"
	}
	if down := bb.Int("downloadMbps"); down > 0 {
		bandwidth["down"] = strconv.Itoa(down) + " mbps"
	}
	config["bandwidth"] = bandwidth

	quic := map[string]interface{}{}
	if v := bb.Int64("streamReceiveWindow"); v > 0 {
		quic["initStreamReceiveWindow"] = v
	}
	if v := bb.Int64("connectionReceiveWindow"); v > 0 {
		quic["initConnReceiveWindow"] = v
	}
	if bb.Bool("disableMtuDiscovery") {
		quic["disablePathMTUDiscovery"] = true
	}
	config["quic"] = quic

	if obfs := bb.Str("obfsPassword"); obfs != "" {
		config["obfs"] = map[string]interface{}{
			"type":       "salamander",
			"salamander": map[string]interface{}{"password": obfs},
		}
	}

	tls := map[string]interface{}{}
	sniGen := bb.Str("sni")
	if sniGen == "" && !util.IsIPAddress(ent.ServerAddress()) {
		sniGen = ent.ServerAddress()
	}
	tls["sni"] = sniGen
	if bb.Bool("allowInsecure") {
		tls["insecure"] = true
	}
	if ca := strings.TrimSpace(bb.Str("caText")); ca != "" {
		path, err := b.writeTemp("hysteria2", ".crt", []byte(ca))
		if err != nil {
			return nil, err
		}
		tls["certificates"] = []interface{}{path}
	}
	config["tls"] = tls

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	spec.ConfigExport = string(data)
	path, err := b.writeTemp("hysteria2", ".json", data)
	if err != nil {
		return nil, err
	}
	spec.Args = []string{"-c", path}
	return spec, nil
}

func (b *builder) buildExternalCustom(ent *db.ProxyEntity, mappingPort, socksPort int) (*ExternalSpec, error) {
	bb := ent.Bean
	spec := &ExternalSpec{Program: b.extraCorePath(bb.Str("core"))}

	for _, arg := range bb.StrList("cmd") {
		spec.Args = append(spec.Args, replaceCustomVars(arg, ent, mappingPort, socksPort))
	}

	if cfg := strings.TrimSpace(bb.Str("cs")); cfg != "" {
		cfg = replaceCustomVars(cfg, ent, mappingPort, socksPort)

		suffix := ""
		if s := bb.Str("cs_suffix"); s != "" {
			suffix = "." + s
		} else {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(cfg), &obj) == nil && len(obj) > 0 {
				suffix = ".json"
			}
		}
		path, err := b.writeTemp("custom", suffix, []byte(cfg))
		if err != nil {
			return nil, err
		}
		for i := range spec.Args {
			spec.Args[i] = strings.ReplaceAll(spec.Args[i], "%config%", path)
		}
		spec.ConfigExport = cfg
	}
	return spec, nil
}

func replaceCustomVars(s string, ent *db.ProxyEntity, mappingPort, socksPort int) string {
	s = strings.ReplaceAll(s, "%mapping_port%", strconv.Itoa(mappingPort))
	s = strings.ReplaceAll(s, "%socks_port%", strconv.Itoa(socksPort))
	s = strings.ReplaceAll(s, "%server_addr%", ent.ServerAddress())
	s = strings.ReplaceAll(s, "%server_port%", strconv.Itoa(ent.ServerPort()))
	return s
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return "[" + strings.Trim(host, "[]") + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

func sameHost(a, b string) bool {
	return strings.Trim(a, "[]") == strings.Trim(b, "[]")
}

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, n)
	for i := range out {
		out[i] = letters[rand.Intn(len(letters))]
	}
	return string(out)
}
