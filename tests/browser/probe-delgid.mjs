// probe-delgid.mjs — 删掉当前选中的订阅之后，"当前分组"是否跟着换了。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));
const rows = () => $$('.profiles tbody tr[data-pid]');
const hue = () => {
    const td = rows()[0]?.querySelector('td.type');
    return td ? td.style.getPropertyValue('--tab-hue') : null;
};
const importTarget = async () => {
    // 打开"导入"对话框，看它说的导入目标是哪个分组
    rclick(rows()[0]);
    await sleep(300);
    const imp = $$('.ctxmenu .ctx-item').find(b => T(b) === '导入');
    if (!imp) return null;
    click(imp);
    await sleep(900);
    const btn = $$('.modal .btn.primary').find(b => T(b).startsWith('导入到'));
    const label = btn ? T(btn) : null;
    const close = $$('.modal-head-actions button')[0];
    if (close) { click(close); await sleep(400); }
    return label;
};

await step('setup', async () => {
    R.groupsBefore = window.__mock.groups.map(g => ({id: g.id, name: g.name}));
    R.rowsBefore = rows().length;
    R.hueBefore = hue();
    R.importTargetBefore = await importTarget();
});

// 删掉当前选中的那个分组（gid=1）
await step('deleteActive', async () => {
    click(rail('所有订阅'));
    await sleep(900);
    const card = $$('.subdrop-table tr.sub-card').find(c => c.dataset.gid === '1') || $$('.subdrop-table tr.sub-card')[0];
    const del = card && $$('.sub-act', card).find(b => b.className.includes('danger'));
    if (!del) return 'no delete button';
    click(del);            // 第一次：进入确认态
    await sleep(500);
    R.armed = T($$('.sub-act', card).find(b => b.className.includes('danger')));
    click($$('.sub-act', card).find(b => b.className.includes('danger')));  // 第二次：真删
    await sleep(1800);
    R.groupsAfter = window.__mock.groups.map(g => ({id: g.id, name: g.name}));
    R.rowsAfter = rows().map(r => r.dataset.pid);
    R.rowGroupAfter = rows().map(r => T(r.querySelector('td.pgroup')));
    R.hueAfter = hue();
    R.importTargetAfter = await importTarget();
    R.crash = !!$('.crash');
    R.errs = errs().map(e => e.text.split('\n')[0]).slice(-4);
});

// 搜索：看会不会出现重复行
await step('searchDuplicates', async () => {
    const search = $('.node-search');
    setVal(search, 'a');
    await sleep(700);
    const pids = rows().map(r => r.dataset.pid);
    const dupes = pids.filter((p, i) => pids.indexOf(p) !== i);
    R.searchRows = pids;
    R.duplicatePids = dupes;
    setVal(search, '');
    await sleep(400);
});

R.finalErrs = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
