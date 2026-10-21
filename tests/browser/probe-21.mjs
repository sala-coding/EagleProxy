// probe-21.mjs — reel 堆叠、导入对话框、出口 IP 面板、错误路径（后端失败时的表现）。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));

// ---------- reel（打开抽屉后出现）----------
await step('reel', async () => {
    click(rail('所有订阅'));
    await sleep(1000);
    R.reelOn = !!$('.reel');
    R.reelCards = $$('.reel-card').map(c => ({gid: c.dataset.gid, front: c.classList.contains('front'), text: T(c)}));
    R.stackI = $$('.reel-card').map(c => c.style.getPropertyValue('--i'));
});

// ---------- 导入对话框 ----------
await step('import', async () => {
    rclick($$('.profiles tbody tr[data-pid]')[0]);
    await sleep(300);
    const imp = $$('.ctxmenu .ctx-item').find(b => T(b) === '导入');
    R.importBtnFound = !!imp;
    if (imp) {
        click(imp);
        await sleep(900);
        R.importOpen = !!$('.modal');
        R.importText = T($('.modal')).slice(0, 250);
        R.importButtons = buttons($('.modal'));
        const ta = $('.modal textarea');
        R.hasTextarea = !!ta;
        if (ta) {
            const b0 = calls().length;
            setVal(ta, 'vmess://eyJhZGQiOiJ4LmV4YW1wbGUuY29tIn0=\nss://YWVzLTI1Ni1nY206cGFzcw==@1.2.3.4:8388#node2');
            await sleep(300);
            const btn = $$('.modal .btn').find(b => /导入|确定|开始/.test(T(b)));
            R.importGoBtn = btn ? T(btn) : null;
            if (btn) { click(btn); await sleep(1500); }
            R.importCalls = calls().slice(b0).map(c => c.name);
            R.importTextAfter = ta ? ta.value : null;
        }
        const close = $$('.modal-head-actions button')[0];
        if (close) { click(close); await sleep(400); }
    }
});

// ---------- 出口 IP 面板 ----------
await step('exitIP', async () => {
    // 先启动一个节点，状态栏才会显示出口地址
    const row = $$('.profiles tbody tr[data-pid]')[0];
    row.dispatchEvent(new MouseEvent('dblclick', {bubbles: true, cancelable: true, view: window}));
    await sleep(1200);
    R.nodeIpBtn = !!$('.node-ip');
    R.nodeIpText = T($('.node-ip'));
    if ($('.node-ip')) {
        click($('.node-ip'));
        await sleep(700);
        R.exitPanel = !!$('.exit-panel');
        R.exitRows = $$('.exit-row').map(e => T(e));
        R.exitText = T($('.exit-panel')).slice(0, 300);
    }
});

// ---------- 错误路径：ListProfiles 失败 ----------
await step('listProfilesFails', async () => {
    window.__mock.fail('ListProfiles');
    const b0 = calls().length;
    // 切到另一个订阅触发重新加载
    click(rail('所有订阅'));
    await sleep(800);
    const card = $$('.subdrop-table tr.sub-card')[1];
    if (card) { click(card); await sleep(1200); }
    R.failCalls = calls().slice(b0).map(c => c.name);
    R.rowsAfterFail = $$('.profiles tbody tr[data-pid]').length;
    R.crash = !!$('.crash');
    R.errs = errs().map(e => e.text.split('\n')[0]);
    window.__mock.unfail('ListProfiles');
});

// ---------- 错误路径：GetSettings 失败（设置面板打开时）----------
await step('settingsFails', async () => {
    window.__mock.fail('GetSettings');
    click($('.status-logo'));
    await sleep(1500);
    R.settingsCrash = !!$('.crash');
    R.settingsDock = !!$('.settings-dock');
    R.errs2 = errs().map(e => e.text.split('\n')[0]);
    window.__mock.unfail('GetSettings');
});

R.errsAll = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
