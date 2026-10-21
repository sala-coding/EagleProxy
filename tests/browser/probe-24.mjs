// probe-24.mjs — 设置面板完全展开后的几何 + 订阅编辑保存。
const R = {};
await step('open', async () => { click($('.status-logo')); await sleep(2500); });
const track = $('.ui-scale-track');
R.scaleGeom = (() => { const r = track.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; })();
R.themeGeom = $$('.theme-cells').map(c => { const r = c.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; });
R.scaleBefore = T($('.ui-scale-read'));

// 订阅编辑保存
await step('subEditSave', async () => {
    const rail = $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes('所有订阅'));
    click(rail);
    await sleep(1200);
    const card = $$('.subdrop-table tr.sub-card')[0];
    const edit = $$('.sub-act', card).find(b => b.className.includes('edit'));
    if (!edit) return 'no edit';
    click(edit);
    await sleep(1200);
    const nameInput = $$('.sub-edit input')[0];
    if (!nameInput) return 'no input';
    const b0 = calls().length;
    setVal(nameInput, '改名后的订阅');
    await sleep(500);
    const save = $$('.sub-edit-foot .btn.primary')[0];
    R.saveBtn = save ? T(save) : null;
    if (save) { click(save); await sleep(1500); }
    R.calls = calls().slice(b0).map(c => c.name);
    R.updateGroupArgs = calls('UpdateGroup').map(c => JSON.stringify(c.args[0]).slice(0, 240));
    R.drawerText = T($('.subdrop')).slice(0, 160);
});
R.errs = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
