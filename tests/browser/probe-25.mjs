// probe-25.mjs — 设置面板停靠位置：新开页面后立即与 3 秒后各测一次。
const R = {};
const rect = e => { if (!e) return null; const r = e.getBoundingClientRect(); return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)}; };
R.window = {w: innerWidth, h: innerHeight};
R.beforeOpen = {dock: rect($('.settings-dock')), app: rect($('.app')), wrap: rect($('.table-wrap')), body: rect($('.body'))};
click($('.status-logo'));
await sleep(300);
R.at300ms = {dock: rect($('.settings-dock')), dockClass: $('.settings-dock')?.className};
await sleep(3000);
R.at3s = {dock: rect($('.settings-dock')), dockClass: $('.settings-dock')?.className};
R.appAt3s = rect($('.app'));
R.wrapAt3s = rect($('.table-wrap'));
R.dockStyle = (() => {
    const d = $('.settings-dock');
    if (!d) return null;
    const cs = getComputedStyle(d);
    return {position: cs.position, left: cs.left, right: cs.right, transform: cs.transform, width: cs.width, zIndex: cs.zIndex, transition: cs.transition};
})();
R.dockParent = (() => {
    const d = $('.settings-dock');
    return d ? {tag: d.parentElement.tagName, cls: d.parentElement.className, parentRect: rect(d.parentElement)} : null;
})();
R.cssVars = (() => {
    const cs = getComputedStyle(document.documentElement);
    return {panelW: cs.getPropertyValue('--panel-w'), railW: cs.getPropertyValue('--rail-w'), statusH: cs.getPropertyValue('--status-h')};
})();
return JSON.stringify(R, null, 2);
