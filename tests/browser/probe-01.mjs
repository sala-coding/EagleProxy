// probe-01.mjs — first look: did the app boot, and what does the page look like?
const out = {
    title: document.title,
    hasApp: !!document.getElementById('app'),
    appChildren: document.getElementById('app')?.children.length ?? 0,
    hasMock: !!window.__mock,
    hasGo: !!window.go?.main?.App,
    bodyClass: document.body.className,
    topClasses: [...document.querySelectorAll('#app > *')].map(e => e.className),
    // the whole visible tree, one line per element, to see the layout
    tree: (() => {
        const lines = [];
        const walk = (el, depth) => {
            if (depth > 6) return;
            for (const c of el.children) {
                const cls = (c.className && typeof c.className === 'string') ? '.' + c.className.trim().split(/\s+/).join('.') : '';
                const txt = (c.children.length === 0 ? (c.textContent || '').trim() : '').slice(0, 40);
                lines.push('  '.repeat(depth) + '<' + c.tagName.toLowerCase() + cls + '>' + (txt ? ' ' + txt : ''));
                walk(c, depth + 1);
            }
        };
        walk(document.getElementById('app') || document.body, 0);
        return lines.slice(0, 200).join('\n');
    })(),
    errors: (window.__err || []).slice(0, 20),
};
return JSON.stringify(out, null, 2);
