<p align="center">
  <img src="build/appicon.png" width="116" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">
  Windows 桌面代理客户端 · 订阅管理 · 节点表格 · TUN / 系统代理一键切换
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center"><b>简体中文</b> · <a href="README_EN.md">English</a></p>

基于 [Wails](https://wails.io)（Go + Vue 3）构建，内核为 [sing-box](https://github.com/SagerNet/sing-box)。

## 下载

| 文件 | 内容 | 用途 |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\`（内核与 wintun.dll） | 首次安装，解压即用 |
| `Eagle-vX.Y.Z-windows.exe` | 仅程序本体 | 覆盖升级；程序内的自动更新读取的也是它 |

程序本体不内嵌内核：它会在自身目录、`sing-box\` 子目录与数据目录附近查找 `sing-box.exe`。设置 → 核心路径会列出该目录内所有内核，可直接点选切换。

## 功能

| 能力 | 说明 |
| --- | --- |
| 订阅管理 | 导入订阅链接或粘贴节点分享链接，自动解析流量配额与到期时间，定时刷新；操作按钮在卡片悬停时出现 |
| 节点表格 | 延迟 / 参考速度实时测速并排序，按组分色；组内热切换（selector）不重启核心 |
| 一键模式 | 系统代理 / TUN 侧栏开关；关闭 TUN 后节点回落到本地端口继续运行；记住上次模式与节点 |
| 局域网共享 | 状态栏开启共享端点，同网段设备填入该地址即可使用；图标显示在线设备数，有设备时呼吸提示 |
| 连接与日志 | 实时连接列表（可断开单条，长列表按需渲染）；日志面板分类着色、暂停、过滤、导出 |
| 规则集 | `.srs` 下载与管理，按国家/类别分栏，内置中国、俄罗斯、美国、伊朗预设；上游缺失的规则集自动跳过并重试 |
| DNS 管理 | 双解析列表可视化编辑、延迟实测、智能优选最快解析器 |
| UWP 回环 | 设置 → TUN 一个开关完成（需一次管理员授权），让应用商店应用连接本机代理端口 |
| 热键与托盘 | 全局热键（主窗口、模式开关、节点切换）；托盘菜单与窗口状态同步 |
| 多语言 | 简体中文 / English / Русский / فارسی，切换即时生效 |
| 自动更新 | 关于卡检查 GitHub 更新，确认后自动下载、替换并重启 |

## 快速开始

1. 在 [Releases](https://github.com/sala-coding/EagleProxy/releases/latest) 下载 `Eagle-v*-windows-full.zip`
2. 解压到任意目录，确保 `Eagle.exe` 与 `sing-box\` 同级
3. 运行 `Eagle.exe`，导入订阅或粘贴分享链接，从节点表格选一个节点启动

<details>
<summary><b>从源码构建</b></summary>

环境要求：

- Windows 10 / 11（WebView2 运行时缺失时程序启动会引导安装）
- [Go](https://go.dev) 1.25+
- [Node.js](https://nodejs.org) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- sing-box.exe 与 wintun.dll：使用发布页的 zip 时已自带；源码构建需自备，放 `sing-box/` 或在设置里指定

```cmd
wails dev

:: 简单打包（不带版本号，关于卡显示 git 提交哈希）
wails build

:: 正式发布（注入版本号与更新检查仓库）
.\build\release.ps1 -Version v1.0.15
```

产物在 `build/bin/Eagle.exe`。

</details>

<details>
<summary><b>目录结构</b></summary>

```
app_*.go          后端（Wails 绑定层）
internal/         核心 Go 逻辑（配置生成、订阅解析、TUN、系统代理…）
  core/             sing-box 进程生命周期、热切换、崩溃监督
  configbuilder/    sing-box 配置生成
  sub/ format/      订阅抓取解析、分享链接解析
  tunfast/ tunserv/ 网卡准备、路由捕获与提权服务
  dnsx/ dnsstub/    解析器与本地 DNS 转发
third_party/      energye/systray 的本地 fork（go.mod replace）
frontend/         Vue 3 + Vite 前端
build/            图标、安装器脚本、发布脚本
scripts/          发布脚本与各版本说明（notes/）
log/              日志（与程序同级，程序自动创建——不提交）
config*/          运行时数据目录（节点、订阅、设置——不提交）
sing-box/         内核与 wintun.dll（不提交）
dashboard/        Clash API 仪表盘（yacd）
```

</details>

## 致谢

- [sing-box](https://github.com/SagerNet/sing-box) — 代理内核
- [nekoray](https://github.com/MatsuriDayo/nekoray) — 数据结构与交互词汇的参考
- [Wails](https://wails.io) — 桌面框架

## 许可证

本项目以 [GPL-3.0](LICENSE) 发布。
