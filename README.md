<p align="center">
  <img src="build/banner.svg" width="100%" alt="EagleProxy">
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center">
  <b>简体中文</b> · <a href="README_EN.md">English</a> · <a href="README_RU.md">Русский</a> · <a href="README_FA.md">فارسی</a>
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/DOWNLOAD-FULL%20ZIP-2f7d5a?style=for-the-badge" alt="download full"></a>
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/DOWNLOAD-EXE%20ONLY-0b6cbd?style=for-the-badge" alt="download exe"></a>
</p>

<p align="center">
  <sub>首次安装选 <b>full</b>，覆盖升级选 <b>exe</b> · 解压即用 · 不写注册表 · 不装服务</sub>
</p>

---

### 功能

<table>
<tr>
<td align="center" width="33%"><b>订阅</b><br><sub>链接导入<br>配额与到期自动解析</sub></td>
<td align="center" width="33%"><b>热切换</b><br><sub>组内换节点<br>不重启内核</sub></td>
<td align="center" width="33%"><b>双模式</b><br><sub>系统代理 / TUN<br>侧栏一键切换</sub></td>
</tr>
<tr><td colspan="3"><br><br></td></tr>
<tr>
<td align="center"><b>局域网共享</b><br><sub>同网段设备<br>填一个地址即可</sub></td>
<td align="center"><b>规则与 DNS</b><br><sub><code>.srs</code> 按国家管理<br>双列表 + 延迟优选</sub></td>
<td align="center"><b>观测</b><br><sub>实时连接列表<br>分类着色的日志</sub></td>
</tr>
</table>

<p align="center">
  <sub>另有：全局热键与托盘 · 四种界面语言 · 关于卡检查 GitHub 更新</sub>
</p>

### 快速开始

1. 从 [Releases](https://github.com/sala-coding/EagleProxy/releases/latest) 下载 `Eagle-v*-windows-full.zip`
2. 解压到任意目录，保持 `Eagle.exe` 与 `sing-box\` 同级
3. 运行 `Eagle.exe`，导入订阅或粘贴分享链接，在节点表格里选一个节点启动

### 内核

内核不内嵌，程序在自身目录、`sing-box\` 子目录与数据目录附近查找 `sing-box.exe`；设置 → 核心路径会列出找到的每一个内核，点选即可切换。数据只放在程序目录的 `config\`。

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

<p align="center">
  <sub>内核 <a href="https://github.com/SagerNet/sing-box">sing-box</a> · 框架 <a href="https://wails.io">Wails</a> · 数据结构参考 <a href="https://github.com/MatsuriDayo/nekoray">nekoray</a> · 以 <a href="LICENSE">GPL-3.0</a> 发布</sub>
</p>
