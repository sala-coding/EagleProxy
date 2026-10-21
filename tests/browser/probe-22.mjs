// probe-22.mjs — 错误路径与后端事件推送。
const R = {};
const note = () => ({
    win: T($('.note-window-body')),
    winErr: $('.note-window')?.classList.contains('err'),
    bar: T($('.status-note')),
});

// ---------- 后端事件：traffic ----------
await step('trafficEvent', async () => {
    window.__mock.emit('traffic', {up: 4096, down: 65536, upRate: 2048, downRate: 32768, count: 3});
    await sleep(400);
    R.trafficText = T($('.statusbar .traffic'));
    R.rates = $$('.statusbar .rate').map(e => T(e));
});

// ---------- 后端事件：speedtest:result ----------
await step('speedtestEvent', async () => {
    const before = $$('.profiles tbody tr[data-pid]').map(r => T(r.querySelector('td.latency')));
    window.__mock.emit('speedtest:result', {id: 3, mode: 0, latency: 42, speed: 0});
    await sleep(500);
    const after = $$('.profiles tbody tr[data-pid]').map(r => ({pid: r.dataset.pid, lat: T(r.querySelector('td.latency'))}));
    R.latencyAfter = after.find(x => x.pid === '3');
    R.before = before;
});

// ---------- 后端事件：core:exit ----------
await step('coreExit', async () => {
    window.__mock.emit('core:exit', 1);
    await sleep(800);
    R.runningAfterExit = $$('.profiles tbody tr.running').map(r => r.dataset.pid);
    R.stateChip = $('.state-chip')?.className;
    R.note = note();
});

// ---------- 后端事件：groups:changed ----------
await step('groupsChanged', async () => {
    window.__mock.emit('groups:changed', 2);
    await sleep(1200);
    R.rowsAfterGroupsChanged = $$('.profiles tbody tr[data-pid]').map(r => r.dataset.pid);
    R.calls = calls().slice(-6).map(c => c.name);
});

// ---------- 后端事件：app:message ----------
await step('appMessage', async () => {
    window.__mock.emit('app:message', '这是一条来自后端的消息', true);
    await sleep(400);
    R.note = note();
});

// ---------- 错误路径：StartProfile 失败 ----------
await step('startFails', async () => {
    window.__mock.fail('StartProfile');
    const row = $$('.profiles tbody tr[data-pid]')[0];
    row.dispatchEvent(new MouseEvent('dblclick', {bubbles: true, cancelable: true, view: window}));
    await sleep(1500);
    R.note = note();
    R.crash = !!$('.crash');
    window.__mock.unfail('StartProfile');
});

// ---------- 错误路径：CheckUpdate 失败 ----------
await step('updateFails', async () => {
    const rail = $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes('关于'));
    click(rail);
    await sleep(800);
    window.__mock.fail('CheckUpdate');
    const btn = $$('.modal .btn').find(b => T(b).includes('检查更新'));
    if (btn) { click(btn); await sleep(1200); }
    R.note = note();
    R.crash = !!$('.crash');
    window.__mock.unfail('CheckUpdate');
    const close = $$('.modal-head-actions button')[0];
    if (close) { click(close); await sleep(400); }
});

// ---------- 错误路径：ListProfiles 失败（切订阅）----------
await step('listFails', async () => {
    window.__mock.fail('ListProfiles');
    const rail = $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes('所有订阅'));
    click(rail);
    await sleep(900);
    const card = $$('.subdrop-table tr.sub-card')[1];
    R.cardFound = !!card;
    if (card) { click(card); await sleep(1500); }
    R.calls = calls().slice(-6).map(c => c.name);
    R.rows = $$('.profiles tbody tr[data-pid]').length;
    R.note = note();
    R.crash = !!$('.crash');
    window.__mock.unfail('ListProfiles');
});

R.errs = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
