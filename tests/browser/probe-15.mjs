// probe-15.mjs — 设置各卡交互：主题、语言、缩放、端口检测、DNS 智能优选、规则集、路由。
const R = {};
const nav = n => $$('.set-nav-item').find(b => T(b).includes(n));
const slide = i => $$('.set-group.set-slide')[i];

await step('open', async () => {
    click($('.status-logo'));
    await sleep(1000);
    R.slideCount = $$('.set-group.set-slide').length;
    R.navNames = $$('.set-nav-item').map(b => T(b));
});

// ---------- 主题 ----------
await step('theme', async () => {
    const swatches = $$('.theme-swatch');
    R.swatchCount = swatches.length;
    const before = document.documentElement.getAttribute('data-theme');
    const sw = swatches[2];
    click(sw);
    await sleep(600);
    R.themeBefore = before;
    R.themeAfter = document.documentElement.getAttribute('data-theme');
    R.variant = document.documentElement.getAttribute('data-variant');
    R.themeActive = $$('.theme-swatch.active').map(e => e.className);
});

// ---------- 语言 ----------
await step('language', async () => {
    const pills = $$('.lang-pills .seg-tab');
    R.pillCount = pills.length;
    const en = pills.find(p => /English|英文/.test(T(p)) || p.getAttribute('lang') === 'en');
    R.pillLabels = pills.map(p => T(p));
    if (en) {
        click(en);
        await sleep(1200);
        R.langAfter = document.documentElement.getAttribute('lang') || '(none)';
        R.railLabelsAfter = $$('.appnav .appnav-btn').map(b => b.getAttribute('aria-label'));
        R.settingsTextAfter = T($('.settings-dock')).slice(0, 120);
    }
});

// ---------- 字体缩放 ----------
await step('uiScale', async () => {
    const read = $('.ui-scale-read');
    R.scaleBefore = T(read);
    const track = $('.ui-scale-track');
    R.trackRect = track ? (() => { const r = track.getBoundingClientRect(); return {l: r.left, t: r.top, w: r.width, h: r.height}; })() : null;
    // 双击轨道 = 恢复系统默认
    track?.dispatchEvent(new MouseEvent('dblclick', {bubbles: true}));
    await sleep(500);
    R.scaleAfterDbl = T($('.ui-scale-read'));
});

// ---------- 入站端口检测 ----------
await step('portCheck', async () => {
    const b0 = calls().length;
    const btn = $$('.settings-dock .btn').find(b => T(b) === '检测');
    if (btn) { click(btn); await sleep(700); }
    R.portCalls = calls().slice(b0).map(c => c.name);
    R.portStatus = T($('.set-notice')) || T($('.rt-warn'));
});

// ---------- DNS 卡 ----------
await step('dnsCard', async () => {
    click(nav('DNS'));
    await sleep(700);
    const s = slide(3);
    R.dnsSlideFound = !!s;
    R.dnsText = s ? T(s).slice(0, 300) : null;
    // 打开"智能优选"开关
    const boxes = $$('input[type=checkbox]', s);
    const smart = boxes.find(b => {
        const lab = b.closest('.jf-field');
        return lab && /智能优选/.test(T(lab));
    });
    R.smartFound = !!smart;
    if (smart) {
        const b1 = calls().length;
        smart.checked = true;
        smart.dispatchEvent(new Event('change', {bubbles: true}));
        await sleep(2000);
        R.smartCalls = calls().slice(b1).map(c => c.name);
        R.dnsTestArgs = calls('TestDNS').slice(-2).map(c => c.args);
        R.notice = T($('.set-notice'));
    }
});

// ---------- 规则集卡 ----------
await step('ruleSets', async () => {
    click(nav('规则集'));
    await sleep(1200);
    const s = $$('.set-group.set-slide').find(x => /rs-nav/.test(x.innerHTML || ''));
    R.rsSlideFound = !!s;
    R.rsText = s ? T(s).slice(0, 400) : null;
    R.rsButtons = s ? buttons(s).slice(0, 20) : null;
    const b0 = calls().length;
    const dl = s && $$('.btn', s).find(b => /下载|更新/.test(T(b)));
    if (dl) { click(dl); await sleep(900); }
    R.rsCalls = calls().slice(b0).map(c => c.name);
});

R.errs = errs();
return JSON.stringify(R, null, 2);
