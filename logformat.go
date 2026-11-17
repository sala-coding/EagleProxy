package main

import (
	"regexp"
	"strings"

	"eagle/internal/logstore"
)

// The panel's log types. Every line the app writes begins with one of these in
// brackets, and the type is the first thing a reader sees:
//
//	14:03:22.118 [节点] 已启动核心（pid 676）
//	14:03:25.470 [DNS]  system 侧解析失败：i/o timeout，已熔断并回退
const (
	logTypeStart   = "启动"
	logTypeExit    = "退出"
	logTypeNode    = "节点"
	logTypeTun     = "TUN"
	logTypeSP      = "系统代理"
	logTypeDNS     = "DNS"
	logTypeSub     = "订阅"
	logTypeRule    = "规则"
	logTypeSpeed   = "测速"
	logTypeUpdate  = "更新"
	logTypeHotkey  = "热键"
	logTypeSetting = "设置"
	logTypeNet     = "网络"
	logTypeCore    = "核心"
	logTypeApp     = "应用"
)

var logTypeWords = map[string]bool{
	logTypeStart: true, logTypeExit: true, logTypeNode: true, logTypeTun: true,
	logTypeSP: true, logTypeDNS: true, logTypeSub: true, logTypeRule: true,
	logTypeSpeed: true, logTypeUpdate: true, logTypeHotkey: true, logTypeSetting: true,
	logTypeNet: true, logTypeCore: true, logTypeApp: true,
}

// logTypeByTag maps the template table's source labels to the type words. The
// table's labels are the old "[vpn]"/"[dns]" prefixes; the panel speaks types
// now, and this is the one place the two are tied together.
var logTypeByTag = map[string]string{
	"vpn":         logTypeTun,
	"tunserv":     logTypeTun,
	"dns":         logTypeDNS,
	"sp":          logTypeSP,
	"shutdown":    logTypeExit,
	"update":      logTypeUpdate,
	"hotkey":      logTypeHotkey,
	"exitip":      logTypeNet,
	"traffic":     logTypeNet,
	"connections": logTypeNet,
	"speedtest":   logTypeSpeed,
	"test":        logTypeSpeed,
	"remember":    logTypeSetting,
	"datadir":     logTypeSetting,
	"theme":       logTypeSetting,
	"settings":    logTypeSetting,
	"warm":        logTypeStart,
	"route":       logTypeRule,
	"groups":      logTypeSub,
	"mode":        logTypeNode,
	"hotswitch":   logTypeNode,
	"app":         logTypeApp,
	"log":         logTypeApp,
	"core":        logTypeCore,
}

// logTypeByKey overrides the label where one label covers several subjects —
// the "app" tag carries the mode switches (TUN), the node actions (节点) and
// the subscriptions' notices alike.
var logTypeByKey = map[string]string{
	"core.rulesetMissing": logTypeRule,
	"core.tunDownCrash":   logTypeTun,
	"core.spDownCrash":    logTypeSP,
	"app.tunNeedAdmin":    logTypeTun,
	"app.waitingUAC":      logTypeTun,
	"app.tunNeedNode":     logTypeTun,
	"app.startingNode":    logTypeNode,
	"app.switchedTo":      logTypeNode,
	"app.switchFail":      logTypeNode,
	"app.groupEmpty":      logTypeSub,
	"groups.savedRestart": logTypeNode,
	"exitip.nodeDown":     logTypeNet,
}

// logTypeByKeyPrefix is the second override: keys whose subject is fixed by
// their family, whatever label the table gave them.
var logTypeByKeyPrefix = []struct{ prefix, typ string }{
	{"vpn.", logTypeTun},
	{"tunserv.", logTypeTun},
	{"sp.", logTypeSP},
	{"dns.", logTypeDNS},
	{"sub.", logTypeSub},
	{"speedtest.", logTypeSpeed},
	{"exitip.", logTypeNet},
	{"remember.", logTypeSetting},
	{"datadir.", logTypeSetting},
	{"hotswitch.", logTypeNode},
	{"hotkey.", logTypeHotkey},
	{"rulesets.", logTypeRule},
	{"route.", logTypeRule},
	{"traffic.", logTypeNet},
	{"update.", logTypeUpdate},
	{"shutdown.", logTypeExit},
	{"warm.", logTypeStart},
	{"mode.", logTypeNode},
	{"core.", logTypeNode},
}

// logTypeOf picks the type word for one table entry.
func logTypeOf(key, tag string) string {
	if t, ok := logTypeByKey[key]; ok {
		return t
	}
	for _, p := range logTypeByKeyPrefix {
		if strings.HasPrefix(key, p.prefix) {
			return p.typ
		}
	}
	if t, ok := logTypeByTag[tag]; ok {
		return t
	}
	return logTypeApp
}

