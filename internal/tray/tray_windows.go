//go:build windows

// Package tray provides the system tray icon and menu.
package tray

import (
	"runtime"
	"sync"
	"time"

	systray "github.com/energye/systray"
)

// showCoalesce is the window one tray gesture's messages fall inside: the two
// button-ups and the double-click arrive within a few dozen milliseconds of each other,
// and this is long enough to swallow the retries while staying invisible to a reader.
const showCoalesce = 350 * time.Millisecond

// showAt and showMu are the last time the show callback ran, for showOnce below.
var (
	showMu sync.Mutex
	showAt time.Time
)

// showOnce runs the show callback at most once per gesture (see the wiring at Setup).
func showOnce(fn func()) {
	if fn == nil {
		return
	}
	showMu.Lock()
	if !showAt.IsZero() && time.Since(showAt) < showCoalesce {
		showMu.Unlock()
		return
	}
	showAt = time.Now()
	showMu.Unlock()
	call(fn)
}

// Callbacks receives tray menu actions.
type Callbacks struct {
	OnShow  func()
	OnStart func()
	OnStop  func()
	OnQuit  func()
	// The state-changing presses: the two switches the rail carries, the "not this
	// one" pair that walks to the next node, and the outbound mode, which needs the
	// mode's name rather than a toggle because it has three answers.
	OnSystemProxy func()
	OnTun         func()
	OnNextProfile func()
	OnEgress      func(mode string)
	// The two answers, not actions: what is running now (painted into a disabled
	// row, for the reader who opened the tray rather than the window), and the
	// update check the About card asks.
	// OnUpdate opens nothing on failure by design — the tray has no window to
	// point at, and the toast the backend raises is the answer.
	OnDataDir func()
	OnUpdate  func()
}

// The two switch items, the status line and the three egress answers, kept so the
// backend can paint the state the rail would show without asking it again: modes
// into the checkboxes, the running node into a disabled row — a fact, not a press —
// and the outbound mode's own word onto whichever of its three answers applies.
var (
	itemProxy   *systray.MenuItem
	itemTun     *systray.MenuItem
	itemCurrent *systray.MenuItem
	itemEgress  map[string]*systray.MenuItem

	// The three facts the hover tooltip is composed from, kept so that either
	// painter (the node row or the mode switches) can rebuild the whole sentence:
	// "who is running" comes from SetCurrent, "which shape" from SetModes, and
	// the tooltip is the one line that says both without opening the menu.
	stateCurrent string
	stateProxy   bool
	stateTun     bool
	// stateEgress is the outbound mode last asked for. Kept for the same reason
	// the two above are: the first refresh lands before Setup has published the
	// menu (main() calls it the moment Setup returns, and Setup returns at
	// once), so the tick has to be remembered and painted when the items
	// appear — the egress submenu used to open with no tick at all until the
	// first node start or mode change.
	stateEgress string

	// trayMu guards the four items: Setup builds them on its own goroutine
	// while SetModes/SetCurrent read from any goroutine the backend calls
	// from — including in the same instant main() snugly follows Setup with
	// the first refresh.
	trayMu sync.RWMutex
)

// refreshTooltipLocked repaints the icon's hover line from the latest facts. The
// caller holds the write lock, because both painters do and the tooltip wants the
// two of them to agree about what it says. One line, not a paragraph: a hover
// popup is read at a glance or not at all.
func refreshTooltipLocked() {
	who := stateCurrent
	if who == "" {
		who = "未运行"
	}
	modes := " · 未代理"
	switch {
	case stateTun && stateProxy:
		modes = " · TUN + 系统代理"
	case stateTun:
		modes = " · TUN 模式"
	case stateProxy:
		modes = " · 系统代理"
	}
	systray.SetTooltip("Eagle — " + who + modes)
}

func setChecked(item *systray.MenuItem, on bool) {
	switch {
	case item == nil:
		return
	case on && !item.Checked():
		item.Check()
	case !on && item.Checked():
		item.Uncheck()
	}
}

