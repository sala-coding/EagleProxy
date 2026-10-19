// The words the status bar shows for a moment. They live in public/fa/tips, one
// file per language, instead of in the components that happen to fire them: the
// whole vocabulary in one place, editable without touching code, and a new
// language is a new file rather than a new branch in here.
//
// The files are fetched rather than imported because that is what the public
// folder is for, and because a word that has not arrived yet must not keep the
// window from painting. tip() therefore falls back to Chinese and then to the
// key itself, which is loud enough to name the missing word without asserting
// anything false about what happened.

import {getLang, type Lang} from './i18n'

// Relative, because the built index.html sits at the root of the bundle and the
// app is a single page — there is no route to be one level deeper than.
const FILE = 'fa/tips/tips.';

const tables: Partial<Record<Lang, Record<string, string>>> = {};
const inflight: Partial<Record<Lang, Promise<unknown>>> = {};
// Chinese doubles as the fallback for every other language, the same way i18n
// does it, so a half-translated file shows one untranslated word rather than a
// screen of keys.
let fallback: Record<string, string> = {};

// loadTips fetches one language's table, at most once per language. A file that
// is missing or malformed leaves an empty table instead of throwing: the
// fallback chain is the error handling.
export function loadTips(lang: Lang): Promise<unknown> {
    if (tables[lang]) return Promise.resolve();
    const pending = inflight[lang];
    if (pending) return pending;
    const p = fetch(FILE + lang + '.json')
        .then(r => (r.ok ? r.json() : Promise.reject(new Error('HTTP ' + r.status))))
        .then((json: unknown) => {
            const table: Record<string, string> = {};
            // Copied key by key rather than trusted whole: a stray value would
            // otherwise reach the screen as the word "undefined".
            for (const [k, v] of Object.entries(json as Record<string, unknown>)) {
                if (typeof v === 'string') table[k] = v;
            }
            tables[lang] = table;
            if (lang === 'zh') fallback = table;
        })
        .catch(() => {
            // Not cached as "loaded": a failed fetch (a file the bundle has not
            // shipped yet, a bad json) must be retryable by the next loadTips —
            // `tables[lang] = {}` here would marry the broken empty table for the
            // whole session, with every tip() falling through to the key.
            delete inflight[lang];
        })
        .then(() => {
            delete inflight[lang];
        });
    inflight[lang] = p;
    return p;
}

// tip resolves one word in the active language, then in Chinese, then as the
// key, and fills {0}, {1}… with args in order.
export function tip(key: string, ...args: (string | number)[]): string {
    const s = tables[getLang()]?.[key] ?? fallback[key] ?? key;
    if (!args.length) return s;
    return s.replace(/\{(\d+)\}/g, (m, i) => {
        const v = args[Number(i)];
        return v === undefined ? m : String(v);
    });
}