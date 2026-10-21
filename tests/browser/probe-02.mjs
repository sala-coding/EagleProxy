// probe-02.mjs — what is on screen: the node table, the rail, the status bar.
return JSON.stringify({
    railButtons: buttons($('.appnav')),
    tableHead: tree($('.profiles thead'), 4),
    firstRows: tree($('.profiles tbody'), 5).split('\n').slice(0, 40).join('\n'),
    statusbar: tree($('.statusbar'), 5),
    statusbarText: T($('.statusbar')),
    batchBtn: T($('.batch-wrap')),
    egressText: T($('.egress')),
    errs: errs(),
}, null, 2);
