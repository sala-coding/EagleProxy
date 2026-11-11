<p align="center">
  <img src="build/appicon.png" width="108" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">Windows 代理客户端 · 订阅 · 节点表格 · TUN / 系统代理</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center"><b>简体中文</b> · <a href="README_EN.md">English</a> · <a href="README_RU.md">Русский</a> · <a href="README_FA.md">فارسی</a></p>

基于 [Wails](https://wails.io)（Go + Vue 3），内核 [sing-box](https://github.com/SagerNet/sing-box)。单文件程序，绿色便携：数据放在程序目录下的 `config\`，不写注册表、不装服务。

## 下载

| 文件 | 内容 | 用途 |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` | 首次安装，解压即用 |
| `Eagle-vX.Y.Z-windows.exe` | 仅程序本体 | 覆盖升级；程序内自动更新读取此文件 |

内核不内嵌：程序在自身目录、`sing-box\` 子目录与数据目录附近查找 `sing-box.exe`，设置 → 核心路径可列出并直接切换。

## 功能

| 能力 | 说明 |
| --- | --- |
| 订阅 | 订阅链接与分享链接导入，自动解析流量与到期时间，定时刷新 |
| 节点 | 延迟 / 速度实测并排序，按组分色；组内热切换不重启内核 |
| 模式 | 系统代理 / TUN 侧栏切换，记住上次节点与模式；TUN 协议栈随内核版本自动适配 |
| 网络 | 局域网共享、UWP 回环、DNS 双列表编辑与延迟优选、`.srs` 规则集按国家管理 |
| 观察 | 实时连接列表（可断开单条）、分类着色的日志面板（暂停 / 过滤 / 导出） |
| 桌面 | 全局热键与托盘、四种界面语言、GitHub 自动更新 |

## 快速开始

1. 从 [Releases](https://github.com/sala-coding/EagleProxy/releases/latest) 下载 `Eagle-v*-windows-full.zip`
2. 解压到任意目录，保持 `Eagle.exe` 与 `sing-box\` 同级
3. 运行 `Eagle.exe`，导入订阅或粘贴分享链接，在节点表格里选一个节点启动

<details>
<summary><b>从源码构建</b></summary>

需要 Windows 10 / 11（缺 WebView2 时程序会引导安装）、[Go](https://go.dev) 1.25+、[Node.js](https://nodejs.org) 18+、[Wails CLI](https://wails.io/docs/gettingstarted/installation) v2。内核需自备：把 `sing-box.exe` 与 `wintun.dll` 放进 `sing-box/`，或在设置里指定路径。

```cmd
wails dev
wails build                               :: 打包，不带版本号
.\build\release.ps1 -Version v1.0.17      :: 正式发布，注入版本号
```

产物：`build/bin/Eagle.exe`。

</details>

## 致谢与许可

内核 [sing-box](https://github.com/SagerNet/sing-box) · 桌面框架 [Wails](https://wails.io) · 数据结构与交互词汇参考 [nekoray](https://github.com/MatsuriDayo/nekoray)。本项目以 [GPL-3.0](LICENSE) 发布。