// SetModes reflects the two switches and the outbound mode in the menu. The backend
// calls it whenever a mode changes — including a change made from the tray itself,
// from a hotkey, or from the window — so the ticks here and the lit buttons on the
// rail never disagree. Safe before the menu exists, and safe to call from any
// goroutine. A legacy "block" or an empty field reads as "rule", the word the
// config builder reads them as.
func SetModes(systemProxy, tun bool, egress string) {
	// The write lock, not the read one: this function WRITES — checked state and the
	// item titles below — and a read lock let two mode refreshes interleave their
	// check-then-act pairs, leaving a tick on the state the older of the two believed.
	trayMu.Lock()
	defer trayMu.Unlock()
	// Normalised and recorded before the nil check: Setup publishes the menu on
	// its own goroutine, so a refresh landing first found no items and used to
	// drop the mode entirely — the submenu then opened with nothing ticked.
	// A legacy "block" or an empty field reads as "rule", the word the config
	// builder reads them as.
	egress = egressMode(egress)
	stateEgress = egress
	stateProxy, stateTun = systemProxy, tun
	if itemEgress == nil {
		setChecked(itemProxy, systemProxy)
		setChecked(itemTun, tun)
		// The tooltip wants the facts even before the menu exists: the first
		// refresh can land here, and a tooltip that stays "未代理" over a TUN
		// icon is the one line in this package that would lie.
		refreshTooltipLocked()
		return
	}
	setChecked(itemProxy, systemProxy)
	setChecked(itemTun, tun)
	for mode, item := range itemEgress {
		setChecked(item, mode == egress)
	}
	refreshTooltipLocked()
}

// egressMode is the mode the menu can tick: the three it offers, with anything
// else (a retired "block", an empty field) reading as "rule".
func egressMode(egress string) string {
	if egress == "direct" || egress == "global" {
		return egress
	}
	return "rule"
}

