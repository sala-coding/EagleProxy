// probe-theme-state.mjs — 当前主题与高亮的色块。
return JSON.stringify({
    dataTheme: document.documentElement.getAttribute('data-theme'),
    variant: document.documentElement.getAttribute('data-variant'),
    activeSwatch: $$('.theme-swatch.active').map(e => e.dataset.swatch || e.className),
    crash: !!$('.crash'),
}, null, 2);
