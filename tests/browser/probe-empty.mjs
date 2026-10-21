// probe-empty.mjs — 删除最后一个订阅之后的空状态；窄窗口。
const R = {};
const rail = n => $$('.appnav .appnav-btn').find(b => (b.getAttribute('aria-label') || '').includes(n));
const rows = () => $$('.profiles tbody tr[data-pid]');

// ---------- 删掉最后一个订阅 ----------
await step('deleteLastSub', async () => {
    // 先只留一个订阅
    const keep = window.__mock.groups.slice();
    while (window.__mock.groups.length > 1) window.__mock.groups.pop();
    window.__mock.emit('groups:changed', window.__mock.groups[0].id);
    await sleep(1200);
    R.before = {groups: window.__mock.groups.length, rows: rows().length};

    click(rail('所有订阅'));
    await sleep(900);
    const card = $$('.subdrop-table tr.sub-card')[0];
    const del = card && $$('.sub-act', card).find(b => b.className.includes('danger'));
    R.delBtnFound = !!del;
    if (del) {
        click(del);
        await sleep(900);
        R.afterDelClick = {
            modal: !!$('.modal'),
            modalText: T($('.modal')).slice(0, 200),
            buttons: buttons($('.modal')),
        };
        // 确认删除
        const okBtn = $$('.modal .btn').find(b => /删除|确定|确认/.test(T(b)));
        R.confirmBtn = okBtn ? T(okBtn) : null;
        if (okBtn) {
            click(okBtn);
            await sleep(1500);
        }
    }
    R.after = {
        groups: window.__mock.groups.length,
        rows: rows().length,
        emptyCell: T($('.profiles tbody td.empty')),
        emptyRowText: T($('.profiles tbody')),
        railSubs: !!rail('所有订阅'),
        crash: !!$('.crash'),
        errs: errs().map(e => e.text.split('\n')[0]).slice(-4),
    };
    // 还原
    window.__mock.groups.push(...keep.slice(1));
    window.__mock.emit('groups:changed', window.__mock.groups[0].id);
    await sleep(1000);
});

// ---------- 窄窗口 ----------
await step('narrow', async () => {
    R.before = innerWidth;
});
R.finalErrs = errs().map(e => e.text.split('\n')[0]);
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
