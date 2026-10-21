// probe-04.mjs — 状态栏菜单（出站模式 / 批量）、导航栏开关、行右键菜单。
const R = {};

// ---- 出站模式菜单 ----
const egressBtn = $('.egress .egress-trigger');
R.egressLabel0 = T(egressBtn);
click(egressBtn);
await sleep(250);
R.egressMenuItems = $$('.egress .egress-menu button').map(b => ({text: T(b), sel: b.classList.contains('on')}));
// pick 直连 (direct)
const direct = byText('.egress .egress-menu button', '直连');
R.hasDirect = !!direct;
if (direct) {
    click(direct);
    await sleep(400);
}
R.egressLabel1 = T($('.egress .egress-trigger'));
R.egressMode = $('.egress')?.dataset.mode;
R.setEgressCalls = calls('SetEgressMode');

// 重新打开菜单确认高亮跟随
click($('.egress .egress-trigger'));
await sleep(250);
R.egressMenuAfter = $$('.egress .egress-menu button').map(b => ({text: T(b), sel: b.classList.contains('on')}));
// 关掉菜单
document.body.dispatchEvent(new MouseEvent('mousedown', {bubbles: true}));
await sleep(200);
R.egressMenuClosed = $$('.egress .egress-menu').length === 0;

// ---- 批量菜单 ----
const batchBtn = $('.batch-wrap .egress-trigger');
R.batchLabel = T(batchBtn);
click(batchBtn);
await sleep(250);
R.batchItems = $$('.batch-wrap .egress-menu button').map(b => T(b));
R.batchMenuRole = $('.batch-wrap .egress-menu')?.getAttribute('role');
document.body.dispatchEvent(new MouseEvent('mousedown', {bubbles: true}));
await sleep(200);
R.batchClosed = $$('.batch-wrap .egress-menu').length === 0;

// ---- 导航栏：系统代理开关 ----
const spBtn = $$('.appnav .appnav-btn.switch')[0];
const tunBtn = $$('.appnav .appnav-btn.switch')[1];
R.spTitle = spBtn?.getAttribute('aria-label');
R.tunTitle = tunBtn?.getAttribute('aria-label');
click(spBtn);
await sleep(600);
R.spPressed = spBtn?.getAttribute('aria-pressed');
R.setSPCalls = calls('SetSystemProxyMode');
R.stateChipAfterSP = $('.state-chip')?.className;

click(tunBtn);
await sleep(600);
R.tunPressed = tunBtn?.getAttribute('aria-pressed');
R.setVPNCalls = calls('SetVPNMode');
R.stateChipAfterTUN = $('.state-chip')?.className;
// 复位
click(spBtn); await sleep(400);
click(tunBtn); await sleep(400);

// ---- 行右键菜单 ----
const row = $$('.profiles tbody tr[data-pid]')[1];
rclick(row);
await sleep(300);
R.ctxOpen = !!$('.rowmenu, .ctx-menu, .nodemenu');
R.ctxClass = $('.rowmenu, .ctx-menu, .nodemenu')?.className;
R.ctxItems = $$('.rowmenu button, .ctx-menu button, .nodemenu button, [role=menu] button').map(b => T(b));
R.ctxTree = tree($('.rowmenu, .ctx-menu, .nodemenu, [role=menu]'), 4, 80);

R.errs = errs();
return JSON.stringify(R, null, 2);
