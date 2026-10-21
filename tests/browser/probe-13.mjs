// probe-13.mjs — 关于面板（.modal）+ 设置对话框。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));

// ================= 关于面板 =================
await step('about', async () => {
    click(rail('关于'));
    await sleep(700);
    const m = $('.modal');
    R.aboutFound = !!m;
    R.aboutText = T(m).slice(0, 500);
    R.aboutRows = $$('.about-rows dt', m).map((dt, i) => dt.textContent.trim() + ' = ' + T($$('.about-rows dd', m)[i]));
    R.aboutButtons = buttons(m);
});
await step('checkUpdate', async () => {
    const b0 = calls().length;
    const btn = $$('.modal .btn').find(b => T(b).includes('检查更新'));
    if (btn) { click(btn); await sleep(900); }
    R.updateCalls = calls().slice(b0).map(c => c.name);
    R.updateNote = T($('.status-note'));
});
await step('bugReport', async () => {
    const b0 = calls().length;
    const btn = $$('.modal .btn').find(b => T(b).includes('Bug'));
    if (btn) { click(btn); await sleep(600); }
    R.bugCalls = calls().slice(b0).map(c => c.name);
    R.bugURL = window.__mock.state.lastURL;
});
await step('closeAbout', async () => {
    const btn = $$('.modal-head-actions button').find(b => !T(b));
    if (btn) click(btn);
    await sleep(500);
    R.aboutClosed = !$('.modal');
});

// ================= 设置对话框 =================
await step('settings', async () => {
    click($('.status-logo'));
    await sleep(900);
    R.settingsClasses = $$('[class*=settings], .setpanel, [class*=sheet]').map(e => e.className).slice(0, 12);
    R.settingsTree = tree($('.modal'), 4, 120);
    R.settingsText = T($('.modal')).slice(0, 600);
    R.settingsButtons = buttons($('.modal')).slice(0, 40);
    R.settingsTabs = $$('.modal [class*=tab], .modal .card-tab, .modal nav button').map(b => T(b));
});
R.errs3 = errs();
return JSON.stringify(R, null, 2);
