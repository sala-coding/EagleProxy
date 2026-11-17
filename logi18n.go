package main

import (
	"regexp"
	"strings"
)

// The log panel's wording table. Every line the app writes is matched here —
// the FINAL line, values already substituted, {0} {1}… standing for the dynamic
// parts — and the entry says two things: the dictionary key the front end
// speaks for another language, and the type word the line is filed under (see
// logformat.go). The front end reads the same table through LogTemplates() and
// matches the plain text itself, so a language switch repaints the lines
// already on screen.
//
// A line nothing matches is passed through unchanged: stale wording reads as
// itself rather than breaking the stream. Templates are matched anchored and in
// order — the generic catch-alls sit at the end.
//
// The tag is the "[vpn]"/"[dns]"-style source label the raw lines used to
// carry; logTypeOf turns it (and the key) into the type word the panel shows.

type logTpl struct {
	tpl string // the exact wording, {0} {1}… for the dynamic parts
	key string // dictionary key, spoken as "log." + key
	tag string // source label; empty renders bare
}

var logTemplates = []logTpl{
	// --- hotkeys ---
	{"[hotkey] {0} 与 {1} 使用了同一组合 {2}，只有后者生效", "hotkey.clash", "hotkey"},
	{"[hotkey] 未注册: {0}", "hotkey.refused", "hotkey"},
	{"[hotkey] 切换分组失败: {0}", "hotkey.groupFail", "hotkey"},
	{"热键冲突：以下组合被两个动作共用，仅后者生效：{0}", "hotkey.clashNote", "app"},
	{"热键注册失败：{0}，组合可能已被系统或其他程序占用", "hotkey.registerFail", "app"},
	{"停止节点失败：{0}", "hotkey.stopFail", "app"},
	{"启用节点失败：{0}", "hotkey.startFail", "app"},
	{"保存出站模式失败：{0}", "hotkey.egressSaveFail", "app"},
	{"出站模式已设为{0}", "hotkey.egressSet", "app"},
	{"切换出站模式失败：{0}", "hotkey.egressFail", "app"},
	{"切换路由失败：{0}", "hotkey.routeFail", "app"},

	// --- groups / rule-sets / routing ---
	{"[groups] 无法修正当前分组: {0}", "groups.fixFail", "groups"},
	{"配置已保存，当前节点正在运行，重新启动该节点后生效", "groups.savedRestart", "app"},
	{"规则集已变更，但核心未能重建：{0}", "rulesets.rebuildFail", "app"},
	{"规则集已生效，核心已按新的规则重建", "rulesets.applied", "app"},
	{"[route] {0}: 仅解析器变化，转发器已即时切换（不重启核心）", "route.resolverOnly", "route"},

	// --- exit ip ---
	{"[exitip] 没有服务回答，角落保持上一次的地址", "exitip.noAnswer", "exitip"},
	{"[exitip] 预热完成: {0}", "exitip.prewarm", "exitip"},
	{"当前节点不可用：出口探测连续失败，请更换节点", "exitip.nodeDown", "app"},

	// --- dns ---
	{"[dns] 无法记录常驻服务状态: {0}", "dns.stateSaveFail", "dns"},
	{"[dns] 退出时无法记录常驻服务状态: {0}", "dns.stateSaveFailExit", "dns"},
	{"[dns] 已收回上次会话遗留的解析器接管", "dns.reclaimed", "dns"},
	{"[dns] 解析器接管残留未能收回（无法确定程序路径）: {0}", "dns.reclaimFailNoPath", "dns"},
	{"[dns] 解析器接管残留：已请求常驻服务收回", "dns.reclaimRequested", "dns"},
	{"[dns] 解析器接管残留未能收回（本进程无权限且未安装常驻服务）——机器的 DNS 可能仍指向本程序", "dns.reclaimFailNoPerm", "dns"},
	{"[dns] 无法记录程序存活心跳: {0}", "dns.heartbeatFail", "dns"},
	{"[dns] {0} 侧解析路径已预热", "dns.warmed", "dns"},
	{"[vpn] 系统 DNS 接管尚未生效，跳过缓存预热", "dns.skipWarmup", "vpn"},
	{"[vpn] 接管尚未答出探测域名，仍按旧例唤醒系统的连通性探测", "dns.probeFallback", "vpn"},

	// --- core lifecycle / auto-restart ---
	{"规则集 {0} 不存在，已跳过该规则并重试", "core.rulesetMissing", "app"},
	{"自动重启失败：{0}", "core.restartFail", "app"},
	{"核心反复退出（{0} 次），已停止自动重启", "core.restartGaveUp", "app"},
	{"TUN 模式已关闭：核心反复退出，当前直连，可重新开启", "core.tunDownCrash", "app"},
	{"系统代理已关闭：核心反复退出，当前直连，可重新开启", "core.spDownCrash", "app"},
	{"核心意外退出，正在自动重启…", "core.crashRestart", "app"},
	{"[vpn] 停止 TUN 前端失败（放弃自动重启路径）: {0}", "vpn.stopFrontFailGiveup", "vpn"},
	{"[sp] 清除系统代理失败（放弃自动重启路径）: {0}", "sp.clearFailGiveup", "sp"},

	// --- subscriptions ---
	{">>> 定时更新订阅: {0}", "sub.autoUpdate", ""},
	{"定时更新失败 ({0}): {1}", "sub.autoUpdateFail", ""},
	{"<<<<<<<< 运行中的节点在清空后保留: {0}", "sub.keepRunning", ""},
	{"<<<<<<<< 走中切换到的运行节点在清空后保留: {0}", "sub.keepHotSwitched", ""},
	{"<<<<<<<< 运行中的节点已不在订阅里，本次保留: {0}", "sub.keepRemoved", ""},
	{"<<<<<<<< 走中切换到的运行节点已不在订阅里，本次保留: {0}", "sub.keepRemovedHotSwitched", ""},

	// --- shutdown ---
	{"[shutdown] 系统代理清理失败: {0}", "shutdown.spCleanFail", "shutdown"},
	{"[shutdown] 核心停止未在限期内确认: {0}", "shutdown.coreStopTimeout", "shutdown"},
	{"[shutdown] stage: tun front-end kept (下下次启动复用)", "shutdown.frontKept", "shutdown"},

	// --- update ---
	{"[update] TUN 前端未能停止（替换程序前需要它退出）: {0}", "update.frontStopFail", "update"},
	{"[update] 常驻服务未退出，替换程序可能失败（脚本会重试）", "update.residentBusy", "update"},

	// --- traffic / speedtest / remember / datadir / warm ---
	{"[traffic] 开始轮询 Clash API (127.0.0.1:{0})", "traffic.pollStart", "traffic"},
	{"[traffic] Clash API 查询失败: {0}", "traffic.queryFail", "traffic"},
	{"[speedtest] 上一轮测速未在 15 秒内收尾，本轮照常开始", "speedtest.staleRun", "speedtest"},
	{"[remember] {0}", "remember.fail", "remember"},
	{"[datadir] 回退到原目录失败: {0}", "datadir.rollbackFail", "datadir"},
	{"[warm] 已预热核心文件 {0} 个 / {1} MB（{2}）", "warm.coreFiles", "warm"},

	// --- system proxy ---
	{"[system proxy] 切向 TUN 前的代理清理失败: {0}", "sp.cleanBeforeTunFail", "sp"},
	{"[system proxy] 核心停止后的代理清理失败: {0}", "sp.cleanAfterStopFail", "sp"},
	{"[system proxy] TUN 开启失败后的回退也未完成: {0}", "sp.rollbackIncomplete", "sp"},
	{"切换系统代理失败后的回退清理也未完成: {0}", "sp.rollbackCleanupIncomplete", "sp"},
	{"[system proxy] 无法还原异常退出前的原始代理设置: {0}", "sp.restoreFail", "sp"},
	{"[system proxy] 已还原异常退出前的原始代理设置", "sp.restored", "sp"},
	{"系统代理开启失败：{0}", "sp.onFail", "app"},
	{"系统代理已开启，节点已重启生效", "sp.onRestarted", "app"},
	{"系统代理开启失败：节点未能重启，当前直连", "sp.onFailNoRestart", "app"},
	{"系统代理关闭失败：{0}", "sp.offFail", "app"},
	{"系统代理已开启", "sp.on", "app"},
	{"系统代理写入失败：{0}", "sp.writeFail", "app"},

	// --- TUN front-end / resident server ---
	{"[vpn] TUN 前端配置已变更（本地端口或鉴权），重建前端", "vpn.frontRebuild", "vpn"},
	{"[vpn] TUN 前端未能启动，清理残留后重试", "vpn.frontStartFailRetry", "vpn"},
	{"[tunserv] server 启动失败，回落直启: {0}", "tunserv.serverFailFallback", "tunserv"},
	{"<<< 程序正在退出，收回刚启动的 TUN 前端 (pid {0})", "vpn.exitReclaimFront", ""},
	{"<<< 程序正在退出，保留 TUN 前端 (pid {0}，下下次启动复用)", "vpn.exitKeepFront", ""},
	{"[vpn] TUN 前端 (pid {0}) 已退出，但记录已换主 (pid {1})，交由新的所有者处理", "vpn.frontExitOwnership", "vpn"},
	{"[vpn] TUN 前端 (pid {0}) 已退出，TUN 模式已关闭，不再重启", "vpn.frontExitTunOff", "vpn"},
	{"[vpn] TUN 前端意外退出 (pid {0})，清理残留网卡后自动重启一次", "vpn.frontCrashRestart", "vpn"},
	{"[vpn] 网卡 DNS 未能{0}（交由常驻服务接管）: {1}", "vpn.adapterDnsDelegated", "vpn"},
	{"[tunserv] 常驻服务来自旧版本，正在重启", "tunserv.oldVersionRestart", "tunserv"},
	{"[tunserv] 旧版本常驻服务未退出，本次仍由它承担（重启程序或系统后生效）", "tunserv.oldVersionBusy", "tunserv"},
	{"[tunserv] 常驻服务已就绪（等在途探测时出现）", "tunserv.readyLate", "tunserv"},
	{"[tunserv] 任务安装未完成: {0}", "tunserv.taskInstallFail", "tunserv"},
	{"[tunserv] 安装后未查到任务，回落直启", "tunserv.taskMissing", "tunserv"},
	{"[tunserv] 计划任务已安装（本次授权一次，后续 TUN 启动不再弹 UAC）", "tunserv.taskInstalled", "tunserv"},
	{"[tunserv] 常驻服务已就绪", "tunserv.ready", "tunserv"},
	{"[tunserv] 常驻服务进程未出现（任务可能指向已不存在的程序），本次使用直启", "tunserv.processMissing", "tunserv"},
	{"[tunserv] 常驻服务未在 10 秒内就绪，本次使用直启（网卡 DNS 由本进程接管）", "tunserv.notReady10s", "tunserv"},
	{"[tunserv] 启动预热: 常驻服务已在运行", "tunserv.prewarmRunning", "tunserv"},
	{"[tunserv] 启动预热: 常驻服务来自旧版本，正在重启", "tunserv.prewarmOldVersion", "tunserv"},
	{"[tunserv] 启动预热: 常驻服务已就绪，后续 TUN 启动免冷等待", "tunserv.prewarmReady", "tunserv"},
	{"[tunserv] 启动预热: 常驻服务未就绪，TUN 启动时再试", "tunserv.prewarmNotReady", "tunserv"},
	{"[tunserv] 启动预热: 已请求预先创建 TUN 网卡（首次开启 TUN 免建卡等待）", "tunserv.prewarmAdapter", "tunserv"},
	{"[tunserv] 程序正在退出，收回刚启动的前端 (pid {0})", "tunserv.exitReclaimFront", "tunserv"},
	{"[tunserv] 拆除前无法记录状态: {0}", "tunserv.stateSaveFail", "tunserv"},
	{"<<< TUN 前端闲置超时（无节点在使用），前端进程退出；网卡保留，下次开启 TUN 秒开", "vpn.frontIdleKeepAdapter", ""},
	{"<<< TUN 前端闲置超时（TUN 已关闭），前端进程退出，网卡一并移除", "vpn.frontIdleRemoveAdapter", ""},
	{"[tunserv] pid 写入失败: {0}", "tunserv.pidWriteFail", "tunserv"},
	{"<<< 已结束 {0} 个残留的 TUN 前端进程", "vpn.leftoverFrontsKilled", ""},
	{"<<< 已清理 {0} 个异常的 TUN 网卡", "vpn.badAdaptersCleaned", ""},
	{"[tunserv] 复用前先停掉旧配置的前端 (pid {0})", "tunserv.reuseRestartOld", "tunserv"},
	{"提示: 已启用 IPv6，VPN MTU 由 {0} 提升为 {1}（IPv6 要求网卡 MTU 不小于 {2}）", "vpn.ipv6MtuRaised", ""},
	{"[tunserv] 拆除未在时限内完成，改走直杀", "tunserv.teardownTimeoutKill", "tunserv"},
	{"[tunserv] 拆除命令未送达，改走逐条停止", "tunserv.teardownCmdMiss", "tunserv"},
	{"[tunserv] server stop 超时，改走直杀", "tunserv.serverStopTimeout", "tunserv"},
	{"[vpn] front-end stop failed (可能需要管理员权限): {0}", "vpn.frontStopFailAdmin", "vpn"},
	{"[tunserv] 请求移除 TUN 网卡失败: {0}", "tunserv.adapterRemoveFail", "tunserv"},
	{"<<< 已请求常驻服务移除 TUN 网卡: {0}", "vpn.adapterRemoveDelegated", ""},
	{"<<< 已移除 TUN 网卡: {0}", "vpn.adapterRemoved", ""},
	{"<<< TUN 网卡未能移除（需要管理员权限），下次开启 TUN 时由常驻服务重建", "vpn.adapterRemoveFailAdmin", ""},
	{">>> TUN 网卡已接管默认路由", "vpn.routeTakenOver", ""},
	{">>> TUN 网卡已接管默认路由（后台补上）", "vpn.routeTakenOverLate", ""},
	{"[vpn] 适配器 DNS 未就位（接管路径）: {0}", "vpn.adapterDnsNotReady", "vpn"},
	{"[vpn] 适配器 DNS 未就位（后台补上）: {0}", "vpn.adapterDnsNotReadyLate", "vpn"},
	{"[vpn] TUN 路由接管失败: {0}", "vpn.routeTakeoverFail", "vpn"},
	{"[vpn] 路由接管重试停止：TUN 前端已由守护重启，交由其完成接管", "vpn.routeRetryHandover", "vpn"},
	{"[vpn] TUN 接管已被关闭取消（不计为失败）", "vpn.takeoverCancelled", "vpn"},
	{"[vpn] TUN 接管仍在等待前端就绪（不计为失败）", "vpn.takeoverWaiting", "vpn"},
	{"<<< TUN 网卡已交还默认路由（适配器保留）", "vpn.routeReturned", ""},
	{"[vpn] 交还默认路由失败: {0}", "vpn.routeReturnFail", "vpn"},
	{"[vpn] TUN 前端记录已换主（pid {0} 已不是当前记录），清理交由新的所有者", "vpn.frontOwnershipMoved", "vpn"},
	{"<<< 已切换到 [{0}]（核心未重启）", "hotswitch.switched", ""},

	// --- TUN / mode switching (the bar, the tray, the hotkeys, the restore) ---
	{"TUN 模式开启失败：需要管理员权限，当前直连", "app.tunNeedAdmin", "app"},
	{"正在等待管理员授权，确认后自动接管 TUN…", "app.waitingUAC", "app"},
	{"请先选择并启动一个节点，再开启 TUN 模式", "app.tunNeedNode", "app"},
	{"[vpn] 已取消待处理的 TUN 请求：本次运行没有管理员权限", "vpn.pendingCancelled", "vpn"},
	{"TUN 模式已开启，节点已重启生效", "vpn.tunOnRestarted", "app"},
	{"TUN 模式已关闭，节点继续运行", "vpn.tunOffNodeUp", "app"},
	{"TUN 模式已关闭，但节点重启失败：{0}，当前直连", "vpn.tunOffRestartFail", "app"},
	{"TUN 模式已关闭，节点继续运行但不经过 TUN", "vpn.tunOffNoTun", "app"},
	{"TUN 模式已开启", "vpn.tunOn", "app"},
	{"TUN 开启失败：{0}", "vpn.tunOnFail", "app"},
	{"正在启动节点…", "app.startingNode", "app"},
	{"TUN 模式开启失败：已回退到系统代理", "vpn.tunOnFailFallbackSp", "app"},
	{"TUN 模式开启失败：节点未能启动，当前直连", "vpn.tunOnFailNoNode", "app"},
	{"TUN 模式已恢复", "vpn.tunRestored", "app"},
	{"上次的模式未恢复：请先选择并启动一个节点", "remember.needNode", "app"},
	{"正在恢复上次的节点…", "remember.restoringNode", "app"},
	{"恢复上次模式失败：{0}", "remember.restoreFail", "app"},
	{"正在恢复上次的 TUN 模式…", "remember.restoringTun", "app"},
	{"[mode] 生成当前模式的配置失败: {0}", "mode.configFail", "mode"},

	// --- tray / window ---
	{"已有实例在运行，本次启动已交还该实例", "app.secondInstance", "app"},
	{"当前分组没有节点", "app.groupEmpty", "app"},
	{"切换节点失败：{0}", "app.switchFail", "app"},
	{"已切换到 {0}", "app.switchedTo", "app"},

	// --- catch-alls: anything the specific templates missed ---
	{"[vpn] {0}", "vpn.misc", "vpn"},
	{"[sp] {0}", "sp.misc", "sp"},
	{"[dns] {0}", "dns.misc", "dns"},
	{"[app] {0}", "app.misc", "app"},
}

