<table>
<tr>
<td width="62%" valign="top">

<h1>EagleProxy</h1>

<p><sub>Windows 上的 sing-box 桌面客户端。<br>
订阅、节点、双模式、规则与 DNS —— 一屏之内，操作可预期。</sub></p>

<p>
<a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
<img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
<img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
<img src="https://img.shields.io/badge/ui-4%20languages-6e6a86?style=flat-square" alt="languages">
<a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p><b>简体中文</b> · <a href="README_EN.md">English</a> · <a href="README_RU.md">Русский</a> · <a href="README_FA.md">فارسی</a></p>

</td>
<td width="38%" valign="top" align="right">

<p><a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/DOWNLOAD-FULL%20ZIP-2f7d5a?style=for-the-badge" alt="download full"></a></p>

<p><a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/DOWNLOAD-EXE%20ONLY-0b6cbd?style=for-the-badge" alt="download exe"></a></p>

<p><sub>解压即用 · 不写注册表<br>不装服务 · 数据只留在自己的目录</sub></p>

</td>
</tr>
</table>

<img src=".github/assets/hero.png" width="100%" alt="EagleProxy 主界面">

<sub>　节点表格：协议、延迟、上下行速率与流量，一行一个节点；左侧一条轨道切换模式、面板与订阅。</sub>

---

## 六件事，一个窗口

<table>
<tr>
<td width="33%" valign="top">

<b>📥 订阅</b>

<p><sub>链接导入，或直接读入本地 sing-box / Clash 配置。配额与到期自动解析，也能再导出成 sing-box / Clash / 分享链接。</sub></p>

</td>
<td width="33%" valign="top">

<b>⚡ 热切换</b>

<p><sub>组内换节点不重启内核；TUN 预热，首次开启不用等冷启动。</sub></p>

</td>
<td width="34%" valign="top">

<b>🧭 双模式</b>

<p><sub>系统代理 / TUN，轨道上一键切换；绕过列表可自己写。</sub></p>

</td>
</tr>
<tr>
<td valign="top">

<b>📡 局域网共享</b>

<p><sub>一个端点给全网用，状态栏实时显示在线设备数。</sub></p>

</td>
<td valign="top">

<b>🧱 规则与 DNS</b>

<p><sub><code>.srs</code> 规则集按国家管理，双列表延迟自动优选。</sub></p>

</td>
<td valign="top">

<b>🔎 状态观测</b>

<p><sub>实时连接可一键断开，流量按上下行统计，日志按类别着色。</sub></p>

</td>
</tr>
</table>

<sub>另有：全局热键与托盘常驻 · 简体中文 / English / Русский / فارسی · 关于卡一键检查更新</sub>

---

## 界面

<img src=".github/assets/gallery.png" width="100%" alt="订阅、连接与设置面板">

<sub>　从上到下：订阅抽屉（配额进度条与到期日） · 实时连接（链路、速率与一键断开） · 设置面板（就地停靠，不遮住表格）。</sub>

---

## 三步开始

<table>
<tr>
<td width="52%" valign="top">

<b>① 下载附件</b>

<table>
<tr>
<td><code>Eagle-v*-windows-full.zip</code></td>
<td>首次安装</td>
<td><code>Eagle.exe</code> + <code>sing-box\</code> 内核目录</td>
</tr>
<tr>
<td><code>Eagle-v*-windows.exe</code></td>
<td>覆盖升级</td>
<td>仅程序本体，替换旧版即可</td>
</tr>
</table>

</td>
<td width="48%" valign="top">

<b>② 解压到任意目录</b>

<p><sub><code>Eagle.exe</code> 与 <code>sing-box\</code> 保持同级，双击运行。</sub></p>

<b>③ 导入并启动</b>

<p><sub>粘贴订阅链接或分享链接 → 在表格里选一个节点 → 点「启动」。</sub></p>

</td>
</tr>
</table>

> **系统要求**：Windows 10 / 11 x64。WebView2 缺失时启动会引导安装；TUN 模式首次开启需管理员授权。

---

## 内核

内核不内嵌。程序在自身目录、`sing-box\` 子目录与数据目录附近查找 `sing-box.exe`；设置 → 核心路径会列出找到的每一个内核，点选即切换 —— 多个版本可以并存，随时换。

## 声明

本项目仅是一个代理客户端程序，不提供、不分发任何节点或订阅服务；请遵守所在地区的法律法规。

---

<sub>内核 <a href="https://github.com/SagerNet/sing-box">sing-box</a> · 框架 <a href="https://wails.io">Wails</a> · 数据结构参考 <a href="https://github.com/MatsuriDayo/nekoray">nekoray</a> · 以 <a href="LICENSE">GPL-3.0</a> 发布</sub>
