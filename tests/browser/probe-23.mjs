// probe-23.mjs — 字体缩放（真实指针拖动 + 越界）、热键事件、订阅编辑表单、列宽拖动。
const R = {};

// ---------- 字体缩放轨道几何 ----------
await step('openSettings', async () => { click($('.status-logo')); await sleep(1300); });
const track = $('.ui-scale-track');
R.scaleGeom = (() => { const r = track.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; })();
R.scaleBefore = T($('.ui-scale-read'));
R.themeGeom = $$('.theme-cells').map(c => { const r = c.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; });

// ---------- 热键事件 ----------
await step('hotkeys', async () => {
    window.__mock.emit('hotkey:logs', 0);
    await sleep(800);
    R.logOpenedByHotkey = !!$('.console-body');
    window.__mock.emit('hotkey:logs', 0);
    await sleep(800);
    R.logClosedByHotkey = !$('.console-body');
    window.__mock.emit('hotkey:connections', 0);
    await sleep(900);
    R.connOpenedByHotkey = !!$('.conn-list');
    window.__mock.emit('hotkey:connections', 0);
    await sleep(900);
    R.connClosedByHotkey = !$('.conn-list');
});

// ---------- 订阅编辑表单 ----------
await step('subEdit', async () => {
    const rail = $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes('所有订阅'));
    click(rail);
    await sleep(900);
    const card = $$('.subdrop-table tr.sub-card')[0];
    const edit = $$('.sub-act', card).find(b => b.className.includes('edit'));
    if (!edit) return 'no edit button';
    click(edit);
    await sleep(1000);
    R.subEditOpen = !!$('.sub-edit');
    R.subEditTree = tree($('.sub-edit'), 5, 60);
    const inputs = $$('.sub-edit input, .sub-edit textarea');
    R.subEditInputs = inputs.map(i => ({cls: i.className, type: i.type, val: i.value, ph: i.placeholder}));
    const b0 = calls().length;
    const nameInput = inputs.find(i => /名称|name/i.test(i.placeholder || '')) || inputs[0];
    if (nameInput) {
        setVal(nameInput, '改名后的订阅');
        await sleep(1400);
    }
    R.subEditCalls = calls().slice(b0).map(c => c.name);
    R.updateGroupArgs = calls('UpdateGroup').map(c => JSON.stringify(c.args[0]).slice(0, 220));
});

// ---------- 关闭设置后回到窗口 ----------
R.finalErrs = errs().map(e => e.text.split('\n')[0]);
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
