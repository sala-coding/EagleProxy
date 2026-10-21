// probe-zascale-seed.mjs — leaves behind the state of a second launch.
//
// The type scale is the reader's own size, and it is kept in localStorage. A
// launch that finds one there opens at the reader's size — and the machine's
// own size is still asked for, because the slider's double-click means "back to
// the machine's size".
//
// The mock reads the machine's size from localStorage as well, so the copy of
// the app that starts next sees a machine that is not at 100%. The check is in
// the next probe (probe-zbscale-check), which runs on the launch this one sets
// up: the runner opens the page afresh before every probe.
const R = {};
try {
    localStorage.setItem('mock-sys-scale', '1.25');
    localStorage.setItem('ui-scale', '1.25');
} catch (e) { /* private mode keeps its silence */ }
R.seededScale = localStorage.getItem('ui-scale');
R.seededSysScale = localStorage.getItem('mock-sys-scale');
R.errs = errs();
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
