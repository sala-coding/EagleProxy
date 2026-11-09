// The interface sizes the settings offer, as a multiplier on the root font size — the one
// number every dimension of type in the app is measured against (see style.css). It is an
// offset on the window's own curve rather than a replacement for it, which is what lets
// "the text follows the window" and "the text suits my eyes" both be true at once.
// The range the type scale is dragged over, and the step. The ends are the same ones a
// stored value is clamped to when it is read back (see uiScale in App.vue), so the
// slider can never write a size the reader cannot return from; the step is fine enough
// to land on a size that suits and coarse enough that every stop is visibly different.
export const UI_SCALE_MIN = 0.7;
export const UI_SCALE_MAX = 1.5;
export const UI_SCALE_STEP = 0.05;

// The same range cut into the cells the control is drawn as: one cell per step, so the
// number of lit cells and the size they stand for are one fact told once. The control
// can therefore never show a size it does not set.
export const UI_SCALE_CELLS = Math.round((UI_SCALE_MAX - UI_SCALE_MIN) / UI_SCALE_STEP) + 1;

// Which cell a size falls on, and the size a cell stands for. Both directions are
// needed: the row is drawn from the value, and the pointer's cell becomes the value.
export const scaleIndex = (v: number) => Math.round((v - UI_SCALE_MIN) / UI_SCALE_STEP);
export const scaleFromIndex = (i: number) =>
    Math.min(UI_SCALE_MAX, Math.max(UI_SCALE_MIN, UI_SCALE_MIN + i * UI_SCALE_STEP));
// The system's answer, clamped into the slider's range: a 175% display would
// frankly ask too much of a card that was designed around 100–150%, and the
// reader still has the slider for the last mile.
export const clampScale = (v: number) => Math.min(UI_SCALE_MAX, Math.max(UI_SCALE_MIN, v));

// The hue a picked colour stands for, in the units buildCustomPalette takes. The
// picker hands back any colour at all; a palette is derived from one hue and one
// ground, so the pick is read for where it sits on the wheel and nothing else —
// its own lightness and saturation belong to the ground and to the derived ladder,
// and taking them as well would be a second opinion about the same thing.
export function hueOf(hex: string): number {
    const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
    if (!m) return 210;
    const n = parseInt(m[1], 16);
    const r = (n >> 16 & 255) / 255, g = (n >> 8 & 255) / 255, b = (n & 255) / 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
    if (d === 0) return 210;
    const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    return Math.round((h * 60 + 360) % 360);
}

// The colour the picker opens on, from a hue: the accent's own lightness, so the
// field shows the palette's accent rather than an unrelated chip of the wheel.
export function hueHex(h: number): string {
    const s = 0.62, l = 0.62;
    const k = (n: number) => (n + h / 30) % 12;
    const a = s * Math.min(l, 1 - l);
    const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
    const hex = (v: number) => Math.round(v * 255).toString(16).padStart(2, '0');
    return `#${hex(f(0))}${hex(f(8))}${hex(f(4))}`;
}


// The palettes the app ships, in the order the picker shows them. Two things read
// this list and nothing else duplicates it: the index is what the settings file's
// `theme` key holds (the first three are NekoRay's own numbering, so a file written
// here still opens there), and the id is what [data-theme] selects in App.css. A
// label that looks like a key is translated; the rest are the palettes' own names,
// which do not translate. `tone` is the ground the palette's colour-family reads as
// — the label the settings row groups it under — not its brightness: the pale
// entries sit beside the deep ones of their own family, and which of them pools the
// light-theme special cases is LIGHT_THEMES's business, below.
export const THEMES: {id: string; label: string; tone: 'cool' | 'warm' | 'custom'}[] = [
    {id: 'dark', label: 'settings.theme.dark', tone: 'cool'},
    {id: 'light', label: 'settings.theme.light', tone: 'cool'},
    {id: 'nord', label: 'Nord', tone: 'cool'},
    {id: 'dracula', label: 'Dracula', tone: 'cool'},
    {id: 'one-dark', label: 'One Dark', tone: 'cool'},
    {id: 'gruvbox', label: 'Gruvbox', tone: 'warm'},
    // Appended, never spliced in: the settings file indexes this array by position,
    // and moving an entry today would turn every theme a reader already picked into
    // a different palette tomorrow.
    // The cool five beside the numbered heads: blues, teals and ink. Trimmed so both
    // tone rows sit level at nine a-side — the four cut (Midnight, Ayu Mirage,
    // Kanagawa, Basalt) were the least own-kind of the eight — with THEME_REMAP
    // below carrying the settings-file numbers of the cut ones over to the kept
    // palette nearest in kind.
    {id: 'tokyo-night', label: 'Tokyo Night', tone: 'cool'},
    {id: 'catppuccin', label: 'Catppuccin', tone: 'cool'},
    {id: 'oceanic', label: 'Oceanic', tone: 'cool'},
    {id: 'aurora', label: 'Aurora', tone: 'cool'},
    {id: 'everforest', label: 'Everforest', tone: 'warm'},
    {id: 'coffee', label: 'Coffee', tone: 'warm'},
    {id: 'sunset', label: 'Sunset', tone: 'warm'},
    {id: 'clay', label: 'Clay', tone: 'warm'},
    {id: 'ember', label: 'Ember', tone: 'warm'},
    {id: 'bordeaux', label: 'Bordeaux', tone: 'warm'},
    {id: 'amber', label: 'Amber', tone: 'warm'},
    {id: 'cinnamon', label: 'Cinnamon', tone: 'warm'},
    // The ninth slot on neither row is the reader's own: a hue and a ground, turned
    // into a full palette by buildCustomPalette (see the effect that wears it). The
    // id is its own place in this array, indexed by the settings file like the rest.
    {id: 'custom', label: 'settings.theme.custom', tone: 'custom'},
];

