// probe-19.mjs — DNS 卡（智能优选/立即测速）、规则集卡、路由卡（中文界面）。
const R = {};
const nav = n => $$('.set-nav-item').find(b => T(b).includes(n));
const slideAt = i => $$('.set-group.set-slide')[i];

await step('open', async () => { click($('.status-logo')); await sleep(1400); });

// ================= DNS 卡 =================
await step('dns', async () => {
    click(nav('DNS'));
    await sleep(1000);
    const s = slideAt(3);
    R.dnsSlideTitle = s ? T(s.querySelector('.set-group-title')) : null;
    R.dnsText = s ? T(s).slice(0, 260) : null;
    // 立即测速按钮（国内 DNS 一侧，不需要节点）
    const testBtns = s ? $$('.btn.tiny', s).filter(b => /测速/.test(T(b))) : [];
    R.testBtnLabels = testBtns.map(b => T(b));
    // 先打开"智能优选"，它会自动跑一轮
    const smart = $$('input[type=checkbox]', s).find(b => {
        const f = b.closest('.jf-field');
        return f && /智能优选/.test(T(f));
    });
    R.smartFound = !!smart;
    const b1 = calls().length;
    if (smart) {
        smart.checked = true;
        smart.dispatchEvent(new Event('change', {bubbles: true}));
        await sleep(3000);
    }
    R.smartCalls = calls().slice(b1).map(c => c.name);
    R.testDNSArgs = calls('TestDNS').map(c => c.args);
    R.setNotice = T($('.set-notice'));
    R.saveRoutingCalls = calls('SaveRouting').map(c => c.args[0] + ' ' + JSON.stringify(c.args[1]).slice(0, 200));
    R.remoteDnsField = (() => { const i = $('.rt-input[placeholder*="dns.google"]'); return i ? i.value : null; })();
    // 再点"立即测速"
    const b2 = calls().length;
    const tb = $$('.btn.tiny', s).find(b => /测速/.test(T(b)));
    if (tb) { click(tb); await sleep(2500); }
    R.measureCalls = calls().slice(b2).map(c => c.name);
});

// ================= 规则集卡 =================
await step('ruleSets', async () => {
    click(nav('规则集'));
    await sleep(1500);
    const s = $$('.set-group.set-slide').find(x => /rs-nav/.test(x.innerHTML || ''));
    R.rsSlideFound = !!s;
    R.rsText = s ? T(s).slice(0, 300) : null;
    R.rsButtons = s ? buttons(s).slice(0, 16) : null;
    const b0 = calls().length;
    const dl = s && $$('.btn', s).find(b => /下载|更新/.test(T(b)));
    R.dlBtn = dl ? T(dl) : null;
    if (dl) { click(dl); await sleep(1200); }
    R.rsCalls = calls().slice(b0).map(c => c.name);
    // 展开一个分类
    const tog = s && $('.rs-nav-toggle', s);
    if (tog) { click(tog); await sleep(700); }
    R.afterToggle = s ? T(s).slice(0, 300) : null;
});

// ================= 路由卡 =================
await step('routing', async () => {
    click(nav('路由规则'));
    await sleep(1200);
    const s = $$('.set-group.set-slide').find(x => x.className.includes('set-routing'));
    R.rtSlideFound = !!s;
    R.rtCards = s ? $$('.rt-card', s).map(c => T(c)) : null;
    const proxyCard = s && $$('.rt-card', s).find(c => /全部走代理/.test(T(c)));
    if (proxyCard) {
        const b0 = calls().length;
        click(proxyCard);
        await sleep(1500);
        R.rtCalls = calls().slice(b0).map(c => c.name);
        R.rtActive = $$('.rt-card.active').map(c => T(c));
        R.saveRouting = calls('SaveRouting').map(c => c.args[0] + ' ' + JSON.stringify(c.args[1]).slice(0, 260));
    }
    // 改一个出站列表
    const ta = $$('.set-routing textarea')[0];
    if (ta) {
        const b1 = calls().length;
        setVal(ta, 'geosite:cn\nexample.com');
        await sleep(1600);
        R.listCalls = calls().slice(b1).map(c => c.name);
        R.listSave = calls('SaveRouting').map(c => JSON.stringify(c.args[1]).slice(0, 200));
    }
});

R.errs = errs().map(e => e.text.split('\n')[0]);
return JSON.stringify(R, null, 2);
