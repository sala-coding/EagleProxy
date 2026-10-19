export function fmtBytes(n: number): string {
    if (!n || n <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// fmtRate renders a live rate the same language fmtBytes speaks, plus the per-second
// suffix the dock's rate slot reads as "moving right now".
export function fmtRate(n: number): string {
    return `${fmtBytes(n)}/s`;
}

// Width of one character in display columns: the CJK and full-width ranges take two,
// everything else one. This is the same approximation a browser makes when it lays
// the text out, and it is close enough to decide where the next thing starts.
export function charCols(ch: string): number {
    const cp = ch.codePointAt(0) ?? 0;
    return (
        (cp >= 0x1100 && cp <= 0x115f) ||   // Hangul Jamo
        (cp >= 0x2e80 && cp <= 0x303e) ||   // CJK radicals and punctuation
        (cp >= 0x3041 && cp <= 0x33ff) ||   // kana, CJK compatibility
        (cp >= 0x3400 && cp <= 0x4dbf) ||   // CJK extension A
        (cp >= 0x4e00 && cp <= 0x9fff) ||   // CJK unified ideographs
        (cp >= 0xa000 && cp <= 0xa4cf) ||   // Yi
        (cp >= 0xac00 && cp <= 0xd7a3) ||   // Hangul syllables
        (cp >= 0xf900 && cp <= 0xfaff) ||   // CJK compatibility ideographs
        (cp >= 0xfe30 && cp <= 0xfe6f) ||   // CJK compatibility forms
        (cp >= 0xff00 && cp <= 0xff60) ||   // full-width forms
        (cp >= 0xffe0 && cp <= 0xffe6) ||   // full-width signs
        (cp >= 0x20000 && cp <= 0x3fffd)    // CJK extension B and beyond
    ) ? 2 : 1;
}

// fitName cuts or pads a name to the column the list reserves for it. The column is a
// count of display columns rather than a width in pixels, and the count comes from the
// row itself — see nameCols in App.tsx.
//
// The two halves come back separately so the filler can be marked as decoration: it is
// there to hold the column's width, and a screen reader reading out a row of hyphens
// would be hearing about the layout rather than about the subscription.
//
// A full-width character is never split in half. When one does not fit in what is
// left, the name stops a column short and the filler makes up the difference — a
// half glyph is not a thing.
export function fitName(name: string, cols: number): {text: string; pad: string} {
    let text = '';
    let w = 0;
    for (const ch of name) {
        const cw = charCols(ch);
        if (w + cw > cols) break;
        text += ch;
        w += cw;
    }
    // The filler is blank on purpose. It used to be dashes, which read as part
    // of the name -- "[小鲤鱼--]" -- rather than as the layout they backed.
    // Non-breaking spaces hold exactly the same columns and show nothing.
    return {text, pad: '\u00a0'.repeat(cols - w)};
}

// SubUsage is what a subscription's own Subscription-UserInfo header says about
// the account behind it: how much of the allowance is left, and when the allowance
// ends. Every field is optional — panels send whatever they have, and some send
// none of it.
//
// `used` and `total` are not printed anywhere. They are the inputs the remaining
// figure is worked out from, and keeping them named is clearer than folding the
// subtraction into one field at parse time.
export type SubUsage = {used: number; total: number; left: number; expire: number};

// parseSubUsage reads the header the backend stored verbatim on the group.
//
// The format is a flat `k=v; k=v` list, but the parts are not dependable: the
// order varies between panels, `total` is sometimes absent, `expire` is a unix
// timestamp on some and a date string on others, and an unlimited plan sends
// `total=0`. So each key is looked up by name and each value coerced on its own,
// and anything unrecognised is simply absent rather than wrong.
export function parseSubUsage(info: string | undefined | null): SubUsage | null {
    if (!info) return null;
    const kv: Record<string, string> = {};
    for (const part of info.split(';')) {
        const eq = part.indexOf('=');
        if (eq <= 0) continue;
        kv[part.slice(0, eq).trim().toLowerCase()] = part.slice(eq + 1).trim();
    }
    const num = (k: string): number => {
        const v = Number(kv[k]);
        return Number.isFinite(v) && v > 0 ? v : 0;
    };
    const used = num('upload') + num('download');
    const total = num('total');
    // The header only ever reports the used side, so the remainder is worked out
    // here. An unlimited plan reports total=0, and reading that as "0 bytes left"
    // would be alarming and wrong — left is only claimed when there is a total to
    // measure against.
    const left = total > 0 ? Math.max(0, total - used) : 0;
    let expire = num('expire');
    if (!expire) {
        // A few panels send the expiry as a date rather than a timestamp. It is
        // read only when it parses as one, because a wrong date is worse than no
        // date — and a number is never a date here: "0" is the standard way of
        // saying "never expires", while Date.parse("0") answers 2000-01-01 quite
        // happily, which is how an unlimited subscription used to be drawn as one
        // that had expired at the turn of the century.
        const raw = String(kv.expire ?? '').trim();
        const d = raw && !/^-?\d+(\.\d+)?$/.test(raw) ? Date.parse(raw) : NaN;
        expire = Number.isFinite(d) ? Math.floor(d / 1000) : 0;
    }
    if (!used && !total && !expire) return null;
    return {used, total, left, expire};
}
