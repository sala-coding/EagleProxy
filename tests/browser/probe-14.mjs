// probe-14.mjs — 设置对话框：卡片、主题、语言、缩放、DNS/规则集/路由/TUN 各卡。
const R = {};
await step('open', async () => {
    click($('.status-logo'));
    await sleep(1000);
    R.dockFound = !!$('.settings-dock');
    R.tree = tree($('.settings-dock'), 6, 200);
    R.text = T($('.settings-dock')).slice(0, 1200);
    R.buttons = buttons($('.settings-dock')).slice(0, 60);
    R.inputs = $$('.settings-dock input, .settings-dock select, .settings-dock textarea').map(e => ({
        tag: e.tagName.toLowerCase(), type: e.type, cls: e.className,
        label: e.getAttribute('aria-label') || e.getAttribute('placeholder') || '',
        val: e.value, opts: e.tagName === 'SELECT' ? $$('option', e).map(o => o.textContent.trim()) : undefined,
    }));
});
R.errs = errs();
return JSON.stringify(R, null, 2);
