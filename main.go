package main

import (
	"context"
	"embed"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"eagle/internal/crashlog"
	"eagle/internal/tray"
	"eagle/internal/tunserv"
	"eagle/internal/util"
	"eagle/internal/webview2rt"
)

// tunElevatedFlag marks a process that was started by this app through a UAC
// prompt to run the tun mode internally.
//
// It used to also give such a copy a single instance lock of its own, so it
// could come up while the instance that launched it was still shutting down.
// That exception is what allowed two Eagle windows to exist at once — the
// elevated one held a lock nobody else was looking at — so it is gone: the
// handover now waits for the shared lock instead (see waitForInstanceLockRelease)
// and the elevated copy inherits it.
const tunElevatedFlag = "-tun-elevated"

// tunServeFlag starts this copy as the resident TUN front-end server (see
// internal/tunserv). The scheduled task runs the same executable with this
// flag, so the server always carries the code the UI carries and an update
// updates both sides at once. It must be recognised before anything else in
// main: the server is not the app, has no window, and must not take the
// single-instance lock the UI's copy treats as "one program only".
const tunServeFlag = "--tun-serve"

// instanceHandoverTimeout bounds the wait for the previous instance to exit
// during a TUN handover. Exiting means stopping the core and handing the system
// proxy back, so a second or two is normal; the ceiling is only there so a
// wedged instance cannot leave the user staring at a window that never appears.
const instanceHandoverTimeout = 8 * time.Second

func hasFlag(flag string) bool {
	for _, a := range os.Args[1:] {
		if a == flag {
			return true
		}
	}
	return false
}

// instanceID names the single instance lock.
//
// One id is shared by every copy of the program — the exe the user launches, a
// renamed copy of it, and the elevated copy the TUN handover starts — because
// "only one Eagle" is what the user is promised: a second launch is
// intercepted, the running window comes back, and the duplicate exits. Keying
// the id on the executable's name, as this used to, meant a renamed copy (or the
// dev build against the packaged one) counted as a different program and ran
// beside it. The dev build still gets an id of its own, for the reason in
// devIDSuffix.
func instanceID() string {
	return "eagle-single-instance" + devIDSuffix()
}

//go:embed all:frontend/dist
var assets embed.FS

// trayIcon is the tray's icon. It is read from frontend/public/fa/icon so the artwork
// sits with the rest of the icons, since the tray is the one icon Windows lets an
// application swap at runtime.
//
// The window's other icons are not runtime choices: wails build compiles
// build/windows/icon.ico into the exe as resource ID 3, and Wails loads the
// title bar, taskbar and desktop icon from that resource (winc.AppIconID) with
// no way to redirect it.
//
// The two icon files hold the same picture but are not the same file. This one
// keeps its frames as PNGs, which the tray's LoadImage reads and which keep the
// file small; the one that goes into the executable has its frames uncompressed,
// because the window framework asks for it with LoadIcon, and LoadIcon reads
// only that form — a PNG frame read that way comes out as a black square, which
// is what the taskbar button was showing. The build makes the second file from
// this one (see tools/mkico and build/release.ps1), so change the artwork here
// and let the build carry it over.
//
//go:embed frontend/public/fa/icon/eagle.ico
var trayIcon []byte

