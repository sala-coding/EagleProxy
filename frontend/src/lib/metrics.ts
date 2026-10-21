import {t} from "../i18n";
import {fmtBytes} from "./format";

// The last two are translation keys rather than names — every other protocol's
// name is the same in every language, and these two are words. Callers pass the
// label through t(), which answers a key that names nothing with itself, so the
// literal names above are unaffected.
export const PROTOCOLS = [
    {id: 'socks', label: 'SOCKS'},
    {id: 'http', label: 'HTTP(S)'},
    {id: 'shadowsocks', label: 'Shadowsocks'},
    {id: 'vmess', label: 'VMess'},
    {id: 'trojan', label: 'Trojan'},
    {id: 'vless', label: 'VLESS'},
    {id: 'hysteria2', label: 'Hysteria2'},
    {id: 'anytls', label: 'AnyTLS'},
    {id: 'tuic', label: 'TUIC'},
    {id: 'naive', label: 'NaïveProxy'},
    {id: 'custom', label: 'proto.custom'},
    {id: 'chain', label: 'proto.chain'},
];

export type Dict = Record<string, unknown>;

export function latencyText(v: number): string {
    if (v > 0) return `${v} ms`;
    if (v < 0) return t('metrics.unavailable');
    return '-';
}

// latencyClass buckets a measured latency into a colour grade. The cut-offs
// follow how proxy nodes actually behave: sub-50ms is a local/nearby server,
// anything past ~500ms is effectively unusable for interactive traffic.
// Untested (0) and failed (<0) return '' so the cell keeps the dim default.
export function latencyClass(v: number): string {
    if (v <= 0) return '';
    if (v < 50) return 'lat-1';
    if (v < 120) return 'lat-2';
    if (v < 250) return 'lat-3';
    if (v < 500) return 'lat-4';
    return 'lat-5';
}

// A node counts as low-latency below 300ms — the first three grades of the
// colour scale. The earlier 120ms cut was so tight that a whole airport's worth
// of nodes (127ms and up) never qualified, so the marker never showed at all.
export function isLowLatency(v: number): boolean {
    return v > 0 && v < 300;
}

// speedText renders a download-speed result: MB/s, or KB/s below 1 MB/s.
export function speedText(v: number): string {
    if (v < 1) return `${Math.round(v * 1024)} KB/s`;
    return `${v.toFixed(2)} MB/s`;
}

// speedCell renders the speed column: the rate, the "unavailable" word for a
// failed measurement, or "-" when the profile was never speed tested.
export function speedCell(v: number): string {
    if (v > 0) return speedText(v);
    if (v < 0) return t('metrics.unavailable');
    return '-';
}

// metricGroup buckets a latency/speed value: real measurements (0) always sort
// ahead of untested (1) and failed (2), regardless of sort direction, so the
// useful nodes never sink below placeholder rows.
export const metricGroup = (v: number) => (v > 0 ? 0 : v === 0 ? 1 : 2);

// trafficCell renders the per-node counters of both directions.
export function trafficCell(up: number, down: number): string {
    if (!up && !down) return '-';
    return `↑ ${fmtBytes(up)} / ↓ ${fmtBytes(down)}`;
}

// Which column each test mode produces (mirrors runSpeedTest in app.go):
// 0 延迟测试 / 1 TCP Ping -> latency, 2 完整测试 -> both, 3 速度测试 -> speed.
export const MODE_HAS_LATENCY = (m: number) => m !== 3;
export const MODE_HAS_SPEED = (m: number) => m === 2 || m === 3;

// How the node table shares a row out, given what its columns measured for themselves.
//
// The measurement is each column's natural width — the widest thing it has to show — and
// the rule is: every column is drawn at exactly that width, and the rest of the row goes
// to the columns that are text. Nothing here is a number per column, and that is the
// point: a hand-written list cannot know what a column holds. The security column was
// capped at 5% by hand once, on the assumption that it held "tls" the way the transport
// holds "tcp" — it holds a description ("TLS · insecure"), and the cap cut it to an
// ellipsis until the number was raised again.
//
// The exception is the columns that are given slack. Three of them measure live numbers:
// a rate or a counter changes while the window sits still, and one frozen at what it
// happened to show when the list was measured would ellipsise the moment the figure grew
// a digit. The fourth is the type column, and it takes the same half again for a
// different reason: it holds one unbreakable word — "AnyTLS" is the longest of them — and
// is measured to exactly its own width, so a hair of shortfall (a scrollbar arriving, the
// share written back rounded down) cut it to "AnyT…" while every text column beside it
// had room to spare. Everything else gets exactly its own, and a value longer than its
// own column still ellipsises — the full text is in the cell's tooltip (see .tls and
// td.name).
export const LIVE_SLACK = 1.5;
// Column indices, with the protocol back in front of the name: type, name, address,
// port, transport, TLS, latency, speed, traffic, group.
export const LIVE_COLS = new Set([0, 6, 7, 8]);
export const TEXT_COLS = new Set([1, 2, 9]);
// How long the window has to hold still before the things measured off it are measured
// again, in milliseconds. A maximise is a few hundred milliseconds of resize events
// stacked on each other, and a measurement taken in the middle of them buys a re-render
// (or a forced layout) for a number the next frame will replace. The column widths, the
// settings sheet's seat and the DNS list's page size all wait behind this.
export const RESIZE_SETTLE_MS = 140;

