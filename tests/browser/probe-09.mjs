// probe-09.mjs — 订阅抽屉：卡片、悬停操作（更新/编辑/删除）、用量解析；分组标签（reel）；日志与连接面板。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));

// ================= 订阅抽屉 =================
click(rail('所有订阅'));
await sleep(600);
R.cards = $$('.subdrop-table tr.sub-card').map(tr => ({
    gid: tr.dataset.gid,
    name: T(tr.querySelector('.cap-name')),
    count: T(tr.querySelector('.tag-count')),
    acts: $$('.sub-act', tr).map(b => ({cls: b.className, title: b.getAttribute('title'), text: T(b)})),
}));
R.details = $$('.subdrop-table tr.sub-detail').map(tr => T(tr));
R.drawerText = T($('.subdrop')).slice(0, 500);

// 悬停第一张卡 → 操作按钮是否出现
const card = $$('.subdrop-table tr.sub-card')[0];
R.actsVisibleBefore = $$('.sub-act', card).map(b => vis(b));
hover(card);
await sleep(250);
R.actsVisibleAfter = $$('.sub-act', card).map(b => vis(b));

// 更新（refresh）
const refresh = $$('.sub-act', card).find(b => b.className.includes('refresh'));
if (refresh) {
    const b0 = calls().length;
    click(refresh);
    await sleep(800);
    R.refreshCalls = calls().slice(b0).map(c => c.name);
    R.refreshNote = T($('.status-note')) || T($('.toast, .notice, [class*=toast]'));
}

// 编辑 → 行内编辑页
const edit = $$('.sub-act', card).find(b => b.className.includes('edit'));
if (edit) {
    const b0 = calls().length;
    click(edit);
    await sleep(700);
    R.editCalls = calls().slice(b0).map(c => c.name);
    R.editFormVisible = !!$('.subdrop-table tr.sub-edit, .subdrop input, .subdrop textarea');
    R.editTree = tree($('.subdrop'), 6, 80);
}
R.errs1 = errs();
return JSON.stringify(R, null, 2);
