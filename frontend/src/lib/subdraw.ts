// fmtRate renders a bytes-per-second value, which is what the status bar shows:
// a lifetime total like "2.1 MB" tells you nothing about what the node is doing
// right now, while "2.1 MB/s" does.
export function fmtRate(bytesPerSec: number): string {
    if (!bytesPerSec || bytesPerSec <= 0) return '0 B/s';
    const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
    let i = 0;
    let v = bytesPerSec;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function fmtTime(ts: number): string {
    if (!ts || ts <= 0) return '—';
    const d = new Date(ts > 1e11 ? ts : ts * 1000);
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// Quota is not a speed and not a running total: it is a number the reader is
// going to compare against a limit, so it gets binary units and two decimals.
// fmtBytes' single decimal would leave "50.3 GiB of 100 GiB" looking like a
// different figure from the one in the panel's own account page.
export function fmtQuota(n: number): string {
    if (!n || n <= 0) return '0 B';
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(i === 0 ? 0 : 2)} ${units[i]}`;
}

// How wide a subscription's name is drawn, in display columns. The standard is the
// sample [性价比机场]: five full-width characters, ten columns, brackets aside — they
// are one column each and every row has them.
//
// It is the standard rather than the width, though: the drawer is not a fixed width, and
// a column of cards does not want to be one either. The reader can drag the drawer, and a
// window narrow enough for the stylesheet's clamp makes it narrower still, and a column
// that kept its ten through both would spend a narrow drawer on filler while showing less
// of a name in a wide one than there was room for. So the count is measured from the row
// it is drawn in — see nameCols in App.tsx — and this is what it falls back to until
// there is a row to measure.
//
// The unit is columns and not characters, and that is the whole point of the
// exercise. A panel's name is whatever it is, and the thing after it on the row —
// the allowance bar — should start at the same place on every card so the column can
// be read at a glance. Padding to ten *characters* would leave "rouaa0" half the
// width of a Chinese name and the column ragged, which is what the padding exists to
// prevent.
export const SUB_NAME_COLS = 10;

// What that measurement is held between. The floor is where a card stops being readable —
// a bracket, a character or two, and the bar. The ceiling is a guard and not a design
// limit: it is set far above any drawer the reader can drag to, so that the count is the
// row's width, and only a measurement gone wrong can reach it.
export const SUB_NAME_COLS_MIN = 4;
export const SUB_NAME_COLS_MAX = 80;

// How a panel in the left seat is leaving: 'flip' turns it back out of the hinge, which
// is the only way it leaves. Being displaced by the panel that shares its seat used to
// be a second kind — a drop out of the window — and is not any more: a displacement is
// not a dismissal, so the displaced panel leaves in the frame its replacement arrives
// (see openSubs) rather than performing an exit nobody asked for.
export type PanelExit = 'flip';

// The drawer's class for a given exit, and the class is what tells the stylesheet which
// exit this is. Written out rather than as a ternary inline so the name lives once.
export function SUBS_PANEL_CLASS(exiting: PanelExit | null): string {
    return `subdrop${exiting === 'flip' ? ' flip-out' : ''}`;
}

// How long before a subscription's expiry the card starts saying so, in days. A week
// is what most panels' own renewal reminders work on, and it is long enough to do
// something about — renew, or move to another subscription — rather than merely long
// enough to notice.
export const EXPIRY_WARN_DAYS = 7;
