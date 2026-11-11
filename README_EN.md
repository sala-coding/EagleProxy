<p align="center">
  <img src="build/appicon.png" width="96" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">
  <sub>A Windows proxy client · subscriptions · node table · TUN / system proxy</sub>
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <b>English</b> · <a href="README_RU.md">Русский</a> · <a href="README_FA.md">فارسی</a>
</p>

<p align="center">
  <sub>One executable · data stays in <code>config\</code> beside it · no registry · no service</sub>
</p>

---

### Features

<table>
<tr>
<td align="center" width="33%"><b>Subscriptions</b><br><sub>links in<br>quota and expiry parsed</sub></td>
<td align="center" width="33%"><b>Hot switch</b><br><sub>change node in a group<br>no core restart</sub></td>
<td align="center" width="33%"><b>Two modes</b><br><sub>system proxy / TUN<br>one click on the rail</sub></td>
</tr>
<tr>
<td align="center"><b>LAN sharing</b><br><sub>one address<br>for the whole network</sub></td>
<td align="center"><b>Rules and DNS</b><br><sub><code>.srs</code> per country<br>two lists, fastest picked</sub></td>
<td align="center"><b>Insight</b><br><sub>live connection list<br>colour-coded logs</sub></td>
</tr>
</table>

<p align="center">
  <sub>Also: global hotkeys and a tray menu · four interface languages · updates checked on GitHub</sub>
</p>

### Download

| File | Contents | Use |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` | First install: unzip and run |
| `Eagle-vX.Y.Z-windows.exe` | the program only | Upgrades in place; the updater reads this one |

The core is not embedded: the program looks for `sing-box.exe` beside itself, in the `sing-box\` folder next to it, and around the data directory. Settings → core path lists what it finds, so switching builds is a pick.

### Quick start

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

<p align="center">
  <sub>Core <a href="https://github.com/SagerNet/sing-box">sing-box</a> · framework <a href="https://wails.io">Wails</a> · data structures after <a href="https://github.com/MatsuriDayo/nekoray">nekoray</a> · <a href="LICENSE">GPL-3.0</a></sub>
</p>