type logCompiled struct {
	re  *regexp.Regexp
	key string
	tag string
}

var logCompiledPatterns []logCompiled

func init() {
	for _, e := range logTemplates {
		logCompiledPatterns = append(logCompiledPatterns, logCompiled{
			re:  compileLogTpl(e.tpl),
			key: e.key,
			tag: e.tag,
		})
	}
}

// compileLogTpl turns a template into an anchored regexp: every literal part is
// quoted, every {n} — any digit — becomes a capture group, lazy but the last,
// so the final value (usually an error's whole text) is taken whole.
var logTplPlaceholders = regexp.MustCompile(`\{\d+\}`)

func compileLogTpl(tpl string) *regexp.Regexp {
	idx := logTplPlaceholders.FindAllStringIndex(tpl, -1)
	if len(idx) == 0 {
		return regexp.MustCompile("^" + regexp.QuoteMeta(tpl) + "$")
	}
	var b strings.Builder
	b.WriteString("^")
	last := 0
	for i, pair := range idx {
		b.WriteString(regexp.QuoteMeta(tpl[last:pair[0]]))
		if i == len(idx)-1 {
			b.WriteString("(.*)")
		} else {
			b.WriteString("(.*?)")
		}
		last = pair[1]
	}
	b.WriteString(regexp.QuoteMeta(tpl[last:]))
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// matchLogT matches one raw line against the table and reports the entry it
// stands for. The multi-placeholder templates are turned into plain {0} form
// here: templates with {1}… are matched by rewriting nothing — the captures
// come back in order and the front end fills its own slots the same way.
func matchLogT(line string) (key, tag string, params []string, ok bool) {
	for _, p := range logCompiledPatterns {
		m := p.re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return p.key, p.tag, m[1:], true
	}
	return "", "", nil, false
}
