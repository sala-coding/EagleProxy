// probe-10.mjs — 日志面板、连接面板、设置对话框、关于面板。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));

// ================= 日志面板 =================
click(rail('核心日志'));
await sleep(800);
R.logOpen = !!$('.console, .logpanel, [class*=console]');
R.logTree = tree($('.console, .logpanel'), 5, 90);
R.logButtons = buttons($('.console, .logpanel'));
R.logText = T($('.console, .logpanel')).slice(0, 300);
R.logClasses = $$('[class*=log], [class*=console]').map(e => e.className).slice(0, 12);

// 日志右键菜单
const logArea = $('.console, .logpanel');
if (logArea) {
    rclick(logArea);
    await sleep(300);
    R.logCtxOpen = !!$('.ctxmenu.narrow');
    R.logCtxItems = $$('.ctxmenu.narrow .ctx-item').map(b => T(b));
}
R.errsLog = errs();
return JSON.stringify(R, null, 2);
