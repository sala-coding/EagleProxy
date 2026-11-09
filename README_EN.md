<p align="center">
  <img src="build/appicon.png" width="116" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">
  A Windows desktop proxy client · subscriptions · node table · one-click TUN / system proxy
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center"><a href="README.md">简体中文</a> · <b>English</b></p>

Built on [Wails](https://wails.io) (Go + Vue 3); the core is [sing-box](https://github.com/SagerNet/sing-box).

## Download

| File | Contents | Use |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` (the core and wintun.dll) | First install: unzip and run |
| `Eagle-vX.Y.Z-windows.exe` | the program only | Upgrades in place; the in-app updater reads this one |

The program does not embed a core: it looks for `sing-box.exe` beside itself, in the `sing-box\` folder next to it, and around the data directory. Settings → core path lists every core in that folder, so switching builds is a pick rather than a retyped path.

## Features

| Capability | Detail |
| --- | --- |
| Subscriptions | Import subscription links or paste node share links; quota and expiry are parsed automatically and refreshed on a schedule |
| Node table | Latency / reference speed measured live and sortable, groups colour-coded; nodes are hot-swapped inside a group (selector) without a core restart |
| One-click modes | System proxy / TUN switches in the sidebar; turning TUN off falls the nodes back to the local port; the last mode and node are remembered |
| LAN sharing | Open the shared endpoint from the status bar and any device on the same network can use it; the icon counts devices and breathes while one is connected |
| Connections and logs | Live connection list (drop a single connection, long lists render on demand); log panel with category colouring, pause, filter and export |
| Rule sets | Download and manage `.srs` by country and category, with presets for China, Russia, the US and Iran; a rule set the upstream does not publish is skipped and the start retried |
| DNS | Visual editing of two resolver lists, latency measurement, automatic pick of the fastest |
| UWP loopback | One switch in Settings → TUN (one administrator prompt) lets Store apps reach the local proxy port |
| Hotkeys and tray | Global hotkeys (main window, mode toggles, node switching); the tray menu stays in sync with the window |
| Languages | 简体中文 / English / Русский / فارسی, applied the moment one is chosen |
| Updates | The About card checks GitHub and, on confirmation, downloads, swaps and restarts |

## Quick start

1. Download `Eagle-v*-windows-full.zip` from [Releases](https://github.com/sala-coding/EagleProxy/releases/latest)
2. Unzip anywhere, keeping `Eagle.exe` and `sing-box\` side by side
3. Run `Eagle.exe`, import a subscription or paste share links, then start a node from the table

<details>
<summary><b>Building from source</b></summary>

Requirements:

- Windows 10 / 11 (the app offers to install the WebView2 runtime when it is missing)
- [Go](https://go.dev) 1.25+
- [Node.js](https://nodejs.org) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- sing-box.exe and wintun.dll: the release zip carries them; a source build needs its own copy in `sing-box/`, or a core path picked in the settings

```cmd
wails dev

:: simple package (no version number; the About card shows the commit hash)
wails build

:: official release (injects the version number and the update-check repository)
.\build\release.ps1 -Version v1.0.15
```

The artefact lands in `build/bin/Eagle.exe`.

</details>

<details>
<summary><b>Directory layout</b></summary>

```
app_*.go          backend (the Wails binding layer)
internal/         core Go logic (config generation, subscription parsing, TUN, system proxy…)
  core/             sing-box process lifetime, hot switch, crash supervision
  configbuilder/    sing-box config generation
  sub/ format/      subscription fetch and parse, share-link parsing
  tunfast/ tunserv/ adapter preparation, route capture and the elevated service
  dnsx/ dnsstub/    resolvers and the local DNS forwarder
third_party/      a local fork of energye/systray (go.mod replace)
frontend/         Vue 3 + Vite front end
build/            icons, installer scripts, release scripts
scripts/          the release script and per-version notes (notes/)
log/              the logs (beside the program, created on launch — not committed)
config*/          runtime data directories (nodes, subscriptions, settings — not committed)
sing-box/         the core and wintun.dll (not committed)
dashboard/        the Clash API dashboard (yacd)
```

</details>

## Acknowledgements

- [sing-box](https://github.com/SagerNet/sing-box) — the proxy core
- [nekoray](https://github.com/MatsuriDayo/nekoray) — the reference for data structures and interaction vocabulary
- [Wails](https://wails.io) — the desktop framework

## License

Released under the [GPL-3.0](LICENSE) license.