// The name of a country, in the reader's own language. Intl.DisplayNames carries the
// whole ISO table inside the engine, so a flag's tooltip needs no data of its own — and
// it comes out translated, which a table shipped with the app would not: fa/flag carries
// the flags themselves and no words.
export const regionNames = (() => {
    try {
        return new Intl.DisplayNames(undefined, {type: 'region'});
    } catch {
        return null;
    }
})();

export const countryName = (cc: string): string => {
    const code = (cc || '').toUpperCase();
    if (code.length !== 2) return '';
    try {
        return regionNames?.of(code) ?? code;
    } catch {
        return code;
    }
};

// The name of the rule-set fold: the countries its rules bite in, and the fold itself.
// Two ways a rule set says where it applies: a reader who imported it said so when they
// did (country, kept beside the file — see App.ImportRuleSets), and a tag the catalogue
// names carries it in its own tail, geoip-cn and geosite-cn for China. A country typed as
// a bare code is read the same way the suffix is, so the two agree on the line; anything
// else a reader typed stands as they wrote it. geosite-apple and geosite-category-ads-all
// say nothing about a country either way, and ride along in the fold rather than getting
// one of their own — the fold is about where rules bite, and a category that bites
// everywhere has no place of its own there. With no country named at all, the line is
// just the fold's own name.
// The category a rule set belongs in — what the card's bars are cut by, and the country
// a file imported under a bar is filed with. The reader's own note comes first (the
// country in countries.json), a two-letter note read as the country it names and
// anything else taken as written; without a note it is read off the tag's tail
// (geoip-cn → CN). Neither — geosite-apple, which bites in no one country — has no
// category at all: a rule in force everywhere belongs to no country, and the empty key
// is that non-country's own.
export const rsCategoryOf = (rs: {tag: string; country?: string}): {key: string; label: string; code: string} => {
    const c = (rs.country || '').trim();
    if (c) {
        const code = /^[a-z]{2}$/i.test(c) ? c.toUpperCase() : '';
        return {
            key: 'c:' + c.toLowerCase(),
            label: code ? `${countryName(code)} (${code})` : c,
            code,
        };
    }
    const m = /-([a-z]{2})$/.exec(rs.tag);
    if (!m) return {key: '', label: '', code: ''};
    const code = m[1].toUpperCase();
    return {key: 'c:' + code.toLowerCase(), label: `${countryName(code)} (${code})`, code};
};

// The words the purity score is read by, and the key its label is translated with. The
// bands are the site's own idea: a low score is a clean address, and past sixty it is
// the kind of address platforms distrust.
export const purityLevel = (v: string): string => {
    const n = Number(v);
    if (!Number.isFinite(n)) return 'mid';
    if (n <= 10) return 'vclean';
    if (n <= 30) return 'clean';
    if (n <= 60) return 'mid';
    return 'risky';
};

// SPEED_MODE_KEYS maps a test mode to its i18n label.
export const SPEED_MODE_KEYS = ['speed.latency', 'speed.tcp', 'speed.full', 'speed.download'];

// The same four modes said as something in progress rather than as a menu item. The
// status bar names the run that is going: a latency sweep and a download sweep are
// different waits — one is over in seconds, the other is minutes of bandwidth — and
// calling both of them "testing" told the reader nothing about which one they were in.
export const SPEED_RUNNING_KEYS = ['status.test.latency', 'status.test.tcp', 'status.test.full', 'status.test.speed'];

// Hue assigned to a subscription tab. Stepping by the golden angle (~137.5°)
// maximises the colour distance between consecutive ids, so tabs sitting next to
// each other rarely get similar colours. Seeding from the id keeps a tab's colour
// stable across restarts instead of shifting when the list is reordered.
export const hueFor = (id: number): number => Math.round((id * 137.508) % 360);