// Setup registers the tray icon and menu.
//
// The tray runs on a dedicated OS thread with its own message loop. This is
// required, not cosmetic: Windows message queues are per-thread, and
// systray.Register only creates the window — the loop that dispatches its
// messages comes from systray.Run. Wails calls OnStartup on a fresh goroutine
// (see wails/internal/frontend/desktop/windows/frontend.go), so the window used
// to land on a thread that only got pumped by accident. When it wasn't pumped,
// the menu still popped up (the shell delivers the click notification while the
// queue is being peeked) but the WM_COMMAND produced by selecting an item was
// never dispatched, so every menu item silently did nothing.
func Setup(icon []byte, cb Callbacks) {
	go func() {
		runtime.LockOSThread()
		systray.Run(func() {
			systray.SetIcon(icon)
			systray.SetTitle("Eagle")
			systray.SetTooltip("Eagle")

			// The status line first: what is running now, before anything that
			// would change it. Disabled — a fact stated where the press would
			// say it — and filled in by SetCurrent as the core comes and goes.
			icCur := systray.AddMenuItem("当前节点：未运行", "此行仅显示运行状态，启动与切换在下面")
			icCur.Disable()
			systray.AddSeparator()
			mStart := systray.AddMenuItem("启动上次使用的节点", "用记住的节点，或列表里的第一个")
			mStop := systray.AddMenuItem("停止当前节点", "断开运行中的节点，流量恢复直连")
			systray.AddSeparator()
			icProxy := systray.AddMenuItem("系统代理模式", "经本地端口分流，软件跟随系统代理设置")
			icTun := systray.AddMenuItem("TUN 虚拟网卡", "虚拟网卡接管整机流量，无需软件设置")
			mNextNode := systray.AddMenuItem("切换到下一个节点", "切到列表中的下一个节点，当前模式保持不变")
			// The outbound mode, as its own corner of the menu: three answers, the
			// one in force checked, the press on another one putting it in force the
			// way the status bar's switch does (rebuild included — the backend's
			// cycleEgress knows how).
			egress := map[string]*systray.MenuItem{}
			parentEgress := systray.AddMenuItem("出站模式", "规则：档案说了算；直连：都直连；代理：都走代理")
			for _, mode := range []struct{ key, label, hint string }{
				{"rule", "规则分流（按路由档案）", "国内直连、国外按档案规则走代理"},
				{"direct", "全局直连（不走代理）", "所有流量直接连接，不经节点"},
				{"global", "全部走代理", "所有流量都经当前节点"},
			} {
				item := parentEgress.AddSubMenuItem(mode.label, mode.hint)
				egress[mode.key] = item
				key := mode.key
				item.Click(func() {
					go call(func() {
						if cb.OnEgress != nil {
							cb.OnEgress(key)
						}
					})
				})
			}
			systray.AddSeparator()
			mShow := systray.AddMenuItem("显示主窗口", "把主界面带回屏幕")
			mDataDir := systray.AddMenuItem("打开数据目录", "核心、规则集、日志都在这里")
			mUpdate := systray.AddMenuItem("检查软件更新", "到主窗口的“关于”卡查看结果")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("退出 Eagle", "退出并还原网络设置（保留的 TUN 前端按设置处理）")

			// Starting/stopping waits on the core, so keep it off the message
			// loop thread or the menu would freeze until it finishes. Every action
			// that touches the core goes the same way, which is why the next-node
			// switch is in this group rather than beside the switches above.
			mStart.Click(func() { go call(cb.OnStart) })
			mStop.Click(func() { go call(cb.OnStop) })
			icProxy.Click(func() { go call(cb.OnSystemProxy) })
			icTun.Click(func() { go call(cb.OnTun) })
			mNextNode.Click(func() { go call(cb.OnNextProfile) })
			mShow.Click(func() { call(cb.OnShow) })
			mDataDir.Click(func() { go call(cb.OnDataDir) })
			mUpdate.Click(func() { go call(cb.OnUpdate) })
			mQuit.Click(func() { go call(cb.OnQuit) })

			// Through showOnce, not call: Windows delivers one double click as two
			// button-ups and then the double-click message, and all three are wired to
			// OnShow — so a double click ran the action three times. Harmless while the
			// action is "show a window that is already up", and not harmless the moment
			// anything one-shot hangs off it, which is what a tray click collects as it
			// grows. The menu's own 打开主窗口 is left as it is: that one press is one
			// press.
			systray.SetOnClick(func(systray.IMenu) { showOnce(cb.OnShow) })
			systray.SetOnDClick(func(systray.IMenu) { showOnce(cb.OnShow) })

			// One locked publish, last: the handlers read these four from their own
			// goroutines (SetModes/SetCurrent, including the first refresh that
			// directly follows Setup), so the items go live only once fully built
			// and every Click is already registered.
			trayMu.Lock()
			itemCurrent = icCur
			itemProxy = icProxy
			itemTun = icTun
			itemEgress = egress
			// Painted from what was already asked for: the first refresh almost
			// always lands before this line (Setup returns immediately), and until
			// now that meant the menu opened with every tick missing.
			setChecked(icProxy, stateProxy)
			setChecked(icTun, stateTun)
			want := egressMode(stateEgress)
			for mode, item := range egress {
				setChecked(item, mode == want)
			}
			trayMu.Unlock()
		}, func() {
			go call(cb.OnQuit)
		})
	}()
}

// SetCurrent states the node that is running, in the disabled first row. The form
// carries the group as well as the node, because the node's name alone is what the
// main table shows — here the two names together is the answer to "who is in charge
// of this machine's traffic". An empty name is "none running", which is what the row
// says until something starts.
func SetCurrent(name string) {
	// A write lock: the title below is written here, and two writers under a shared
	// read lock tore the string the tray thread was reading for its menu.
	trayMu.Lock()
	defer trayMu.Unlock()
	// Recorded before the nil check: the first paint can arrive while Setup is
	// still building the menu, and the tooltip is composed from what was last
	// said, not from what the row could show at the time.
	stateCurrent = name
	if itemCurrent == nil {
		refreshTooltipLocked()
		return
	}
	if name == "" {
		name = "当前节点：未运行"
	} else {
		name = "当前节点：" + name
	}
	itemCurrent.SetTitle(name)
	refreshTooltipLocked()
}

// Quit removes the tray icon.
func Quit() { systray.Quit() }

func call(fn func()) {
	if fn != nil {
		fn()
	}
}
