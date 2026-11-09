# EagleProxy

[简体中文](README.md) | **English**

A Windows desktop proxy client built on [Wails](https://wails.io) (Go + Vue 3): subscription management, a node table, live connection state, and one-click switching between TUN and the system proxy. The core is [sing-box](https://github.com/SagerNet/sing-box).

## Download

Each release carries two assets — pick the one that matches what you are doing:

| File | Contents | Use |
|---|---|---|
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` (the core and wintun.dll) | First install: unzip and run |
| `Eagle-vX.Y.Z-windows.exe` | the program only | Upgrades in place; the in-app updater reads this one |

The program does not embed a core. It looks for `sing-box.exe` beside itself, in the `sing-box\` folder next to it, and around the data directory. Every `.exe` in that folder is listed in Settings → core path, so switching builds is a pick rather than a retyped path.

## Features

- **Subscription management**: import subscription links or paste node share links, with traffic quota and expiry parsed automatically; scheduled refresh; the action buttons appear on card hover
- **Node table**: latency / reference-speed columns are measured live and sortable; groups are colour-coded; nodes can be hot-swapped inside a group (selector, no core restart)
- **One-click modes**: system proxy / TUN switches in the sidebar; turning TUN off falls the nodes back to the local port so they keep running; the last mode and node are remembered and restored on the next launch
- **LAN sharing**: open the shared endpoint from the status bar and any device on the same network can use it; the icon counts the devices online and breathes while one is connected
- **Connections and logs**: a live connection list (individual connections can be dropped, long lists render on demand), plus a core log panel (categorised colouring, live stream, pause, filter, export) that opens itself once, on the session's first node start
- **Rule sets**: download and manage sing-box rule-sets (.srs) by country and category, with presets for China, Russia, the US and Iran; when the core reports a rule set the upstream does not publish (404), it is dropped from the next build and the start retried instead of crashing in a loop
- **DNS card**: visual editing of two resolver lists, latency measurement, automatic pick of the fastest DNS
- **UWP loopback**: one switch in Settings → TUN (one administrator prompt) lets Store apps reach the local proxy port
- **Hotkeys and tray**: global hotkeys (main window, mode toggles, node switching…); the tray menu stays in sync with the window state
- **Interface languages**: 简体中文 / English / Русский / فارسی, applied the moment one is chosen
- **Automatic updates**: detects the WebView2 runtime on startup (and offers to install it when missing); the About card checks GitHub for updates and, on confirmation, downloads, exits, swaps the build and restarts

## Requirements

- Windows 10 / 11 (the app offers to install the WebView2 runtime when it is missing)
- [Go](https://go.dev) 1.25+
- [Node.js](https://nodejs.org) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- sing-box.exe and wintun.dll: the release zip already carries them; a source build needs its own copy in `sing-box/`, or a core path picked in the settings

## Development

```cmd
wails dev
```

## Building

```cmd
:: simple package (no version number; the About card shows the git commit hash)
wails build

:: official release (injects the version number and the update-check repository)
.\build\release.ps1 -Version v1.0.15
```

The artefact lands in `build/bin/Eagle.exe`.

## Directory layout

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
sing-box/         the core and wintun.dll (not committed — see .gitignore)
dashboard/        the Clash API dashboard (yacd)
```

## Acknowledgements

- [sing-box](https://github.com/SagerNet/sing-box) — the proxy core
- [nekoray](https://github.com/MatsuriDayo/nekoray) — the reference for data structures and interaction vocabulary
- [Wails](https://wails.io) — the desktop framework

## License

Released under the [GPL-3.0](LICENSE) license.
