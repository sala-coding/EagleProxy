package main

import "testing"

// The table is the panel's dictionary in disguise: a template that stops
// matching its own wording is a line the panel can no longer translate. These
// pairs pin the matchers to the exact strings the back end writes — one per
// placeholder shape (none, one, many, greedy last) and one catch-all.
func TestMatchLogT(t *testing.T) {
	cases := []struct {
		line   string
		key    string
		params []string
	}{
		{"[hotkey] 未注册: Ctrl+Alt+T", "hotkey.refused", []string{"Ctrl+Alt+T"}},
		{"核心意外退出，正在自动重启…", "core.crashRestart", nil},
		{"[dns] 系统 侧解析路径已预热", "dns.warmed", []string{"系统"}},
		{">>> 定时更新订阅: 一毛机场", "sub.autoUpdate", []string{"一毛机场"}},
		{"定时更新失败 (一毛机场): Get \"https://x\": dial tcp: refused", "sub.autoUpdateFail",
			[]string{"一毛机场", `Get "https://x": dial tcp: refused`}},
		{"[warm] 已预热核心文件 12 个 / 33.5 MB（10ms）", "warm.coreFiles", []string{"12", "33.5", "10ms"}},
		{"[vpn] TUN 前端 (pid 1234) 已退出，但记录已换主 (pid 5678)，交由新的所有者处理",
			"vpn.frontExitOwnership", []string{"1234", "5678"}},
		{"[vpn] whatever new wording", "vpn.misc", []string{"whatever new wording"}},
		{"[sp] 某个没进表的词", "sp.misc", []string{"某个没进表的词"}},
		{"提示: 已启用 IPv6，VPN MTU 由 9000 提升为 14000（IPv6 要求网卡 MTU 不小于 1280）",
			"vpn.ipv6MtuRaised", []string{"9000", "14000", "1280"}},
	}
	for _, c := range cases {
		key, _, params, ok := matchLogT(c.line)
		if !ok {
			t.Errorf("no match: %q", c.line)
			continue
		}
		if key != c.key {
			t.Errorf("%q matched %q, want %q", c.line, key, c.key)
		}
		if len(params) != len(c.params) {
			t.Errorf("%q: got %d params (%q), want %d", c.line, len(params), params, len(c.params))
			continue
		}
		for i := range params {
			if params[i] != c.params[i] {
				t.Errorf("%q: param %d = %q, want %q", c.line, i, params[i], c.params[i])
			}
		}
	}
}

// The catch-alls sit last on purpose: a specific template must win over the
// generic "[vpn] {0}", or every wording collapses into the same key.
func TestMatchLogTOrder(t *testing.T) {
	key, _, _, ok := matchLogT("[vpn] TUN 前端未能启动，清理残留后重试")
	if !ok || key != "vpn.frontStartFailRetry" {
		t.Errorf("specific template lost to a catch-all: %q (ok=%v)", key, ok)
	}
}
