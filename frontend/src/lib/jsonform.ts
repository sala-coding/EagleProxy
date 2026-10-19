// Pure helpers shared by JsonForm.vue and its row/line-list/hotkey sub-components.
// Moved verbatim out of JsonForm.tsx during the React -> Vue migration; the
// behaviour is unchanged.
import {t} from "../i18n";

export type Json = unknown;

// Field names are the raw JSON keys — they are the contract with the config
// file and with the docs. Where a translation exists it is shown as the label
// and the key moves to the tooltip, so nothing is hidden and the mapping stays
// verifiable. An unknown key (a profile field, a protocol-specific option)
// simply keeps its own name.
export function fieldLabel(key: string): string {
    const hit = t(`cfg.${key}`);
    return hit.startsWith('cfg.') ? key : hit;
}

export const LONG_KEYS = new Set([
    'cs', 'cs_suffix', 'c_cfg', 'c_out', 'custom', 'dns_object',
    'custom_inbound', 'custom_route', 'vpn_bypass_process', 'vpn_bypass_cidr',
    'core_map', 'direct_ip', 'direct_domain', 'proxy_ip', 'proxy_domain',
    'block_ip', 'block_domain',
]);

// The fields whose value is a list of lines rather than one block of text: the
// subscription fetcher's client identifiers, one per line, in the order they are tried.
// Edited as a table — one editable row per identifier, a rule between rows, and a button
// to add or drop one. As a textarea it was a wall of lines whose only boundary was a
// newline, and there was no way to take one out without editing the text around it.
export const LINE_LIST_KEYS = new Set(['user_agent2']);

// The line under a field, for a key whose value needs a rule stated rather than a
// name given. Same lookup shape as fieldLabel: a key with no translation gets no
// hint, so this costs nothing on the fields that have nothing to explain.
export function fieldHint(key: string): string | undefined {
    const hit = t(`hint.${key}`);
    return hit.startsWith('hint.') ? undefined : hit;
}

// --- hotkeys ---

// Every hotkey field, by the prefix the backend's settings use. A whole class rather
// than a list of names: a combination is captured, not typed (see HotkeyInput), and
// that has to hold for a key added later as much as for the twelve there are.
export const HOTKEY_KEY = /^hk_/;

// A press as the name internal/hotkey.Parse knows it. Anything else is a key the backend
// has no name for, and the press is passed over rather than recorded as something that
// would never bind.
export function captureName(key: string): string | undefined {
    if (key === ' ') return 'Space';
    if (/^[a-z0-9]$/i.test(key)) return key.toUpperCase();
    if (/^F([1-9]|1[0-9]|2[0-4])$/.test(key)) return key;
    switch (key) {
        case 'Tab': return 'Tab';
        case 'Enter': return 'Enter';
        case 'Escape': return 'Esc';
        case 'Home': return 'Home';
        case 'End': return 'End';
        case 'ArrowLeft': return 'Left';
        case 'ArrowRight': return 'Right';
        case 'ArrowUp': return 'Up';
        case 'ArrowDown': return 'Down';
    }
    return undefined;
}

// The modifiers as the backend spells them, in the order it reads them back.
const MODIFIERS: ['ctrlKey' | 'shiftKey' | 'altKey' | 'metaKey', string][] = [
    ['ctrlKey', 'Ctrl'],
    ['shiftKey', 'Shift'],
    ['altKey', 'Alt'],
    ['metaKey', 'Win'],
];

export function heldModifiers(e: KeyboardEvent): string {
    return MODIFIERS.filter(([flag]) => e[flag]).map(([, name]) => name).join('+');
}

export function isPlainObject(v: Json): v is Record<string, Json> {
    return v !== null && typeof v === 'object' && !Array.isArray(v);
}

export function arrayToText(arr: Json[]): string {
    return arr.map(x => (typeof x === 'object' ? JSON.stringify(x) : String(x))).join(', ');
}

export function textToArray(text: string, sample: Json[] | undefined): Json[] {
    const parts = text.split(',').map(s => s.trim()).filter(s => s.length > 0);
    const numeric = sample && sample.length > 0 && sample.every(x => typeof x === 'number');
    if (numeric) {
        return parts.map(p => Number(p)).filter(n => !Number.isNaN(n));
    }
    return parts;
}
