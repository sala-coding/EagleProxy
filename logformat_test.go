package main

import (
	"strings"
	"testing"
)

// TestFormatLogLineFor pins the panel's shape: one type word in brackets, the
// old label and the decorative marks gone, and a line already in the new shape
// left alone.
func TestFormatLogLineFor(t *testing.T) {
	cases := []struct{ name, category, in, want string }{
		{"old label becomes the type", "core",
			"[vpn] 停止 TUN 前端失败（放弃自动重启路径）: x",
			"[TUN] 停止 TUN 前端失败（放弃自动重启路径）: x"},
		{"marker removed", "core",
			">>> TUN 网卡已接管默认路由",
			"[TUN] 网卡已接管默认路由"},
		{"key overrides a broad label", "core",
			"已切换到 香港01",
			"[节点] 已切换到 香港01"},
		{"mode message typed TUN", "core",
			"TUN 模式已开启",
			"[TUN] 模式已开启"},
		{"unmatched notice falls back to 应用", "app",
			"节点已停止",
			"[应用] 节点已停止"},
		{"already typed", "core", "[DNS] x", "[DNS] x"},
		{"stamp kept in front", "core",
			"13:00:00.000 [vpn] x",
			"13:00:00.000 [TUN] x"},
		{"core output falls back to 核心", "core",
			"INFO[0001] started",
			"[核心] INFO[0001] started"},
		{"test output falls back to 测速", "speedtest-download",
			"raw line",
			"[测速] raw line"},
		{"hot-switch marker line", "core",
			"<<< 已切换到 [x]（核心未重启）",
			"[节点] 已切换到 [x]（核心未重启）"},
	}
	for _, c := range cases {
		if got := formatLogLineFor(c.category, c.in); got != c.want {
			t.Errorf("%s: formatLogLineFor(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestLogTemplatesSkipsCatchAlls pins the exported table: the catch-alls would
// match every line and translate to the text itself, so they are left out; the
// entries that do go out carry the text without its old label.
func TestLogTemplatesSkipsCatchAlls(t *testing.T) {
	seen := false
	tunSeen := false
	for _, e := range (&App{}).LogTemplates() {
		if e.Tpl == "{0}" || e.Tpl == "" {
			t.Fatalf("catch-all exported: %+v", e)
		}
		if strings.HasPrefix(e.Tpl, "[") || strings.HasPrefix(e.Tpl, ">>>") || strings.HasPrefix(e.Tpl, "<<<") {
			t.Fatalf("old mark left in template: %+v", e)
		}
		if e.Key == "vpn.routeTakenOver" {
			seen = true
		}
		if e.Key == "vpn.tunOn" {
			tunSeen = true
			if e.Tpl != "模式已开启" {
				t.Fatalf("TUN wording not deduplicated in the exported table: %+v", e)
			}
		}
	}
	if !seen {
		t.Fatal("vpn.routeTakenOver missing from the exported table")
	}
	if !tunSeen {
		t.Fatal("vpn.tunOn missing from the exported table")
	}
}
