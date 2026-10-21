// probe-03.mjs — 节点表格：排序（延迟/速度，三态）、搜索过滤、选中。
const rows = () => $$('.profiles tbody tr[data-pid]');
const order = () => rows().map(r => ({
    pid: r.dataset.pid,
    name: T(r.querySelector('td.name')),
    addr: T(r.querySelector('td.addr')),
    latency: r.querySelector('td.latency')?.getAttribute('title') || T(r.querySelector('td.latency')),
    latClass: r.querySelector('td.latency')?.className,
    speed: T(r.querySelector('td.speed')),
}));
const report = {};

// --- 排序：延迟列，三态（降序 → 升序 → 取消） ---
const latTh = $('.profiles th.latency.sortable');
report.latencyThFound = !!latTh;
report.before = order().map(o => `${o.pid}:${o.latClass}`);

click(latTh);
await sleep(350);
report.afterClick1 = order().map(o => `${o.pid}:${o.latClass}`);

click(latTh);
await sleep(350);
report.afterClick2 = order().map(o => `${o.pid}:${o.latClass}`);

click(latTh);
await sleep(350);
report.afterClick3 = order().map(o => `${o.pid}:${o.latClass}`);

// --- 排序：速度列 ---
const spTh = $('.profiles th.speed.sortable');
report.speedThFound = !!spTh;
click(spTh);
await sleep(350);
report.afterSpeed1 = order().map(o => `${o.pid}:${o.speed}`);
click(spTh);
await sleep(350);
report.afterSpeed2 = order().map(o => `${o.pid}:${o.speed}`);

// --- 搜索 ---
const search = $('.node-search');
report.searchFound = !!search;
setVal(search, 'hk01');
await sleep(400);
report.searchFiltered = order().map(o => o.addr);
report.searchOtherGroups = window.__mock.calls.filter(c => c.name === 'ListProfiles').slice(-3);
setVal(search, '');
await sleep(400);
report.afterClear = order().length;

// --- 行点击选中 ---
const first = rows()[0];
click(first);
await sleep(200);
report.selectedRows = $$('.profiles tbody tr.selected, .profiles tbody tr[aria-selected=true], .profiles tbody tr.is-selected').map(r => r.dataset.pid);
report.rowClasses = rows().slice(0, 3).map(r => r.className);

report.errs = errs();
return JSON.stringify(report, null, 2);
