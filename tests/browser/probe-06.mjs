// probe-06.mjs — 排序方向一致性（手动点表头 vs 测速自动排序）、启动节点、模式开关、批量操作。
const R = {};
const rows = () => $$('.profiles tbody tr[data-pid]');
const speeds = () => rows().map(r => ({pid: r.dataset.pid, speed: T(r.querySelector('td.speed'))}));

// --- 1. 手动点"参考速度"表头 ---
const spTh = $('.profiles th.speed.sortable');
click(spTh);
await sleep(400);
R.manualSort1 = speeds();
R.manualArrow1 = T($('.profiles th.speed .sort-mark'));
click(spTh);
await sleep(400);
R.manualSort2 = speeds();
R.manualArrow2 = T($('.profiles th.speed .sort-mark'));
// 复位
click(spTh);
await sleep(300);

// --- 2. 测速运行时自动排的方向（整组测速，模式=速度测试）---
const ctxRow = rows()[0];
rclick(ctxRow);
await sleep(250);
const sub = $('.ctxmenu .ctx-item-sub');       // 测当前
hover(sub);
await sleep(250);
const panelItems = $$('.ctxmenu-sub .ctx-item');
R.panelItems = panelItems.map(b => T(b));
const speedItem = panelItems.find(b => T(b) === '速度测试');
if (speedItem) {
    click(speedItem);
    await sleep(1200);
    R.afterSpeedtestSort = speeds();
    R.afterSpeedtestArrow = T($('.profiles th.speed .sort-mark'));
    R.speedtestCall = lastCall('SpeedtestProfiles');
}

R.errs = errs();
return JSON.stringify(R, null, 2);
