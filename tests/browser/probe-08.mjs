// probe-08.mjs — 订阅抽屉 / 日志 / 连接：打开后整页结构快照（不猜类名，先看真实 DOM）。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));
R.railLabels = $$('.appnav .appnav-btn').map(b => b.getAttribute('aria-label'));

const before = tree($('#app'), 4, 40);
click(rail('订阅'));
await sleep(600);
R.afterDrawer = tree($('#app'), 5, 160);
R.newClasses = [...new Set($$('#app *').map(e => typeof e.className === 'string' ? e.className : '').filter(c => c && !before.includes(c)))].slice(0, 40);

R.errs = errs();
return JSON.stringify(R, null, 2);
