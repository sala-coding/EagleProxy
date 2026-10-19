# EagleProxy

[简体中文](README.md) | **English**

A Windows desktop proxy client built on [Wails](https://wails.io) (Go + Vue 3): subscription management, a node table, live connection state, and one-click switching between TUN and the system proxy. The core is [sing-box](https://github.com/SagerNet/sing-box).

## Features

- **Subscription management**: import subscription links or paste node share links, with traffic quota and expiry parsed automatically; scheduled refresh; the action buttons appear on card hover
- **Node table**: latency / reference-speed columns are measured live and sortable; groups are colour-coded; nodes can be hot-swapped inside a group (selector, no core restart)
- **One-click modes**: system proxy / TUN switches in the sidebar; turning TUN off falls the nodes back to the local port so they keep running; the last mode and node are remembered and restored on the next launch
- **Connections and logs**: a live connection list (individual connections can be dropped), plus a core log panel (categorised colouring, pause, filter, export)
- **Rule sets**: download and manage sing-box rule-sets (.srs), organised into categories
- **DNS card**: visual editing of two resolver lists, latency measurement, automatic pick of the fastest DNS
- **Hotkeys and tray**: global hotkeys (main window, mode toggles, node switching…); the tray menu stays in sync with the window state
- **Automatic updates**: detects the WebView2 runtime on startup (and offers to install it when missing); the About card checks GitHub for updates

## Requirements

- Windows 10 / 11 (the app offers to install the WebView2 runtime when it is missing)
- [Go](https://go.dev) 1.25+
- [Node.js](https://nodejs.org) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- sing-box.exe and wintun.dll (put them in the `sing-box/` directory, or point the app at the core path in the settings after it starts)

## Development

```cmd
wails dev
```

## Building

```cmd
:: simple package (no version number; the About card shows the git commit hash)
wails build

:: official release (injects the version number and the update-check repository)
.\build\release.ps1 -Version v1.0.0
```

The artefact lands in `build/bin/Eagle.exe`.

## Directory layout

```
app_*.go          backend (the Wails binding layer)
internal/         core Go logic (config generation, subscription parsing, TUN, system proxy…)
third_party/      a local fork of energye/systray (go.mod replace)
frontend/         Vue 3 + Vite front end
build/            icons, installer scripts, release scripts
config*/          runtime data directories (nodes, subscriptions, settings — not committed)
dashboard/        the Clash API dashboard (yacd)
```

## Acknowledgements

- [sing-box](https://github.com/SagerNet/sing-box) — the proxy core
- [nekoray](https://github.com/MatsuriDayo/nekoray) — the reference for data structures and interaction vocabulary
- [Wails](https://wails.io) — the desktop framework

## License

Released under the [GPL-3.0](LICENSE) license.
