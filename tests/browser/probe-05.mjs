// probe-05.mjs — 行右键菜单（.ctxmenu）：条目、子菜单、每个动作的��端调用。
const R = {};
const row = $$('.profiles tbody tr[data-pid]')[1];
R.rowPid = row?.dataset.pid;

rclick(row);
await sleep(300);
R.menuOpen = !!$('.ctxmenu');
R.items = $$('.ctxmenu .ctx-item').map(b => T(b));
R.subItems = $$('.ctxmenu .ctx-item-sub').map(b => T(b));

// 悬停第一个子菜单项 → 子面板
const subBtn = $('.ctxmenu .ctx-item-sub');
if (subBtn) {
    hover(subBtn);
    await sleep(300);
    R.subPanelOpen = !!$('.ctxmenu-sub');
    R.subPanelItems = $$('.ctxmenu-sub .ctx-item').map(b => T(b));
    R.subPanelBox = (() => { const e = $('.ctxmenu-sub'); if (!e) return null; const r = e.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; })();
    R.mainBox = (() => { const e = $('.ctxmenu'); if (!e) return null; const r = e.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; })();
    R.subInsideViewport = (() => { const e = $('.ctxmenu-sub'); if (!e) return null; const r = e.getBoundingClientRect(); return r.left >= 0 && r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight; })();
}

// 逐个点击子菜单里的项，看后端被调用了什么
const before = calls().length;
if (subBtn) {
    const panelItem = $$('.ctxmenu-sub .ctx-item')[0];
    if (panelItem) {
        R.subPanelFirst = T(panelItem);
        click(panelItem);
        await sleep(500);
        R.callsFromSub = calls().slice(before).map(c => c.name);
    }
}

// 重新打开菜单，点"复制链接"（第一个非子菜单项）
R.menuOpen2 = false;
rclick(row);
await sleep(300);
R.menuOpen2 = !!$('.ctxmenu');
const copyItem = $$('.ctxmenu .ctx-item').find(b => T(b).includes('复制'));
R.copyItemText = copyItem ? T(copyItem) : null;
if (copyItem) {
    const b2 = calls().length;
    click(copyItem);
    await sleep(500);
    R.copyCalls = calls().slice(b2).map(c => c.name);
    R.clipboard = window.__mock.state.clipboard;
    R.menuClosedAfterRun = !$('.ctxmenu');
}

// 菜单是否被外部点击关闭
rclick(row);
await sleep(250);
R.menuOpen3 = !!$('.ctxmenu');
document.body.dispatchEvent(new MouseEvent('click', {bubbles: true}));
await sleep(250);
R.menuClosedByOutsideClick = !$('.ctxmenu');

// Escape 关闭
rclick(row);
await sleep(250);
R.menuOpen4 = !!$('.ctxmenu');
document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
await sleep(250);
R.menuClosedByEscape = !$('.ctxmenu');

R.errs = errs();
return JSON.stringify(R, null, 2);
