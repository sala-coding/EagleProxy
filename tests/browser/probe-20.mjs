// probe-20.mjs — 分组标签(reel)、订阅用量、导入/二维码/编辑/移动对话框、搜索框添加订阅、预览/导出配置。
const R = {};
const ctxRow = () => $$('.profiles tbody tr[data-pid]')[0];

// ---------- 分组标签 reel ----------
await step('reel', async () => {
    R.reelCards = $$('.reel-card').map(c => T(c));
    R.reelCount = $$('.reel-card').length;
    if ($$('.reel-card').length > 1) {
        const b0 = calls().length;
        click($$('.reel-card')[1]);
        await sleep(1200);
        R.reelCalls = calls().slice(b0).map(c => c.name);
        R.rowsAfterSwitch = $$('.profiles tbody tr[data-pid]').map(r => r.dataset.pid);
        R.gidAfter = $$('.reel-card.front').map(c => T(c));
    }
});

// ---------- 订阅用量（drawer 里的 info）----------
await step('subUsage', async () => {
    const rail = $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes('所有订阅'));
    click(rail);
    await sleep(900);
    R.drawerText = T($('.subdrop')).slice(0, 400);
    R.subMeta = $$('.sub-meta-item').map(e => T(e));
    R.subMetaTitles = $$('.sub-meta-item').map(e => e.getAttribute('title'));
    document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
    await sleep(600);
});

// ---------- 右键菜单：新增节点（协议子菜单）----------
await step('addNode', async () => {
    rclick(ctxRow());
    await sleep(300);
    const newBtn = $$('.ctxmenu .ctx-item-sub').find(b => T(b) === '新增');
    R.newBtnFound = !!newBtn;
    if (newBtn) {
        hover(newBtn);
        await sleep(350);
        R.protocols = $$('.ctxmenu-sub .ctx-item').map(b => T(b));
        const b0 = calls().length;
        const vmess = $$('.ctxmenu-sub .ctx-item').find(b => /VMess/.test(T(b)));
        if (vmess) { click(vmess); await sleep(1200); }
        R.addCalls = calls().slice(b0).map(c => c.name);
        R.editorOpen = !!$('.modal');
        R.editorText = T($('.modal')).slice(0, 300);
    }
});

// ---------- 编辑器对话框：改名字并保存 ----------
await step('editorSave', async () => {
    if (!$('.modal')) return 'no editor';
    const inputs = $$('.modal input, .modal textarea');
    R.editorInputs = inputs.length;
    const nameInput = inputs[0];
    R.firstInputVal = nameInput ? nameInput.value : null;
    if (nameInput) {
        const b0 = calls().length;
        setVal(nameInput, '我的测试节点');
        await sleep(400);
        const saveBtn = $$('.modal .btn').find(b => /保存|确定|应用/.test(T(b)));
        R.saveBtn = saveBtn ? T(saveBtn) : null;
        if (saveBtn) { click(saveBtn); await sleep(1200); }
        R.saveCalls = calls().slice(b0).map(c => c.name);
        R.saveProfileArgs = calls('SaveProfileJSON').map(c => c.args[1]);
    }
});

// ---------- 移动对话框 ----------
await step('move', async () => {
    rclick(ctxRow());
    await sleep(300);
    const mv = $$('.ctxmenu .ctx-item').find(b => T(b) === '移动');
    R.moveBtnFound = !!mv;
    if (mv) {
        click(mv);
        await sleep(900);
        R.moveOpen = !!$('.picklist');
        R.moveItems = $$('.picklist li').map(e => T(e));
        const b0 = calls().length;
        if ($$('.picklist li').length) { click($$('.picklist li')[0]); await sleep(1000); }
        R.moveCalls = calls().slice(b0).map(c => c.name);
    }
});

// ---------- 二维码 ----------
await step('qr', async () => {
    rclick(ctxRow());
    await sleep(300);
    const qr = $$('.ctxmenu .ctx-item').find(b => T(b) === '二维码');
    R.qrBtnFound = !!qr;
    if (qr) {
        const b0 = calls().length;
        click(qr);
        await sleep(1200);
        R.qrCalls = calls().slice(b0).map(c => c.name);
        R.qrImg = !!$('.modal img[src^="data:image"]');
        R.qrSrcHead = ($('.modal img[src^="data:image"]')?.src || '').slice(0, 30);
        const close = $$('.modal-head-actions button')[0];
        if (close) { click(close); await sleep(500); }
    }
});

// ---------- 预览配置 ----------
await step('preview', async () => {
    rclick(ctxRow());
    await sleep(300);
    const pv = $$('.ctxmenu .ctx-item').find(b => T(b) === '预览配置');
    R.previewBtnFound = !!pv;
    if (pv) {
        const b0 = calls().length;
        click(pv);
        await sleep(1200);
        R.previewCalls = calls().slice(b0).map(c => c.name);
        R.previewText = T($('.modal')).slice(0, 200);
        const close = $$('.modal-head-actions button')[0];
        if (close) { click(close); await sleep(500); }
    }
});

// ---------- 搜索框：粘贴订阅链接并回车 ----------
await step('addSub', async () => {
    const search = $('.node-search');
    setVal(search, 'https://example.com/newsub');
    await sleep(300);
    const b0 = calls().length;
    search.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true, cancelable: true}));
    await sleep(1800);
    R.addSubCalls = calls().slice(b0).map(c => c.name);
    R.searchValueAfter = search.value;
    R.notice = T($('.status-note'));
});

R.errs = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