// The pale palettes. App.css has one set of rules — hovers, edges, the log panel's
// zebra and the like — tuned for a light ground, and this set is what they pool
// under: the theme effect writes data-variant from it (below), and App.css keys the
// shared arithmetic on that attribute (see [data-variant="light"] there), so a pale
// palette is only its own set of values above ground it did not have to restate.
// One pale palette is all each tone keeps — light on the cool side, and none on
// the warm one: a row of themes that read as white paper in a card of dark discs
// was a row the eye could not walk down.
export const LIGHT_THEMES = new Set(['light']);

// The numbers the settings file once wrote for palettes the tone rows have since
// shed, carried over to the kept palette nearest in kind. Read once where the file
// is opened (see the opening effect below), never again: after that load the file
// itself is rewritten with the new number, and the table is history. Numbers not
// listed kept their places and need no lift.
// ---- the reader's own palette ----
// A custom theme is not a second stylesheet and not a form full of hex codes: it
// is two choices — a hue and a ground — and everything a full palette needs is
// derived from those two. The derived set is written to localStorage once, at
// Save, from the popover in SettingsDialog (see buildCustomPalette there); the
// effect that wears themes reads it and puts it on the document as inline
// variables, and the settings file only ever stores the number the custom entry
// holds in THEMES, exactly like every other palette.

// The one footprint a worn palette leaves on the document is this handful of
// inline custom properties; the effect needs the same list to take them off
// again when another theme is picked.
export const CUSTOM_VARS = ['--bg', '--bg-2', '--bg-3', '--line', '--line-strong', '--text',
    '--text-dim', '--accent', '--accent-soft', '--accent-edge', '--on-fill',
    '--green', '--red', '--amber', '--green-bg', '--green-bg-hover',
    '--lat-1', '--lat-2', '--lat-3', '--lat-4', '--lat-5'];

// What the popover stores: the two choices and the derived set, every value a
// finished string ready for style.setProperty. Derivation runs once, at Save;
// everything that touches a worn palette afterwards — first paint, the ground
// arithmetic, its own swatch — takes a value straight out of the box.
export type CustomPalette = Record<string, string>;

// The coefficient the palette was built on, riding in the set as a key of its
// own (never painted: the wearing effect hands every key from CUSTOM_VARS, and
// this is not in it). It is what the ground attribute resolves to while the
// custom palette is on.
export const CUSTOM_GROUND = '--custom-ground';

// The hue the palette was built from, kept the same way: it is what the picker
// opens on, so a saved palette comes back to the colour it was made with.
export const CUSTOM_HUE = '--custom-hue';

// The picked colour itself, in hex, kept beside the hue: the field reopens on the
// colour the window is actually wearing rather than on a reconstruction of it.
export const CUSTOM_COLOR = '--custom-color';

// The derivation that made a stored set, kept in it so an older one is re-made
// instead of worn: the ink a palette carries is the derivation's, not the reader's,
// and a set saved before the ink was whitened would keep showing the old grey.
export const CUSTOM_VERSION = '--custom-version';
export const CUSTOM_VERSION_NOW = '5';

// The three numbers the derivation works in. All of them are used: the hue tints the
// surfaces, and the picked colour's own saturation and lightness drive the accent — so
// a muted slate and a vivid blue are two different palettes, not one hue family.
export function toneOf(hex: string): {h: number; s: number; l: number} {
    const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
    if (!m) return {h: 210, s: 62, l: 62};
    const n = parseInt(m[1], 16);
    const r = (n >> 16 & 255) / 255, g = (n >> 8 & 255) / 255, b = (n & 255) / 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
    const l = (max + min) / 2;
    if (d === 0) return {h: 210, s: 0, l: Math.round(l * 100)};
    const sat = d / (1 - Math.abs(2 * l - 1));
    const raw = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    return {
        h: Math.round((raw * 60 + 360) % 360),
        s: Math.round(Math.min(1, sat) * 100),
        l: Math.round(l * 100),
    };
}

