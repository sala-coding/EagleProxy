// probe-review-app.mjs — review round: log legend "live" dot, import double-submit,
// egress mode write, and a few state checks.
const R = {};

// ---------------------------------------------------------------- 1. legend dot
// The log panel's legend dot for a category must go still ~1.5s after that
// category stops. Two categories arriving in two separate batches must not
// leave the first one lit forever.
const logBtn = $('.appnav-btn[aria-label*="日志"], .appnav-btn[aria-label*="log" i]');
R.logBtnFound = !!logBtn;
if (logBtn) {
    click(logBtn);
    await sleep(600);
}
R.panelOpen = !!$('.console');

// clear whatever is there so the counts are about our own lines
window.__mock.emit('core:log', '00:00:00.000 preflight core line', 'core');
await sleep(400);

// batch 1: a core line
window.__mock.emit('core:log', '00:00:01.000 batch-one core line', 'core');
await sleep(400);            // > 120ms so it drains into its own batch

// batch 2: a speedtest-category line (a different category)
window.__mock.emit('core:log', '00:00:02.000 batch-two speedtest line', 'speedtest-url');
await sleep(2200);           // well past the 1.5s the dot is given to settle

const legend = () => $$('.log-key').map(e => ({
    id: (e.className.match(/cat-([a-z\-]+)/) || [])[1],
    live: e.classList.contains('live'),
}));
R.legend = legend();
R.coreStillLive = legend().find(x => x.id === 'core')?.live ?? null;
R.speedtestStillLive = legend().find(x => x.id === 'speedtest-url')?.live ?? null;

// ---------------------------------------------------------------- 2. import double click
// The import modal's primary button has no busy guard: two clicks while the
// first import is still on the wire import the same links twice.
const row = $$('.profiles tbody tr[data-pid]')[0];
rclick(row);
await sleep(300);
const importItem = $$('.ctxmenu .ctx-item').find(b => T(b).includes('导入'));
R.importItemFound = !!importItem;
if (importItem) {
    click(importItem);
    await sleep(400);
}
const ta = $('textarea.rawjson');
R.modalOpen = !!ta;
if (ta) {
    setVal(ta, 'vmess://eyJhZGQiOiJhLmIuYyIsInBvcnQiOjQ0MywiaWQiOiJ4IiwibmFtZSI6IngifQ==');
    await sleep(200);
    const before = calls('ImportLinks').length;
    const btn = $$('.modal-foot .btn.primary').find(b => T(b).includes('导入'));
    R.importBtnText = btn ? T(btn) : null;
    click(btn);
    click(btn);              // the impatient second press
    await sleep(600);
    R.importCalls = calls('ImportLinks').length - before;
    R.importArgs = calls('ImportLinks').map(c => c.args);
}

// ---------------------------------------------------------------- 3. egress write
// The status bar's outbound mode must be written to the settings file with the
// mode the reader picked, and the bar must show the same word.
R.egressBefore = T($('.egress .egress-trigger'));
const egressBtn = $$('.egress-menu [role="option"]').find(b => T(b).includes('代理'));
if (egressBtn) {
    click($('.egress .egress-trigger'));
    await sleep(250);
    const opts = $$('.egress-menu [role="option"]');
    R.egressOptions = opts.map(o => T(o));
    const g = opts.find(o => T(o).includes('代理'));
    click(g);
    await sleep(900);
}
R.egressAfter = T($('.egress .egress-trigger'));
R.updateSettingsCalls = calls('UpdateSettings').map(c => ({
    egress_mode: c.args[0] && c.args[0].egress_mode,
}));
R.backendEgress = window.__mock.settings.egress_mode;

// ---------------------------------------------------------------- 4. speedtest on the wrong node
// "测当前" from the row menu must test the row the menu was opened on.
const row2 = $$('.profiles tbody tr[data-pid]')[1];
rclick(row2);
await sleep(300);
const testItem = $$('.ctxmenu .ctx-item-sub').find(b => T(b).includes('测当前'));
if (testItem) {
    hover(testItem);
    await sleep(300);
    const first = $$('.ctxmenu-sub .ctx-item')[0];
    const before = calls('SpeedtestProfiles').length;
    click(first);
    await sleep(500);
    R.speedtestArgs = calls('SpeedtestProfiles').slice(before).map(c => c.args);
    R.row2Pid = row2?.dataset.pid;
}

R.errs = errs();
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
