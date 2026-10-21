// probe-zclegend.mjs — the log legend's "live" dot must go still for every
// category, not only for the one that streamed last.
//
// The dot breathes while a category's lines are arriving and goes still a
// moment after they stop. The breath is per category, and each one runs its own
// course: the timers were cleared by the log stream's own watcher whenever it
// ran again, and the watcher runs again for the next batch — so the timer that
// would have put a dot out was thrown away with it, and that category's dot
// stayed lit for good. With two categories streaming at once — the core's
// lines and a speed test's — one of them could never go still.
const R = {};

const logBtn = $('.appnav-btn[aria-label*="日志"], .appnav-btn[aria-label*="log" i]');
R.logBtnFound = !!logBtn;
if (logBtn) {
    click(logBtn);
    await sleep(700);
}
R.panelOpen = !!$('.console');

const legend = () => $$('.log-key').map(e => ({
    id: (e.className.match(/cat-([a-z\-]+)/) || [])[1],
    live: e.classList.contains('live'),
}));

// Two categories, in two batches — the core's lines and a speed test's.
//
// The lines carry no timestamp on purpose: the merged stream is ordered by
// the time a line carries, and a stamped line would sort in front of the
// lines the panel already holds instead of behind them — the new line would
// then land in the middle of the list, and the category it belongs to would
// never be read as fresh.
window.__mock.emit('core:log', 'a core line', 'core');
await sleep(500);
window.__mock.emit('core:log', 'a speedtest line', 'speedtest-url');
await sleep(2400);          // well past the 1.5s a dot is given to settle

R.legend = legend();
const still = id => {
    const e = legend().find(x => x.id === id);
    return e ? e.live === false : null;
};
R.coreStill = still('core');
R.speedtestStill = still('speedtest-url');
if (R.coreStill === false) {
    console.error('the core category\'s dot is still lit long after its lines stopped');
}
if (R.speedtestStill === false) {
    console.error('the speedtest category\'s dot is still lit long after its lines stopped');
}

R.errs = errs();
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
