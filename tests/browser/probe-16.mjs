// probe-16.mjs — 定位主题色条的几何坐标，供真实指针拖动使用。
await step('open', async () => {
    click($('.status-logo'));
    await sleep(1200);
});
const bars = $$('.theme-line').map(b => {
    const r = b.getBoundingClientRect();
    return {l: Math.round(r.left), t: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height)};
});
return JSON.stringify({bars, theme: document.documentElement.getAttribute('data-theme')}, null, 2);