// hsl → #rrggbb, for the record of the colour a palette was made from.
export function hexOf(h: number, s: number, l: number): string {
    const sn = s / 100, ln = l / 100;
    const k = (n: number) => (n + h / 30) % 12;
    const a = sn * Math.min(ln, 1 - ln);
    const f = (n: number) => ln - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
    const hex = (v: number) => Math.round(v * 255).toString(16).padStart(2, '0');
    // With the `#`: this string is the record of the picked colour and goes back
    // into the picker, which reads only a #rrggbb — a bare six digits fell back to
    // the derived accent, so the field never showed the colour it was made with.
    return `#${hex(f(0))}${hex(f(8))}${hex(f(4))}`;
}

// The colour the picker opens on, out of a stored palette: the record kept beside
// the hue when there is one, and the ground otherwise — --bg *is* the picked colour,
// so a palette saved before the record existed still opens on the colour it was made
// with rather than on the accent derived from it.
export function pickedColor(p: CustomPalette): string {
    const c = p[CUSTOM_COLOR];
    if (typeof c === 'string' && /^#?[0-9a-f]{6}$/i.test(c)) return c.startsWith('#') ? c : `#${c}`;
    const m = /hsl\((\d+),\s*(\d+)%,\s*(\d+)%\)/.exec(p['--bg'] ?? '');
    return m ? hexOf(Number(m[1]), Number(m[2]), Number(m[3])) : '';
}

// A colour is the whole palette. The picked colour is the ground itself — not a hue
// it is distilled down to and then pinned at a fixed lightness — and everything drawn
// on it is derived from how light that colour is: a light colour gets dark ink, a dark
// one gets light ink, and the two secondary surfaces step away from it rather than
// being a second guess at it.
//
// What the colour cannot supply is an accent legible on top of itself, so the accent is
// the same hue pushed away from the ground: lighter on a dark ground, darker on a pale
// one. The signal colours and the latency scale keep the neutral legs the built-in
// palettes use — a clock red has to stay believable in every palette, so that is not
// the place for cleverness.
export function buildCustomPalette(tone: string | {h: number; s: number; l: number}): CustomPalette {
    const {h, s, l} = typeof tone === 'string' ? toneOf(tone) : tone;
    const pale = l >= 55;
    const pc = (v: number) => Math.min(100, Math.max(0, Math.round(v)));
    // A surface `step` away from the ground: down when the ground is pale, up when it
    // is dark, so both "raised" surfaces read as raised.
    const surf = (step: number, sat = 1) => `hsl(${h}, ${pc(s * sat)}%, ${pc(l + (pale ? -step : step))}%)`;
    const acc = {h, s: pc(Math.max(45, s)), l: pc(pale ? Math.max(18, l - 32) : Math.min(88, l + 30))};
    const accent = `hsl(${acc.h}, ${acc.s}%, ${acc.l}%)`;
    const core: CustomPalette = {
        '--bg': `hsl(${h}, ${pc(s)}%, ${pc(l)}%)`,
        '--bg-2': surf(4),
        '--bg-3': surf(8),
        '--line': surf(12, 0.7),
        '--line-strong': surf(20, 0.85),
        // The deep side's ink, near-neutrally coloured: the picked saturation only
        // tints it, because a saturated blue at high lightness reads as grey-blue
        // rather than as white. 92% is the plain reading — 97 was the correction
        // overshooting into "too white".
        '--text': `hsl(${h}, ${pc(Math.min(s, 6))}%, ${pale ? 12 : 92}%)`,
        '--text-dim': `hsl(${h}, ${pc(Math.min(s, 10))}%, ${pale ? 38 : 76}%)`,
        '--accent': accent,
        '--accent-soft': `hsla(${acc.h}, ${acc.s}%, ${acc.l}%, ${pale ? 0.14 : 0.18})`,
        '--accent-edge': `hsla(${acc.h}, ${acc.s}%, ${acc.l}%, ${pale ? 0.45 : 0.50})`,
        // What sits on a filled control — chosen from the accent's own lightness, not
        // from the ground's: on a very dark ground the accent lands near 37%, and the
        // dark ink that belongs on a pale fill read as two greys on it (the "淡蓝底、
        // 字看不清" case). A fill light enough to carry dark ink gets it; everything
        // darker gets white.
        '--on-fill': acc.l >= 60 ? `hsl(${h}, ${pc(Math.min(s, 25))}%, 10%)` : '#ffffff',
        '--green': pale ? '#279c56' : '#4fd489',
        '--red': pale ? '#d64545' : '#ff6b6b',
        '--amber': pale ? '#c98a00' : '#ffc266',
        '--green-bg': pale ? 'rgba(39, 156, 86, 0.14)' : 'rgba(79, 212, 137, 0.16)',
        '--green-bg-hover': pale ? 'rgba(39, 156, 86, 0.20)' : 'rgba(79, 212, 137, 0.24)',
        '--lat-1': pale ? '#279c56' : '#4fd489',
        '--lat-2': pale ? '#4aa96a' : '#6fe0a5',
        '--lat-3': pale ? '#c98a00' : '#ffc266',
        '--lat-4': pale ? '#cf6a1e' : '#ff8a5c',
        '--lat-5': pale ? '#d64545' : '#ff6b6b',
    };
    // The ground attribute the rest of the app keys its light/dark arithmetic on, taken
    // from the colour rather than asked for separately.
    core[CUSTOM_GROUND] = pale ? 'pale' : 'deep';
    core[CUSTOM_HUE] = String(h);
    core[CUSTOM_COLOR] = hexOf(h, s, l);
    core[CUSTOM_VERSION] = CUSTOM_VERSION_NOW;
    return core;
}

// The stored answer, or null where nothing was saved yet: the swatch wears its
// empty uniform until the reader has made one.
export function readCustomPalette(): CustomPalette | null {
    try {
        const raw = window.localStorage.getItem('custom-palette');
        const p = raw ? JSON.parse(raw) as CustomPalette : null;
        if (!p || typeof p['--bg'] !== 'string') return null;
        if (p[CUSTOM_VERSION] === CUSTOM_VERSION_NOW) return p;
        // Rebuilt from the colour it was made with, so nothing the reader chose is
        // lost — only the derivation that turns it into surfaces and ink is new.
        const rebuilt = buildCustomPalette(pickedColor(p) || p['--bg']);
        window.localStorage.setItem('custom-palette', JSON.stringify(rebuilt));
        return rebuilt;
    } catch {
        return null;
    }
}

// Wearing: put the stored palette on the document as inline variables and
// stamp the ground attribute from it. Returns false when nothing is stored
// yet, so the caller can fall through to the built-in palettes
// arithmetic. Effect calls it on 'custom'; Save calls it again when the
// custom theme is already the one being worn — a re-derived set has to land
// even though the theme itself did not change.
export function wearCustom(): boolean {
    const p = readCustomPalette();
    if (!p) return false;
    for (const k of CUSTOM_VARS) {
        const v = p[k];
        if (typeof v === 'string') document.documentElement.style.setProperty(k, v);
    }
    document.documentElement.setAttribute('data-variant', p[CUSTOM_GROUND] === 'pale' ? 'light' : 'dark');
    document.documentElement.setAttribute('data-theme', 'custom');
    return true;
}

// ---- the hover surface ----
// The colour a hovered row, menu item or button paints. One variable for the whole
// app (--hover-bg, see 01-theme.css), defaulted to the theme's own raised surface and
// overridable from the settings' colour field. Kept beside the custom palette rather
// than in it: it is a preference of its own that survives a theme change, which is
// why the theme watcher must not take it off the document (it is not in CUSTOM_VARS).
export const HOVER_KEY = 'hover-bg';
export const HOVER_VAR = '--hover-bg';

export function readHover(): string {
    try {
        return window.localStorage.getItem(HOVER_KEY) || '';
    } catch {
        return '';
    }
}

// A computed colour (rgb()/rgba(), or hex — both forms the stylesheets use) as
// #rrggbb, so the colour field opens on something that matches the window it sits in.
export function computedHex(varName: string, fallback: string): string {
    const v = getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
    const m = /^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/.exec(v) || /^#([0-9a-f]{6})$/i.exec(v);
    if (!m) return fallback;
    if (m.length === 2) return `#${m[1].toLowerCase()}`;
    const h = (n: string) => Math.round(Number(n)).toString(16).padStart(2, '0');
    return `#${h(m[1])}${h(m[2])}${h(m[3])}`;
}

export function wearHover(hex: string): void {
    try {
        if (hex) window.localStorage.setItem(HOVER_KEY, hex);
        else window.localStorage.removeItem(HOVER_KEY);
    } catch { /* private mode keeps its silence: this lasts for the run */ }
    if (hex) document.documentElement.style.setProperty(HOVER_VAR, hex);
    else document.documentElement.style.removeProperty(HOVER_VAR);
}

export const THEME_REMAP: Record<number, number> = {
    9: 8, 10: 6, 11: 9, 12: 9, 13: 9,
    14: 10, 15: 11, 16: 12, 17: 13, 18: 14,
    19: 15, 20: 16, 21: 17,
};