// logOldPrefixRe is the label a raw line carries today. It is stripped before
// the type word is written, so a line never carries two labels.
var logOldPrefixRe = regexp.MustCompile(`^\[(vpn|tunserv|system proxy|shutdown|dns|update|hotkey|exitip|connections|traffic|theme|test|speedtest|sp|settings|route|remember|mode|hotswitch|groups|datadir|warm|port|app|log|core)\] `)

// logLeadingMarkers are the decorative marks older lines carried in front of
// their text — the ">>>"/"<<<" lifecycle arrows and the "##" section mark. They
// say nothing a reader needs and are removed with the old label.
var logLeadingMarkers = []string{"<<<<<<<< ", ">>> ", "<<< ", "## ", "提示: "}

// stripLogText removes the leading marks and the old label from a line's text,
// reporting the label it carried (for lines the template table does not know).
func stripLogText(s string) (rest, oldLabel string) {
	rest = s
	for {
		changed := false
		for _, m := range logLeadingMarkers {
			if strings.HasPrefix(rest, m) {
				rest = strings.TrimPrefix(rest, m)
				changed = true
				break
			}
		}
		if !changed {
			break
		}
	}
	if m := logOldPrefixRe.FindStringSubmatch(rest); m != nil {
		oldLabel = m[1]
		rest = rest[len(m[0]):]
	}
	return rest, oldLabel
}

// splitLogStamp takes the timestamp off a line, when it carries one, so the
// text can be rewritten without disturbing it.
func splitLogStamp(line string) (stamp, msg string) {
	if !logstore.HasStamp(line) {
		return "", line
	}
	return line[:13], line[13:]
}

// hasLogType reports whether a message already begins with one of the type
// words — the idempotency check every formatting path relies on.
func hasLogType(msg string) bool {
	if len(msg) < 4 || msg[0] != '[' {
		return false
	}
	i := strings.IndexByte(msg, ']')
	if i <= 1 || i+1 >= len(msg) || msg[i+1] != ' ' {
		return false
	}
	return logTypeWords[msg[1:i]]
}

// formatLogLineFor rewrites one line into the panel's shape:
//
//	[类型] 文本
//
// The timestamp, when the line already carries one, stays in front. category is
// the line's file/stream category: lines nothing recognises are core output
// ("core") or a test's own output (anything else), and that is what their type
// word falls back to. A line already in the new shape is returned unchanged.
func formatLogLineFor(category, line string) string {
	if line == "" {
		return line
	}
	stamp, msg := splitLogStamp(line)
	if hasLogType(msg) {
		return line
	}
	typ := ""
	if key, tag, _, ok := matchLogT(msg); ok {
		typ = logTypeOf(key, tag)
	}
	rest, oldLabel := stripLogText(msg)
	if typ == "" {
		if t, ok := logTypeByTag[oldLabel]; ok {
			typ = t
		}
	}
	if typ == "" {
		switch category {
		case "", "core":
			typ = logTypeCore
		case "app":
			typ = logTypeApp
		default:
			typ = logTypeSpeed
		}
	}
	// The type word already says "TUN"; a text that opens with it would repeat
	// it. ("TUN 模式已开启" reads as "[TUN] 模式已开启".)
	if typ == logTypeTun {
		rest = strings.TrimPrefix(rest, "TUN ")
	}
	return stamp + "[" + typ + "] " + rest
}

// LogTemplate is one entry of the translation table as the window reads it: the
// wording with {0} {1}… for the dynamic parts, and the dictionary key it stands
// for. The window matches a line's text against these and speaks the words its
// own dictionary holds.
type LogTemplate struct {
	Tpl string `json:"tpl"`
	Key string `json:"key"`
}

// LogTemplates exposes the translation table. The catch-alls ("{0}") are left
// out: they match everything and translate to the text itself, which is what a
// line with no entry shows anyway.
func (a *App) LogTemplates() []LogTemplate {
	out := make([]LogTemplate, 0, len(logTemplates))
	for _, e := range logTemplates {
		tpl, _ := stripLogText(e.tpl)
		if tpl == "" || tpl == "{0}" {
			continue
		}
		// The text the panel matches is the text the formatter writes: the TUN
		// type word replaces the one the wording opened with, so the exported
		// template has to drop it too.
		if logTypeOf(e.key, e.tag) == logTypeTun {
			tpl = strings.TrimPrefix(tpl, "TUN ")
		}
		out = append(out, LogTemplate{Tpl: tpl, Key: e.key})
	}
	return out
}
