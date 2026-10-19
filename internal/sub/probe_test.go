package sub

import (
	"encoding/base64"
	"strings"
	"testing"

	"eagle/internal/setting"
)

func probe(t *testing.T, body string) (int, string) {
	t.Helper()
	return probeWith(body, &setting.Setting{})
}

func TestProbeClashCountsImportableOnly(t *testing.T) {
	yamlBody := `port: 7890
proxies:
  - { name: n1, type: hysteria2, server: a, port: 1, password: p }
  - { name: n2, type: trojan, server: b, port: 443, password: p }
  - { name: n3, type: ss, server: c, port: 1, cipher: aes-128-gcm, password: p }
  - { name: n4, type: ssr, server: d, port: 1, cipher: aes-128-gcm, password: p }
  - { name: n5, type: juicity, server: e, port: 1, password: p }
proxy-groups:
  - { name: g, type: select, proxies: [n1] }`
	count, kind := probe(t, yamlBody)
	if count != 3 {
		t.Fatalf("count = %d, want 3 (ssr has no bean, juicity has no bean, group names are not proxies)", count)
	}
	if kind != "clash" {
		t.Fatalf("kind = %q, want clash", kind)
	}
}

func TestProbeLinkList(t *testing.T) {
	links := "hysteria2://pw@a:1/?insecure=1#n1\n" +
		"anytls://pw@b:443/?insecure=1&sni=b#n2\n" +
		"mystery://unparseable"
	count, kind := probe(t, links)
	if count != 2 {
		t.Fatalf("count = %d, want 2 (anytls is importable, the unknown scheme is not)", count)
	}
	if kind != "links" {
		t.Fatalf("kind = %q, want links", kind)
	}
}

func TestProbeBase64Subscription(t *testing.T) {
	links := "hysteria2://pw@a:1/?insecure=1#n1\ntrojan://pw@b:443#n2\nhysteria2://pw@c:1/?insecure=1#n3"
	encoded := base64.StdEncoding.EncodeToString([]byte(links))
	count, kind := probe(t, strings.TrimSpace(encoded))
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if kind != "links" {
		t.Fatalf("kind = %q, want links", kind)
	}
}

func TestProbeNonSubscription(t *testing.T) {
	if count, _ := probe(t, "<!DOCTYPE html><html><body>login page</body></html>"); count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if count, _ := probe(t, ""); count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}

func TestProbeClashInsideBase64(t *testing.T) {
	yamlBody := "proxies:\n  - { name: n1, type: anytls, server: a, port: 443, password: p }"
	encoded := base64.URLEncoding.EncodeToString([]byte(yamlBody))
	count, kind := probe(t, encoded)
	if count != 1 || kind != "clash" {
		t.Fatalf("count = %d kind = %q, want 1/clash", count, kind)
	}
}
