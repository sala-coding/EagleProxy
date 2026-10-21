// probe-zbscale-check.mjs — the type scale on a launch that finds a size of the
// reader's own already written down.
//
// This is the second launch, and it is where the scale went wrong:
//
//   - the machine's own size was asked for only when there was no size of the
//     reader's own to start from. Once a size was stored, the machine was never
//     asked again, and the slider's double-click — "back to default" — went
//     back to 100% instead of to the machine's size, for good;
//   - the machine's answer was written into localStorage as if the reader had
//     chosen it, which is what made the next launch start at the machine's
//     size and take the machine's size away.
const R = {};

// The machine is asked for its size on this launch too, even though a size of
// the reader's own is stored.
R.sysScaleCalls = calls('SystemFontScale').length;
if (R.sysScaleCalls === 0) {
    console.error('the machine was never asked for its text size on a launch that had a stored size: the slider\'s "back to default" has nothing to go back to');
}

// The stored size is the reader's own, and it is what the window opens at.
R.storedBefore = localStorage.getItem('ui-scale');

const logo = $('.status-logo');
R.logoFound = !!logo;
if (logo) {
    click(logo);
    await sleep(1400);
}
R.settingsOpen = !!$('.settings-dock');

const track = $('.ui-scale-track');
R.trackFound = !!track;
if (track) {
    // The double-click is "back to default", and the default is the machine's
    // own size.
    track.dispatchEvent(new MouseEvent('dblclick', {bubbles: true, cancelable: true, view: window}));
    await sleep(700);
}
const read = $('.ui-scale-read');
R.readout = read ? (read.textContent || '').trim() : null;
if (!/125/.test(R.readout || '')) {
    console.error('the scale slider\'s "back to default" gave ' + JSON.stringify(R.readout) + ', want the machine\'s own size (125%)');
}

// The machine's size is a default, not a choice of the reader's, so it is not
// written down as one.
R.storedAfter = localStorage.getItem('ui-scale');
if (R.storedAfter !== null) {
    console.error('the machine\'s own size was written into localStorage as the reader\'s choice: ' + R.storedAfter);
}

R.errs = errs();
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
