# tests — 测试

这个目录是所有测试的家。

```
tests/
  go/<包名>/       Go 的测试
  browser/         前端的浏览器回归测试（真实界面 + 假后端 + 无头浏览器）
```

## 跑起来

```cmd
::: Go 的测试
go test ./...

::: 前端的浏览器回归测试
cd frontend
npm run test:harness
```

## go/ —— Go 的测试

`tests/go/<包名>/` 下是**外部测试包**（`package <包名>_test`），它们只碰被测包的
导出名字，所以可以离开被测包自己待着。

**为什么有些测试仍然留在被测包旁边**：Go 的规矩是，同一个目录里的测试文件可以
直接用该包自己的名字——包括没导出的那些。只有当测试文件用到了包内的未导出符号
（未导出的函数、字段、方法）时，它**必须**待在那个包里，搬走就编译不过。这类
测试留在原处，是 Go 语言的限制，不是没整理干净。当前留在原处的有：

| 留在原处的文件 | 它碰了什么 |
|---|---|
| `app_hotkeys_bugfind_test.go`、`review_routing_test.go`、`review_setting_swap_test.go`、`review_group_test.go`、`review_tun_startup_test.go`、`review_update_script_test.go`、`review_dns_node_test.go`、`review_exit_fail_test.go`、`review_exit_backoff_test.go`、`review_logdir_test.go` | `package main` 不可被导入，根包的测试只能在根目录 |
| `internal/configbuilder/review_config_test.go` | 包内的 `hopPortRanges`；另有"两种模式生成的配置是否相同"的测试（模式切换不重启节点的前提） |
| `internal/dnsstub/dnsstub_test.go` 等 | 包内的 `readFramed` / `writeFramed` / `startOn` |
| `internal/dnsstub/review_failfast_test.go` | 包内的 `readFramed` / `writeFramed` / `setSide`（失败的一侧不再被反复去问） |
| `internal/dnsstub/review_failure_identity_test.go` | 包内的 `startOn` / `setSide` / `clientFor` / `noteFailure`（换了 client 之后的在途失败不记到新侧头上） |
| `internal/core/review_core_test.go` | 客户端的未导出字段 `mu` |
| `internal/dnsx/review_dnsx_test.go`、`internal/dnsx/review_doh_retry_test.go` | 未导出的方法 `exchangeDoHOnce` / `dohTransport` |
| `internal/tunfast/review_dns_test.go` | 未导出的方法 `resolverAddrs` |
| `internal/tunfast/internal/winipcfg/winipcfg_test.go` | 嵌套的 `internal` 包，仓库外不可导入 |
| `internal/sub/review_sub_test.go` 等 | 包内的 `containsInt` / `rawUpdater` |
| `internal/util/review_util_test.go` | 包内的 `ipEchoURLs` |

## browser/ —— 前端的浏览器回归测试

把**真实的** Vue 应用跑在**真实的**浏览器里，后端换成一个假的（mock），然后像用户
一样点它。详细说明见 [browser/README.md](browser/README.md)。

要点：被测的页面就是应用自己的首页，假后端由驱动在导航之前注入（`cdp.mjs` 用
`Page.addScriptToEvaluateOnNewDocument`），所以这套脚手架不需要自己的测试页。
