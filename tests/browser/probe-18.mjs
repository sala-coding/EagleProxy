// probe-18.mjs — 设置各卡：语言、字体缩放、端口检测、DNS 智能优选、规则集、路由。
const R = {};
const nav = n => $$('.set-nav-item').find(b => T(b).includes(n));
const slideAt = i => $$('.set-group.set-slide')[i];

await step('open', async () => {
    click($('.status-logo'));
    await sleep(1400);
    R.slideCount = $$('.set-group.set-slide').length;
});

// ---------- 语言 ----------
await step('language', async () => {
    const pills = $$('.lang-pills .seg-tab');
    R.pillCount = pills.length;
    R.pillLabels = pills.map(p => T(p));
    if (!pills.length) return 'no pills';
    const before = T($$('.appnav .appnav-btn')[0]);
    const en = pills.find(p => /English|英文/i.test(T(p))) || pills[pills.length - 1];
    R.picked = T(en);
    click(en);
    await sleep(1500);
    R.railLabelsAfter = $$('.appnav .appnav-btn').map(b => b.getAttribute('aria-label'));
    R.dockTextAfter = T($('.settings-dock')).slice(0, 160);
    R.errsAfterLang = errs().map(e => e.text.split('\n')[0]);
    // 切回中文
    const zh = $$('.lang-pills .seg-tab').find(p => /中文|简体/.test(T(p)));
    if (zh) { click(zh); await sleep(1200); }
    R.backToChinese = T($$('.appnav .appnav-btn')[0]);
});

// ---------- 字体缩放（用真实指针在轨道上拖）----------
await step('uiScaleGeom', async () => {
    const track = $('.ui-scale-track');
    R.scaleBefore = T($('.ui-scale-read'));
    R.trackRect = (() => { const r = track.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; })();
});

// ---------- 端口检测 ----------
await step('portCheck', async () => {
    const b0 = calls().length;
    const btn = $$('.settings-dock .btn').find(b => T(b) === '检测');
    if (!btn) return 'no button';
    click(btn);
    await sleep(900);
    R.portCalls = calls().slice(b0).map(c => c.name);
    R.portNote = T($('.set-notice')) || T($('.rt-warn')) || T($('.port-note'));
});

// ---------- DNS 卡 ----------
await step('dnsCard', async () => {
    click(nav('DNS'));
    await sleep(900);
    const s = slideAt(3);
    R.dnsSlideFound = !!s;
    R.dnsHasSmart = !!(s && /智能优选/.test(T(s)));
    const boxes = s ? $$('input[type=checkbox]', s) : [];
    R.dnsCheckboxCount = boxes.length;
    const smart = boxes.find(b => {
        const f = b.closest('.jf-field');
        return f && /智能优选/.test(T(f));
    });
    if (!smart) return 'no smart switch';
    const b1 = calls().length;
    smart.checked = true;
    smart.dispatchEvent(new Event('change', {bubbles: true}));
    await sleep(2500);
    R.smartCalls = calls().slice(b1).map(c => c.name);
    R.testDNSArgs = calls('TestDNS').map(c => c.args);
    R.notice = T($('.set-notice'));
    R.dnsLatencyInDom = $$('.resolver-row .lat, .res-row').map(e => T(e)).slice(0, 10);
});

// ---------- 规则集卡 ----------
await step('ruleSets', async () => {
    click(nav('规则集'));
    await sleep(1500);
    const s = $$('.set-group.set-slide').find(x => /rs-nav/.test(x.innerHTML || ''));
    R.rsSlideFound = !!s;
    R.rsText = s ? T(s).slice(0, 350) : null;
    R.rsButtons = s ? buttons(s).slice(0, 16) : null;
    const b0 = calls().length;
    const dl = s && $$('.btn', s).find(b => /下载|更新/.test(T(b)));
    if (dl) { click(dl); await sleep(1000); }
    R.rsCalls = calls().slice(b0).map(c => c.name);
});

// ---------- 路由卡 ----------
await step('routing', async () => {
    click(nav('路由规则'));
    await sleep(1000);
    const s = $$('.set-group.set-slide').find(x => x.className.includes('set-routing'));
    R.rtSlideFound = !!s;
    R.rtCards = s ? $$('.rt-card', s).map(c => T(c)) : null;
    // 切到"全部走代理"这张卡
    const proxyCard = s && $$('.rt-card', s).find(c => /全部走代理/.test(T(c)));
    if (proxyCard) {
        const b0 = calls().length;
        click(proxyCard);
        await sleep(1200);
        R.rtCalls = calls().slice(b0).map(c => c.name);
        R.rtActive = $$('.rt-card.active').map(c => T(c));
        R.saveRoutingArgs = calls('SaveRouting').map(c => c.args[0] + ' -> ' + JSON.stringify(c.args[1]).slice(0, 220));
    }
});

R.errs = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