func main() {
	defer crashlog.Install("")

	// The resident server outranks everything in this file: no window, no
	// tray, no instance lock, no crash log that would confuse the UI's own.
	// Its only survivable state is files under the data directory, and a
	// crash of half the app's lifetime can be waited out by the next
	// heartbeat read (see ServerAlive's interval budget).
	if hasFlag(tunServeFlag) {
		// The scheduled task hands this process a console of its own, and a
		// console-subsystem build keeps it visible for the server's whole
		// lifetime — a black window over the desktop. Drop it: the server is
		// the only process attached, so the window closes, and everything the
		// server says goes to files anyway.
		util.DetachConsole()
		tunserv.RunServer()
		return
	}

	id := instanceID()
	// Handover: an elevated copy is started while the instance that asked for it
	// is still alive, and that instance holds the lock. Wait for it to let go —
	// which happens when it has finished exiting — so this copy takes over the
	// same lock instead of being turned away by it.
	if hasFlag(tunElevatedFlag) && !waitForInstanceLockRelease(id, instanceHandoverTimeout) {
		// Still held after the timeout, so the previous instance is likely
		// wedged rather than exiting. Fall back to a lock of our own: two
		// windows briefly is a better outcome than a UAC prompt that leads to a
		// program which never appears.
		id += "-tun"
	}

	app := NewApp()

	// The window draws in WebView2, and the runtime is supposed to already be on
	// the machine — "supposed to" being the part this check answers. A machine
	// without one used to be a double-click that did nothing: wails.Run answered
	// "runtime not found" to a window that did not exist to show it. Here the
	// reader is told and taken to Microsoft's download page instead; the install
	// is theirs to run (the bootstrapper there always carries the newest
	// runtime), and the program starts only when the runtime is found. The
	// server has no window to check for; its host already has one.
	if runtime.GOOS == "windows" && webview2rt.InstalledVersion() == "" {
		crashlog.Write("", "webview2: runtime not found")
		if webview2rt.OfferDownload("Eagle",
			"未检测到 WebView2 运行时，程序无法显示界面。\r\n\r\n"+
				"是否打开微软官方下载页面？\r\n"+
				"下载并安装其中的 Evergreen 引导程序后，重新运行本程序即可。") {
			webview2rt.OpenDownloadPage()
		}
		return
	}

	err := wails.Run(&options.App{
		// No caption text on purpose: the window is identified by its icon, and a
		// name repeated in the frame (and on the taskbar button) is chrome the
		// layout gets nothing from. The tray tooltip still spells the app out.
		Title:  "",
		Width:  1100,
		Height: 530,
		// Minimums equal to the default size: the window cannot be dragged
		// smaller. The layout needs it — below this the node table, the
		// subscription rail and the two floating bars start colliding — and a
		// user who shrinks it just ends up with a cramped view they cannot
		// undo by dragging back. Maximising is unaffected.
		MinWidth:  1100,
		MinHeight: 530,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:  &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		HideWindowOnClose: runtime.GOOS == "windows",
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			// A tray action can fail in a way the user has to see — "start" with
			// neither mode on, say — and the window may be hidden when it
			// happens, so a failure brings the window back rather than leaving
			// the click unanswered. The message itself comes from the backend as
			// an app:message toast.
			tray.Setup(trayIcon, tray.Callbacks{
				OnShow: app.ShowWindow,
				OnStart: func() {
					if err := app.StartLastProfile(); err != nil {
						app.ShowWindow()
					}
				},
				OnStop: func() { _ = app.StopProfile() },
				OnQuit: app.QuitApp,
				// The three state-changing presses. They call the same functions the
				// rail's buttons and the hotkeys do, so the tray cannot drift from the
				// window: whatever they report, the window shows.
				OnSystemProxy: app.toggleSystemProxy,
				OnTun:         app.toggleVPN,
				OnNextProfile: app.nextProfile,
				// The outbound mode from the tray: set, saved, and put in force the
				// way the status bar's own switch is — the cycle skips to the chosen
				// answer rather than stepping, so the checked one and the readout
				// agree. The egress:changed event and the toast come from there.
				OnEgress: func(mode string) {
					// Through the accessor every value read goes through (see
					// App.curSetting): the field itself is swapped by a reload, and a
					// direct read races that swap — and reads a string header that can
					// be half-written while the file's own copy is being replaced.
					s := app.curSetting()
					if s != nil && s.EgressMode == mode {
						return
					}
					app.setEgress(mode)
				},
				// The two answers, not actions: the data directory the app keeps its
				// truths in, and the update check the About card asks. The check's
				// result is told by the backend's own toast, since a tray click with
				// the window hidden must say something the reader can still see.
				OnDataDir: func() { _ = app.OpenDataDir() },
				OnUpdate: func() {
					go func() {
						// One at a time, and said so rather than dropped: this item does
						// not disable itself while a request is out, so a reader who
						// pressed it twice used to get two requests and two toasts for one
						// answer, plus a rate limit on GitHub's side if they kept at it.
						if !app.updateBusy.CompareAndSwap(false, true) {
							app.notifyMessage("正在检查更新，请稍候")
							return
						}
						defer app.updateBusy.Store(false)
						rel, err := app.CheckUpdate()
						app.ShowWindow()
						if err != nil {
							app.notifyMessage("检查更新失败: "+err.Error(), true)
							return
						}
						if rel == nil {
							app.notifyMessage("已是最新版本，没有可用的更新")
							return
						}
						app.notifyMessage("发现新版本 "+rel.TagName+"，详情见主窗口", true)
					}()
				},
			})
			app.applyHotkeys()
			// The TUN handover may have come up already running; sync the tray
			// animation with whatever state the startup path left behind.
			app.refreshTrayStatus()
		},
		// The webview is listening from here on, so messages raised while it was
		// still starting — the TUN handover's "now pick a node" — can be shown.
		OnDomReady: func(context.Context) {
			app.domReady()
		},
		OnShutdown: func(context.Context) {
			app.shutdown()
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: id,
			// A second launch never becomes a second window: Wails hands the
			// request to the running instance, which raises its window and says
			// why, and the duplicate exits before it can draw anything.
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.ShowWindow()
				app.notifySecondInstance()
			},
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		// Written down as well as printed. This is a GUI-subsystem binary, so stderr goes
		// nowhere a reader can look: a start that failed for want of the WebView2 runtime,
		// or on something Windows refused, was a double-click that did nothing at all —
		// no window, no tray, no note anywhere. crashlog puts the line beside the
		// executable (see crashlog.write), which is where someone looking will look.
		println("Error:", err.Error())
		crashlog.Write("", "wails.Run failed: "+err.Error())
	}
}
