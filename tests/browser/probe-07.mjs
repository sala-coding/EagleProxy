// probe-07.mjs — 启动节点、系统代理/TUN 开关、批量操作、停止。
const R = {};

// --- 双击启动节点 ---
const row = $$('.profiles tbody tr[data-pid]')[0];
row.dispatchEvent(new MouseEvent('dblclick', {bubbles: true, cancelable: true, view: window}));
await sleep(800);
R.startCall = lastCall('StartProfile');
R.runningRow = $$('.profiles tbody tr.running').map(r => r.dataset.pid);
R.stateChip = $('.state-chip')?.className;
R.statusNote = T($('.status-note'));

// --- 系统代理开关（此时已有节点在跑）---
const spBtn = $$('.appnav .appnav-btn.switch')[0];
click(spBtn);
await sleep(900);
R.spPressed = spBtn?.getAttribute('aria-pressed');
R.setSPCall = lastCall('SetSystemProxyMode');
R.getSPModeCall = lastCall('GetSPMode');
R.stateChipAfterSP = $('.state-chip')?.className;

// 再点一次关闭
click(spBtn);
await sleep(900);
R.spPressedAfter = spBtn?.getAttribute('aria-pressed');
R.setSPCall2 = lastCall('SetSystemProxyMode');

// --- TUN 开关 ---
const tunBtn = $$('.appnav .appnav-btn.switch')[1];
click(tunBtn);
await sleep(900);
R.tunPressed = tunBtn?.getAttribute('aria-pressed');
R.setVPNCall = lastCall('SetVPNMode');
R.stateChipAfterTUN = $('.state-chip')?.className;
click(tunBtn);
await sleep(900);

// --- 批量：去重 ---
click($('.batch-wrap .egress-trigger'));
await sleep(250);
const dedupe = byText('.batch-wrap .egress-menu button', '去除重复');
const b0 = calls().length;
click(dedupe);
await sleep(700);
R.batchCalls = calls().slice(b0).map(c => c.name);
R.batchNote = T($('.status-note'));

// --- 停止节点 ---
const stopBtn = byText('button', '停止');
R.stopBtnFound = !!stopBtn;
if (stopBtn) {
    click(stopBtn);
    await sleep(800);
    R.stopCall = lastCall('StopProfile');
    R.runningAfterStop = $$('.profiles tbody tr.running').map(r => r.dataset.pid);
}

R.errs = errs();
return JSON.stringify(R, null, 2);
