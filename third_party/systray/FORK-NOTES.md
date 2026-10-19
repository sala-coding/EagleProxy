# systray — local fork

Fork of `github.com/energye/systray@v1.0.3`（上游最新版本就是 v1.0.3）。

保留的文件（Windows 编译只需要这三个）：

- `systray.go`（无构建标签，跨平台）
- `systray_other.go`（`//go:build !darwin`）
- `systray_windows.go`（`//go:build windows`）

已删除：`systray_unix.go`、`systray_menu_unix.go`、`systray_darwin.go/.m`、
`internal/`（dbus 生成代码）、`icon/`、`example/`。随之把 `go.mod` 里的
`github.com/godbus/dbus/v5` 依赖也去掉了。

## 为什么 fork

`winTray.ShowMenu` 里把定位参数写死成 `TPM_BOTTOMALIGN|TPM_LEFTALIGN`，
也就是把**菜单底边**对齐到鼠标位置。当任务栏不在底部时（例如用户把任务栏
放在屏幕顶部），Windows 只会把菜单限制在显示器范围内，于是菜单被顶到屏幕
最上方、**盖住任务栏**。上游没有提供任何调整接口，v1.0.3 之后也没有新版本。

## 改了什么

只改了 `systray_windows.go` 的 `ShowMenu`（外加新增一个 `SystemParametersInfoW`
的 proc 声明和 `rect` 结构体）：

- 用 `SPI_GETWORKAREA` 取工作区（Windows 的工作区**已排除停靠的任务栏**）；
- 鼠标在顶部边缘 → 菜单**顶边**对齐到 `work.Top`，即紧贴任务栏下方；
- 鼠标在底部 → 保持原来的 `TPM_BOTTOMALIGN`（菜单向上展开）；
- 鼠标在左/右边缘 → 水平锚点收进 `work.Left` / `work.Right`，避免盖住竖向任务栏。

## 升级方式

若要同步上游新版本：重新拉取该模块覆盖本目录，然后按上面的描述重新应用
`ShowMenu` 的改动（其余文件无需改动）。
