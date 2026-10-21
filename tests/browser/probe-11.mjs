// probe-11.mjs — 日志面板交互（暂停/级别/分类/复制/清空/折叠、右键菜单）。
const R = {steps: []};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));

await step('open', async () => { click(rail('核心日志')); await sleep(800); return !!$('.console-body'); });
R.linesBefore = $$('.console-body .logline').length;

const sel = $('.log-level');
R.levelOptions = $$('option', sel).map(o => ({v: o.value, t: o.textContent.trim()}));
await step('levelFilter', async () => {
    const b0 = calls().length;
    setVal(sel, 'error');
    await sleep(700);
    R.linesAfterErrorFilter = $$('.console-body .logline').length;
    R.levelFilterCalls = calls().slice(b0).map(c => c.name);
    setVal(sel, 'all');
    await sleep(400);
});

await step('catFilter', async () => {
    const filter = $$('.log-filter-item').find(b => T(b) === '代理');
    if (!filter) return 'no filter button';
    const b1 = calls().length;
    click(filter);
    await sleep(700);
    R.linesAfterCatFilter = $$('.console-body .logline').length;
    R.catFilterCalls = calls().slice(b1).map(c => c.name);
    R.filterOn = $$('.log-filter-item.on').map(b => T(b));
    click($$('.log-filter-item').find(b => T(b) === '全部'));
    await sleep(400);
});

await step('pause', async () => {
    click($('.log-pause'));
    await sleep(400);
    R.pauseClass = $('.log-pause')?.className;
    R.pauseTitle = $('.log-pause')?.getAttribute('title');
    click($('.log-pause'));
    await sleep(300);
});

await step('copy', async () => {
    const b2 = calls().length;
    click($('.log-copy'));
    await sleep(700);
    R.copyCalls = calls().slice(b2).map(c => c.name);
    R.clipboardLen = (window.__mock.state.clipboard || '').length;
});

await step('clear', async () => {
    const b3 = calls().length;
    click($('.log-clear'));
    await sleep(700);
    R.clearCalls = calls().slice(b3).map(c => c.name);
    R.linesAfterClear = $$('.console-body .logline').length;
    R.consoleStillThere = !!$('.console');
    R.consoleClasses = $('.console')?.className;
    R.consoleBodyThere = !!$('.console-body');
    R.consoleHTML = T($('.console')).slice(0, 200);
});

await step('bodyCtx', async () => {
    rclick($('.console-body'));
    await sleep(400);
    R.bodyCtxOpen = !!$('.ctxmenu.narrow');
    R.bodyCtxItems = $$('.ctxmenu.narrow .ctx-item').map(b => T(b));
    document.body.dispatchEvent(new MouseEvent('click', {bubbles: true}));
    await sleep(200);
});

await step('lineCtx', async () => {
    const line = $$('.console-body .logline')[0];
    if (!line) return 'no line';
    rclick(line);
    await sleep(400);
    R.lineCtxOpen = !!$('.ctxmenu.narrow');
    R.lineCtxItems = $$('.ctxmenu.narrow .ctx-item').map(b => T(b));
    document.body.dispatchEvent(new MouseEvent('click', {bubbles: true}));
    await sleep(200);
});

await step('collapse', async () => {
    const btn = $('.console-collapse');
    if (!btn) return 'no collapse button';
    click(btn);
    await sleep(600);
    R.afterCollapse = !!$('.console-body');
    R.collapsedClasses = $('.console')?.className;
    const btn2 = $('.console-collapse');
    if (btn2) { click(btn2); await sleep(600); }
    R.afterExpand = !!$('.console-body');
});

R.errsLog = errs();
return JSON.stringify(R, null, 2);
