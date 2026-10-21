// probe-edge.mjs — 前端边界：空订阅、空分组、缺字段的节点、超长名、连点、窄窗口。
const R = {};
const rows = () => $$('.profiles tbody tr[data-pid]');

// ---------- 1) 一个订阅都没有 ----------
await step('noGroups', async () => {
    const keep = window.__mock.groups.slice();
    window.__mock.groups.length = 0;
    window.__mock.emit('groups:changed', 1);
    await sleep(1500);
    R.emptyGroups = {
        rows: rows().length,
        emptyRow: T($('.profiles tbody tr.empty')),
        crash: !!$('.crash'),
        railHasSubs: !!$$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes('所有订阅')),
        errs: errs().map(e => e.text.split('\n')[0]).slice(-3),
    };
    window.__mock.groups.push(...keep);
    window.__mock.emit('groups:changed', 1);
    await sleep(1200);
    R.afterRestore = rows().length;
});

// ---------- 2) 一个空分组（零节点）----------
await step('emptyGroup', async () => {
    const gid = 99;
    window.__mock.groups.push({id: gid, name: '空分组', archive: false, profileCount: 0, url: '', subLastUpdate: 0, info: ''});
    window.__mock.emit('groups:changed', gid);
    await sleep(1500);
    R.emptyGroup = {
        rows: rows().length,
        emptyText: T($('.profiles tbody tr.empty')),
        crash: !!$('.crash'),
    };
    const i = window.__mock.groups.findIndex(g => g.id === gid);
    if (i >= 0) window.__mock.groups.splice(i, 1);
    window.__mock.emit('groups:changed', 1);
    await sleep(1000);
});

// ---------- 3) 缺字段 / 空串 / 超长名的节点 ----------
await step('oddProfile', async () => {
    window.__mock.profiles.push({
        id: 900, gid: 1, type: 'vmess', name: '名字很长的节点' + 'x'.repeat(300),
        address: '', latency: 0, report: '', speed: 0, up: 0, down: 0,
        port: 0, network: '', security: '', tlsNote: '', group: '',
    });
    window.__mock.profiles.push({
        id: 901, gid: 1, type: '', name: '', address: '', latency: -1, report: '',
        speed: -1, up: 0, down: 0, port: 0, network: '', security: '', tlsNote: '', group: '',
    });
    window.__mock.emit('groups:changed', 1);
    await sleep(1500);
    R.oddProfile = {
        rows: rows().length,
        lastRows: rows().slice(-2).map(r => ({
            pid: r.dataset.pid,
            type: T(r.querySelector('td.type')),
            addr: T(r.querySelector('td.addr')),
            port: T(r.querySelector('td.port')),
            lat: T(r.querySelector('td.latency')),
            speed: T(r.querySelector('td.speed')),
            traffic: T(r.querySelector('td.ptraffic')),
            group: T(r.querySelector('td.pgroup')),
        })),
        crash: !!$('.crash'),
        errs: errs().map(e => e.text.split('\n')[0]).slice(-3),
    };
});

// ---------- 4) 搜索框里的正则元字符 ----------
await step('searchMetachars', async () => {
    const search = $('.node-search');
    for (const q of ['(', '.*', '[', '\\', 'a|b', 'hk01']) {
        setVal(search, q);
        await sleep(250);
        R['q_' + q] = {rows: rows().length, crash: !!$('.crash')};
    }
    setVal(search, '');
    await sleep(250);
});

// ---------- 5) 连点同一行（启停竞争）----------
await step('rapidDoubleClick', async () => {
    const b0 = calls().length;
    const r0 = rows()[0];
    for (let i = 0; i < 5; i++) {
        r0.dispatchEvent(new MouseEvent('dblclick', {bubbles: true, cancelable: true, view: window}));
        await sleep(40);
    }
    await sleep(1200);
    R.rapid = {
        calls: calls().slice(b0).map(c => c.name),
        running: $$('.profiles tbody tr.running').map(r => r.dataset.pid),
        crash: !!$('.crash'),
    };
});

// ---------- 6) 窄窗口 ----------
await step('narrowWindow', async () => {
    R.widthBefore = innerWidth;
    window.resizeTo(700, 600);
    await sleep(800);
    R.narrow = {
        innerWidth: innerWidth,
        tableVisible: !!$('.profiles'),
        crash: !!$('.crash'),
        errs: errs().map(e => e.text.split('\n')[0]).slice(-3),
    };
});

R.finalErrs = errs().map(e => e.text.split('\n')[0]);
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
