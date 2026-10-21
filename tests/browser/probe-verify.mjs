// probe-verify.mjs — 修复后的行为核对：排序方向 + 主题条越界不再崩。
const R = {};
const rows = () => $$('.profiles tbody tr[data-pid]');
const speeds = () => rows().map(r => `${r.dataset.pid}:${T(r.querySelector('td.speed'))}`);
const lats = () => rows().map(r => `${r.dataset.pid}:${T(r.querySelector('td.latency'))}`);

// --- 速度列：首次点击应是最快在前（降序，▼）---
const spTh = $('.profiles th.speed.sortable');
click(spTh);
await sleep(400);
R.speedClick1 = speeds();
R.speedArrow1 = T($('.profiles th.speed .sort-mark'));
click(spTh);
await sleep(400);
R.speedClick2 = speeds();
R.speedArrow2 = T($('.profiles th.speed .sort-mark'));
click(spTh);
await sleep(400);
R.speedClick3 = speeds();
R.speedArrow3 = T($('.profiles th.speed .sort-mark'));

// --- 延迟列：行为不变，仍是首次升序（最快的延迟在前，▲）---
const latTh = $('.profiles th.latency.sortable');
click(latTh);
await sleep(400);
R.latClick1 = lats();
R.latArrow1 = T($('.profiles th.latency .sort-mark'));
click(latTh);
await sleep(400);
R.latClick2 = lats();
R.latArrow2 = T($('.profiles th.latency .sort-mark'));
click(latTh);
await sleep(400);
R.latClick3 = lats();

// --- 测速后的自动排序应与手动首次点击一致 ---
rclick(rows()[0]);
await sleep(250);
const sub = $('.ctxmenu .ctx-item-sub');
hover(sub);
await sleep(250);
const speedItem = $$('.ctxmenu-sub .ctx-item').find(b => T(b) === '速度测试');
if (speedItem) { click(speedItem); await sleep(1200); }
R.afterSpeedtest = speeds();
R.afterSpeedtestArrow = T($('.profiles th.speed .sort-mark'));

R.errs = errs().map(e => e.text.split('\n')[0]);
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
