// probe-26.mjs — 定位字体缩放轨道（确保停在第一张卡）。
const R = {};
try { localStorage.removeItem('settings-slide'); } catch (e) {}
click($('.status-logo'));
await sleep(1600);
const track = $('.ui-scale-track');
const r = track.getBoundingClientRect();
R.track = {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)};
R.activeSlide = $$('.set-group.set-slide.on').map(e => e.className);
R.scaleBefore = T($('.ui-scale-read'));
R.themeCells = $$('.theme-cells').map(c => { const q = c.getBoundingClientRect(); return {l: Math.round(q.left), t: Math.round(q.top), w: Math.round(q.width), h: Math.round(q.height)}; });
return JSON.stringify(R, null, 2);
