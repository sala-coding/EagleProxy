# EagleProxy

基于 [Wails](https://wails.io)（Go + Vue 3）的 Windows 桌面代理客户端：订阅管理、节点表格、连接状态、TUN / 系统代理一键切换。内核为 [sing-box](https://github.com/SagerNet/sing-box)。

## 功能

- **订阅管理**：导入订阅链接或粘贴节点分享链接，自动解析用量（流量配额、到期时间）；定时自动更新；订阅卡片悬停即出操作按钮
- **节点表格**：延迟 / 参考速度列实时测速并排序；按组分色；组内热切换（selector，切换不重启核心）
- **一键模式**：系统代理 / TUN 侧栏开关；关闭 TUN 后节点自动切回本地端口继续运行；支持记住上次模式与节点，下次启动自动恢复
- **连接与日志**：实时连接列表（可断开单条）、核心日志面板（分类着色、暂停、过滤、导出）
- **规则集**：sing-box rule-set（.srs）下载与管理，分类分栏
- **DNS 管理卡**：双解析列表可视化编辑、延迟实测、智能优选最快 DNS
- **热键与托盘**：全局热键（主窗口、模式开关、节点切换等）；托盘菜单与窗口状态同步
- **自动更新**：启动时检测 WebView2 运行时（缺失则引导安装）；关于卡内检查 GitHub 更新

## 环境要求

- Windows 10 / 11（WebView2 运行时缺失时程序启动会引导安装）
- [Go](https://go.dev) 1.25+
- [Node.js](https://nodejs.org) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- sing-box.exe 与 wintun.dll（放 `sing-box/` 目录，或启动后在设置里指定核心路径）

## 开发

```cmd
wails dev
```

## 构建

```cmd
:: 简单打包（不带版本号，关于卡显示 git 提交哈希）
wails build

:: 正式发布（注入版本号与更新检查仓库）
.\build\release.ps1 -Version v1.0.0
```

产物在 `build/bin/Eagle.exe`。

## 目录结构

```
app_*.go          后端（Wails 绑定层）
internal/         核心 Go 逻辑（配置生成、订阅解析、TUN、系统代理…）
third_party/      energye/systray 的本地 fork（go.mod replace）
frontend/         Vue 3 + Vite 前端
build/            图标、安装器脚本、发布脚本
config*/          运行时数据目录（节点、订阅、设置——不提交）
dashboard/        Clash API 仪表盘（yacd）
```

## 致谢

- [sing-box](https://github.com/SagerNet/sing-box) — 代理内核
- [nekoray](https://github.com/MatsuriDayo/nekoray) — 数据结构与交互词汇的参考
- [Wails](https://wails.io) — 桌面框架

## 许可证

本项目以 [GPL-3.0](LICENSE) 发布。
