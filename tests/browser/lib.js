// lib.js — shared helpers for the harness probes. Prepended to every `run` file.
const T = (el) => (el && (el.innerText || el.textContent) || '').replace(/\s+/g, ' ').trim();
const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];
const vis = (el) => !!(el && el.getClientRects().length);
const byText = (sel, text, root = document) =>
    $$(sel, root).find(e => (e.innerText || e.textContent || '').replace(/\s+/g, ' ').trim().includes(text));

function click(el) {
    if (!el) throw new Error('click: element not found');
    el.scrollIntoView({block: 'center'});
    ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'].forEach(t =>
        el.dispatchEvent(new MouseEvent(t, {bubbles: true, cancelable: true, view: window, button: 0})));
    return el;
}
function rclick(el) {
    if (!el) throw new Error('rclick: element not found');
    el.dispatchEvent(new MouseEvent('contextmenu', {bubbles: true, cancelable: true, view: window, button: 2}));
    return el;
}
function hover(el) {
    if (!el) throw new Error('hover: element not found');
    el.dispatchEvent(new MouseEvent('mouseenter', {bubbles: false, view: window}));
    el.dispatchEvent(new MouseEvent('mouseover', {bubbles: true, view: window}));
    return el;
}
function setVal(el, v) {
    if (!el) throw new Error('setVal: element not found');
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype
        : el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
    setter.call(el, v);
    el.dispatchEvent(new Event('input', {bubbles: true}));
    el.dispatchEvent(new Event('change', {bubbles: true}));
    return el;
}
const sleep = ms => new Promise(r => setTimeout(r, ms));
async function waitFor(fn, ms = 3000, step = 60) {
    const end = Date.now() + ms;
    while (Date.now() < end) {
        let v;
        try { v = fn(); } catch (e) { v = false; }
        if (v) return v;
        await sleep(step);
    }
    return false;
}
// The mock backend's call log — what the UI asked the backend for.
const calls = (name) => (window.__mock.calls || []).filter(c => !name || c.name === name);
const lastCall = (name) => {
    const l = calls(name);
    return l.length ? l[l.length - 1] : null;
};
const errs = () => (window.__err || []).slice();
// step runs one probe action and records what went wrong instead of letting the
// whole probe die: a missing button is a finding, not a reason to stop looking.
async function step(name, fn) {
    try {
        const v = await fn();
        return {name, ok: true, value: v};
    } catch (e) {
        return {name, ok: false, error: String(e && e.message || e)};
    }
}
// A compact map of what is on screen, so a probe can assert on the visible state.
function tree(root = document.getElementById('app'), maxDepth = 7, max = 400) {
    if (!root) return '(no such element)';
    const lines = [];
    const walk = (el, d) => {
        if (d > maxDepth || lines.length >= max) return;
        for (const c of el.children) {
            const cls = (typeof c.className === 'string' && c.className) ? '.' + c.className.trim().split(/\s+/).join('.') : '';
            const leaf = c.children.length === 0;
            const t = leaf ? T(c).slice(0, 60) : '';
            const dis = vis(c) ? '' : ' (hidden)';
            lines.push('  '.repeat(d) + '<' + c.tagName.toLowerCase() + cls + '>' + (t ? ' ' + t : '') + dis);
            walk(c, d + 1);
        }
    };
    walk(root, 0);
    return lines.join('\n');
}
// A list of the clickable things on screen, by their visible name.
function buttons(root = document) {
    if (!root) return [];
    return $$(`button, a[href], [role=button], .rowmenu-item, .menu-item, [tabindex="0"]`, root)
        .filter(vis)
        .map((e, i) => `${i}: <${e.tagName.toLowerCase()} class="${typeof e.className === 'string' ? e.className : ''}"> ${T(e).slice(0, 50)}`);
}
