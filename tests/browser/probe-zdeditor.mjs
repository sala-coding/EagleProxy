// probe-zdeditor.mjs — the node editor taken away by the settings panel must
// take its node back.
//
// "New" creates the node first and opens the editor on it, so that the editor
// knows which node it is editing; if the reader does not save, the node that
// was created for the editor is taken back and no empty shell is left in the
// list.
//
// The settings panel takes the editor away rather than closing it — the modal's
// own close event never fires — so the editor's discard never ran, and the
// empty node stayed in the list for good.
const R = {};
const ctxRow = () => $$('.profiles tbody tr[data-pid]')[0];

// The new node, with the editor open on it.
rclick(ctxRow());
await sleep(300);
const newBtn = $$('.ctxmenu .ctx-item-sub').find(b => T(b) === '新增');
R.newBtnFound = !!newBtn;
if (newBtn) {
    hover(newBtn);
    await sleep(350);
    const vmess = $$('.ctxmenu-sub .ctx-item').find(b => /VMess/.test(T(b)));
    R.protocolFound = !!vmess;
    if (vmess) { click(vmess); await sleep(1400); }
}
R.editorOpen = !!$('.modal');
R.createdCalls = calls('CreateProfile').length;

// Now the settings panel opens, and the editor is taken away with it.
const logo = $('.status-logo');
R.logoFound = !!logo;
if (logo) {
    click(logo);
    await sleep(1500);
}
R.editorAfter = !!$('.modal');
R.settingsOpen = !!$('.settings-dock');
R.deletedCalls = calls('DeleteProfile').length;

if (R.editorOpen && !R.editorAfter) {
    if (R.deletedCalls === 0) {
        console.error('the editor was taken away without its discard running: the node that was created for it is still in the list');
    }
}

R.errs = errs();
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
