# tests/browser — 前端的回归测试

这是 EagleProxy 前端的浏览器回归测试：把**真实的** Vue 应用跑在**真实的**浏览器里，
后端换成一个假的（mock），然后像用户一样点它。

它回答的是后端测试回答不了的那一类问题："这个界面在真实交互下会不会崩、会不会
报出异常、会不会把请求发错"。第一轮全面走查里发现的四个前端/后端问题，有三个是
这样找出来的（主题色条拖动越界崩溃、参考速度列排序方向反了、设置键前后端不一致）。

## 跑起来

```cmd
cd frontend
npm run test:harness
```

这一条命令会自己把需要的东西都起起来：Vite 开发服务器（5199）和一个无头浏览器
（CDP 端口 9333），跑完全部探针，然后把两者关掉。

只想跑其中几个：

```cmd
cd tests\browser
node regress.mjs probe-05 probe-verify
```

跑完想留着服务器和浏览器（方便手工点）：

```cmd
cd tests\browser
node regress.mjs --keep
```

## 它由什么组成

| 文件 | 是什么 |
|---|---|
| `mock-bridge.js` | 假的后端。实现了 `wailsjs/go/main/App.d.ts` 里全部 92 个方法，外加 `window.runtime` 的事件总线。带三样东西：一个**调用记录**（界面向后端要了什么）、一个**失败注入**（`__mock.fail('ListProfiles')` 让某个调用开始就失败）、一个**事件注入**（`__mock.emit('core:exit', -1919)` 模拟后端推送）。 |
| `cdp.mjs` | 一个很小的 Chrome DevTools Protocol 驱动。`open`（装上错误收集器和假后端再导航）、`run`（在页面里跑一段 JS）、`mouse`（发真实的指针事件）、`viewport`（模拟窗口尺寸）、`shot`（截图）、`logs`（取页面里的报错）。 |
| `lib.js` | 探针共用的辅助函数：`click` / `rclick` / `hover` / `setVal` / `waitFor` / `tree` / `buttons`，以及 `calls(name)` 读后端的调用记录。 |
| `regress.mjs` | 跑全套的入口。起服务、起浏览器、逐个跑探针、汇总报错与崩溃、收摊。 |
| `vite.harness.config.ts` | 测试用的 Vite 配置。**不改动**生产的 `vite.config.ts`；唯一的差别是把无头浏览器的 profile 目录排除在文件监听之外（Chrome 锁着它的 cookie 文件，Vite 监听它会 EBUSY 崩掉）。 |
| `probe-*.mjs` | 37 个探针，每个管一件事。 |
| `diff-settings-keys.mjs` | 一个诊断脚本：把前端设置表单写的键和后端 `Setting` 结构体的字段列出来对照。（同样的检查现在由 `tests/go/setting/bugfind_frontendkeys_test.go` 在 Go 侧常驻，两者留一个就够。） |

## 页面就是应用自己的页面

这套脚手架没有自己的测试页：它驱动的是应用自己的首页
（`frontend/index.html`，由 Vite 开发服务器供着）。假后端不是页面里的一行
`<script>`，而是驱动在导航之前用 `Page.addScriptToEvaluateOnNewDocument` 注入的——
和错误收集器同一个机制，因此它在应用自己的模块跑起来之前就已经在了
（应用第一件事就是调 `window.go`）。

这样做的两个好处：被测的页面就是读者会看到的那一页（不多一份要维护的副本），
假后端与页面的装载顺序也不再靠两份文件的相对位置来保证。


## 探针怎么写

探针是一段在页面里跑的 JavaScript，**返回一个 JSON 对象**。跑的人看两个键：

* `errs` —— 页面上收集到的报错数组（`console.error`、未捕获的异常、Promise 拒绝）。
* `crash` —— 应用是不是掉进了崩溃页。

有一个算一个失败。

```js
// probe-例子.mjs
const R = {};
const rows = () => $$('.profiles tbody tr[data-pid]');

click($('.profiles th.speed.sortable'));   // lib.js 里的 click
await sleep(400);
R.order = rows().map(r => T(r.querySelector('td.speed')));
R.errs = errs();
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
```

`lib.js` 里的常用东西：

| | |
|---|---|
| `T(el)` | 元素里看得见的文字 |
| `$(sel)` / `$$(sel)` | 查元素 |
| `click(el)` / `rclick(el)` / `hover(el)` | 合成鼠标事件（`click` 会把 pointerdown 到 click 一整套都发一遍） |
| `setVal(el, v)` | 改输入框的值，并发 `input`/`change` 事件（Vue 的受控组件需要这个） |
| `waitFor(fn, ms)` | 等某个条件成立 |
| `calls(name)` / `lastCall(name)` | 读后端的调用记录 |
| `errs()` | 页面上收集到的报错 |
| `tree(root)` / `buttons(root)` | 看页面上有什么（调试用） |
| `step(name, fn)` | 跑一步并把失败记下来，而不是让整个探针死掉 |

## 真实指针事件

有些问题只有真的指针事件才出得来——拖动、按下、按住拖动。`cdp.mjs mouse` 发的是
**浏览器认可**的事件（`Input.dispatchMouseEvent`），和真人按下去的一样：

```cmd
node cdp.mjs mouse mousePressed 400 286
node cdp.mjs mouse mouseMoved 150 286
node cdp.mjs mouse mouseReleased 150 286
```

主题色条越界那个崩溃，就是这样复现的：合成事件在页面里派发不出 `buttons` 状态，
真实的指针事件可以。

## 假后端

```js
window.__mock.calls            // 全部调用：{name, args}
window.__mock.fail('GetSettings')    // 让这个调用开始就失败
window.__mock.unfail('GetSettings')
window.__mock.emit('core:exit', -1919)
window.__mock.groups           // 假后端的数据，可以直接改
window.__mock.profiles
```

改完数据后，发一个 `groups:changed` 事件让应用重新读一遍：

```js
window.__mock.groups.length = 0;
window.__mock.emit('groups:changed', 1);
```

## 排障

* **"the browser did not come up on 9333"** —— 上一次的浏览器还活着，把自己那份
  profile 锁住了。`taskkill /F /IM chrome.exe` 再跑。`regress.mjs` 自己起的浏览器
  会在结束时关掉。
* **"the dev server did not come up on 5199"** —— 端口被占了（`netstat -ano | findstr
  5199`），或者上一次的服务还在跑（那就直接用，`regress.mjs` 会发现它已经在了）。
* **探针报告了一个错，但看不出是什么错** —— `node cdp.mjs logs` 打印页面上收集到的
  全部报错。
* **想看某一步的画面** —— `node cdp.mjs shot out.png`。
