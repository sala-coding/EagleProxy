// probe-12.mjs — 连接面板、设置对话框、关于面板。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));

// ================= 连接面板 =================
await step('openConns', async () => {
    click(rail('连接'));
    await sleep(800);
    R.connClasses = $$('[class*=conn]').map(e => e.className).slice(0, 10);
    R.connTree = tree($('[class*=connpanel], .conns, [class*=conn]'), 5, 90);
    R.connText = T($('[class*=connpanel], .conns')).slice(0, 400);
    R.connButtons = buttons($('[class*=connpanel], .conns'));
});
await step('closeOne', async () => {
    const b0 = calls().length;
    const kill = $$('[class*=conn] button').find(b => /断开|close|×|✕/i.test(T(b) + (b.getAttribute('title') || '') + b.className));
    R.killBtn = kill ? (T(kill) + '|' + kill.className) : null;
    if (kill) { click(kill); await sleep(600); }
    R.closeCalls = calls().slice(b0).map(c => c.name);
    R.connCountAfter = window.__mock.connections.connections.length;
});
await step('closeConns', async () => {
    click(rail('连接'));
    await sleep(700);
    R.connClosed = $$('[class*=connpanel], .conns').length === 0;
});

// ================= 关于面板 =================
await step('about', async () => {
    click(rail('关于'));
    await sleep(700);
    R.aboutClasses = $$('[class*=about]').map(e => e.className).slice(0, 10);
    R.aboutTree = tree($('[class*=about]'), 5, 70);
    R.aboutText = T($('[class*=about]')).slice(0, 400);
    R.aboutButtons = buttons($('[class*=about]'));
});
await step('checkUpdate', async () => {
    const b0 = calls().length;
    const btn = $$('[class*=about] button').find(b => /检查更新|更新/.test(T(b)));
    R.updateBtn = btn ? T(btn) : null;
    if (btn) { click(btn); await sleep(800); }
    R.updateCalls = calls().slice(b0).map(c => c.name);
    R.updateNote = T($('.status-note'));
});
await step('bugReport', async () => {
    const b0 = calls().length;
    const btn = $$('[class*=about] button').find(b => /反馈|报告|问题/.test(T(b)));
    R.bugBtn = btn ? T(btn) : null;
    if (btn) { click(btn); await sleep(500); }
    R.bugCalls = calls().slice(b0).map(c => c.name);
    R.lastURL = window.__mock.state.lastURL;
});

R.errs2 = errs();
return JSON.stringify(R, null, 2);
