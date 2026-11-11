<p align="center">
  <img src="build/appicon.png" width="108" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">A Windows proxy client · subscriptions · node table · TUN / system proxy</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center"><a href="README.md">简体中文</a> · <b>English</b> · <a href="README_RU.md">Русский</a> · <a href="README_FA.md">فارسی</a></p>

Built on [Wails](https://wails.io) (Go + Vue 3) with [sing-box](https://github.com/SagerNet/sing-box) as the core. One executable, portable: its data lives in `config\` beside the program, with no registry keys and no service.

## Download

| File | Contents | Use |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` | First install: unzip and run |
| `Eagle-vX.Y.Z-windows.exe` | the program only | Upgrades in place; the in-app updater reads this one |

The core is not embedded: the program looks for `sing-box.exe` beside itself, in the `sing-box\` folder next to it, and around the data directory. Settings → core path lists what it finds, so switching builds is a pick.

## Features

| Capability | Detail |
| --- | --- |
| Subscriptions | Subscription links and share links, quota and expiry parsed automatically, refreshed on a schedule |
| Nodes | Latency and speed measured live and sortable, groups colour-coded; nodes are hot-swapped inside a group without restarting the core |
| Modes | System proxy / TUN from the sidebar, last node and mode remembered; the TUN stack follows the core's version |
| Networking | LAN sharing, UWP loopback, two DNS resolver lists with latency picking, `.srs` rule sets per country |
| Insight | Live connection list (drop a single connection), colour-coded log panel with pause, filter and export |
| Desktop | Global hotkeys and a tray menu, four interface languages, GitHub-based updates |

## Quick start

1. Download `Eagle-v*-windows-full.zip` from [Releases](https://github.com/sala-coding/EagleProxy/releases/latest)
2. Unzip anywhere, keeping `Eagle.exe` and `sing-box\` side by side
3. Run `Eagle.exe`, import a subscription or paste share links, then start a node from the table

<details>
<summary><b>Building from source</b></summary>

Needs Windows 10 / 11 (the app offers to install the WebView2 runtime when it is missing), [Go](https://go.dev) 1.25+, [Node.js](https://nodejs.org) 18+ and the [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2. The core is yours to provide: put `sing-box.exe` and `wintun.dll` in `sing-box/`, or point at them in the settings.

```cmd
wails dev
wails build                               :: package, no version number
.\build\release.ps1 -Version v1.0.17      :: official release, version stamped
```

The artefact lands in `build/bin/Eagle.exe`.

</details>

## Acknowledgements and licence

Core [sing-box](https://github.com/SagerNet/sing-box) · framework [Wails](https://wails.io) · data structures and interaction vocabulary after [nekoray](https://github.com/MatsuriDayo/nekoray). Released under [GPL-3.0](LICENSE).
