// The interface sizes the settings offer, as a multiplier on the root font size — the one
// number every dimension of type in the app is measured against (see style.css). It is an
// offset on the window's own curve rather than a replacement for it, which is what lets
// "the text follows the window" and "the text suits my eyes" both be true at once.
// The range the type scale is dragged over, and the step. The ends are the same ones a
// stored value is clamped to when it is read back (see uiScale in App.tsx), so the
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

// The hues the custom-palette popover offers, one disc per stop of the wheel:
// twelve is enough to aim with, and the aim is at a colour family, not at a
// colour — the derivation turns the aim into a whole palette.
export const TONE_HUES = [0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330];

// The two grounds a custom palette can stand on, as the panel counts them (see
// readCustomPalette, which is what a saved one is read back through). They are drawn as
// the named answers they are — a capsule with the word in it, the chosen one filled —
// rather than as two pictures of a ground: the panel already shows twelve colours of the
// hue, and two more swatches beside them said nothing the words do not.
export const TONE_GROUNDS = [
    {value: 'deep', label: 'settings.customGround.deep'},
    {value: 'pale', label: 'settings.customGround.pale'},
];

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

// h is 0–359; ground is a deep report or a pale one. The hue alone drives the
// derivation: surfaces are the hue's own ink at a ladder of lightness, the
// accent is the hue at its clearest, and the signal colours and the latency
// scale keep the neutral legs the built-in palettes use — a clock red has to
// stay believable in every palette, so that is not the place for cleverness.
export function buildCustomPalette(h: number, ground: 'deep' | 'pale'): CustomPalette {
    const core: CustomPalette = ground === 'pale' ? {
        '--bg': `hsl(${h}, 45%, 96%)`,
        '--bg-2': `hsl(${h}, 42%, 92%)`,
        '--bg-3': `hsl(${h}, 40%, 88%)`,
        '--line': `hsl(${h}, 30%, 80%)`,
        '--line-strong': `hsl(${h}, 28%, 68%)`,
        '--text': `hsl(${h}, 25%, 25%)`,
        '--text-dim': `hsl(${h}, 15%, 45%)`,
        '--accent': `hsl(${h}, 55%, 38%)`,
        '--accent-soft': `hsla(${h}, 55%, 38%, 0.14)`,
        '--accent-edge': `hsla(${h}, 55%, 38%, 0.45)`,
        '--on-fill': '#ffffff',
        '--green': '#279c56',
        '--red': '#d64545',
        '--amber': '#c98a00',
        '--green-bg': 'rgba(39, 156, 86, 0.14)',
        '--green-bg-hover': 'rgba(39, 156, 86, 0.20)',
        '--lat-1': '#279c56',
        '--lat-2': '#4aa96a',
        '--lat-3': '#c98a00',
        '--lat-4': '#cf6a1e',
        '--lat-5': '#d64545',
    } : {
        '--bg': `hsl(${h}, 20%, 7%)`,
        '--bg-2': `hsl(${h}, 20%, 11%)`,
        '--bg-3': `hsl(${h}, 20%, 15%)`,
        '--line': `hsl(${h}, 18%, 22%)`,
        '--line-strong': `hsl(${h}, 20%, 30%)`,
        '--text': `hsl(${h}, 30%, 88%)`,
        '--text-dim': `hsl(${h}, 15%, 63%)`,
        '--accent': `hsl(${h}, 62%, 62%)`,
        '--accent-soft': `hsla(${h}, 62%, 62%, 0.18)`,
        '--accent-edge': `hsla(${h}, 62%, 62%, 0.50)`,
        '--on-fill': 'var(--bg)',
        '--green': '#4fd489',
        '--red': '#ff6b6b',
        '--amber': '#ffc266',
        '--green-bg': 'rgba(79, 212, 137, 0.16)',
        '--green-bg-hover': 'rgba(79, 212, 137, 0.24)',
        '--lat-1': '#4fd489',
        '--lat-2': '#6fe0a5',
        '--lat-3': '#ffc266',
        '--lat-4': '#ff8a5c',
        '--lat-5': '#ff6b6b',
    };
    core[CUSTOM_GROUND] = ground;
    return core;
}

// The stored answer, or null where nothing was saved yet: the swatch wears its
// empty uniform until the reader has made one.
export function readCustomPalette(): CustomPalette | null {
    try {
        const raw = window.localStorage.getItem('custom-palette');
        const p = raw ? JSON.parse(raw) as CustomPalette : null;
        return p && typeof p['--bg'] === 'string' ? p : null;
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
export const THEME_REMAP: Record<number, number> = {
    9: 8, 10: 6, 11: 9, 12: 9, 13: 9,
    14: 10, 15: 11, 16: 12, 17: 13, 18: 14,
    19: 15, 20: 16, 21: 17,
};
