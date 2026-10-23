import * as API from "../../wailsjs/go/main/App";
import {t} from "../i18n";
import {errText} from "./shared";

// --- routing ---

// The domain strategies sing-box accepts, as the Go side validates them (see
// singBoxStrategies in setting.go): the empty one is "let the core decide", and the other
// four are its own keywords — written as they are read, because they are values out of the
// config rather than prose about it.
export const STRATEGY_OPTIONS = [
    {value: '', label: 'routing.strategyDefault'},
    {value: 'prefer_ipv4', label: 'prefer_ipv4'},
    {value: 'prefer_ipv6', label: 'prefer_ipv6'},
    {value: 'ipv4_only', label: 'ipv4_only'},
    {value: 'ipv6_only', label: 'ipv6_only'},
];

// A resolver is chosen by name and stored by address, so the two are kept apart: the
// menu shows the first and the field holds the second. The encrypted endpoints could be
// listed as their own addresses and still be picked from; the plain resolvers below
// cannot — a menu of bare addresses is one nobody can choose out of.
export type DnsPreset = {name: string; value: string; group?: string};

// The encrypted endpoints, first in the menu because they are the better answer.
//
// Their names were their addresses — an https:// URL says both what it is and where it
// goes — which left this one group with nothing to choose by but the spelling of an
// address. They are named now, and where a service is here more than once, the host is
// what tells its rows apart: Google and Cloudflare are each reachable by literal address,
// by their own hostname, and over IPv6, and which of those to take is a question about the
// network in the way rather than about the resolver.
export const DOH_NAMES: Record<string, string> = {
    // Google
    'https://8.8.8.8/dns-query': 'Google',
    'https://dns.google/dns-query': 'Google (dns.google)',
    'https://[2001:4860:4860::8888]/dns-query': 'Google (IPv6)',
    // Cloudflare, whose three policies are three different answers to one query: the
    // ordinary resolver, one that blocks malware, and one that blocks malware and adult
    // content. The policy is the thing a reader picks between here, so it is what the
    // parenthesis says — the address tells the two spellings of the same policy apart.
    'https://1.1.1.1/dns-query': 'Cloudflare',
    'https://1.0.0.1/dns-query': 'Cloudflare (1.0.0.1)',
    'https://one.one.one.one/dns-query': 'Cloudflare (one.one.one.one)',
    'https://dns.cloudflare.com/dns-query': 'Cloudflare (dns.cloudflare.com)',
    'https://[2606:4700:4700::1111]/dns-query': 'Cloudflare (IPv6)',
    'https://1.1.1.2/dns-query': 'Cloudflare (Malware, 1.1.1.2)',
    'https://1.1.1.3/dns-query': 'Cloudflare (Family, 1.1.1.3)',
    'https://security.cloudflare-dns.com/dns-query': 'Cloudflare (Malware)',
    'https://family.cloudflare-dns.com/dns-query': 'Cloudflare (Family)',
    // Quad9: the bare name blocks malware, dns10 answers everything, dns11 blocks and
    // also sends the client's subnet along — a difference that shows up as a different
    // answer for the same name depending on where the node is.
    'https://dns.quad9.net/dns-query': 'Quad9',
    'https://dns10.quad9.net/dns-query': 'Quad9 (Unfiltered)',
    'https://dns11.quad9.net/dns-query': 'Quad9 (Secured, ECS)',
    'https://doh.opendns.com/dns-query': 'OpenDNS',
    'https://doh.familyshield.opendns.com/dns-query': 'OpenDNS (FamilyShield)',
    'https://dns.adguard-dns.com/dns-query': 'AdGuard',
    // The independents, each one operator and one policy: a Greek privacy service, a
    // filtering service that publishes both of its filters as separate endpoints, a free
    // tier of Control D, a Japanese carrier's public resolver, and a Swiss association's.
    'https://doh.libredns.gr/dns-query': 'LibreDNS',
    'https://doh.cleanbrowsing.org/doh/security-filter/': 'CleanBrowsing (Security)',
    'https://doh.cleanbrowsing.org/doh/family-filter/': 'CleanBrowsing (Family)',
    'https://freedns.controld.com/p0': 'Control D',
    'https://public.dns.iij.jp/dns-query': 'IIJ (Japan)',
    'https://dns.digitale-gesellschaft.ch/dns-query': 'Digitale Gesellschaft (CH)',
    // And the same three services again over DoT: port 853 rather than 443, a different
    // wire format, the same promise. Worth having both ways — DoH rides on the web's own
    // port and looks like it, which is what gets it through some networks and what gets it
    // blocked on others.
    'tls://dns.google': 'Google (DoT)',
    'tls://1.1.1.1': 'Cloudflare (DoT)',
    'tls://dns.quad9.net': 'Quad9 (DoT)',
    'tls://dns.adguard-dns.com': 'AdGuard (DoT)',
};

export const DOH_PRESETS = [
    // Google
    'https://8.8.8.8/dns-query',
    'https://dns.google/dns-query',
    'https://[2001:4860:4860::8888]/dns-query',
    // Cloudflare
    'https://1.1.1.1/dns-query',
    'https://1.0.0.1/dns-query',
    'https://one.one.one.one/dns-query',
    'https://dns.cloudflare.com/dns-query',
    'https://[2606:4700:4700::1111]/dns-query',
    'https://1.1.1.2/dns-query',
    'https://1.1.1.3/dns-query',
    'https://security.cloudflare-dns.com/dns-query',
    'https://family.cloudflare-dns.com/dns-query',
    // Quad9
    'https://dns.quad9.net/dns-query',
    'https://dns10.quad9.net/dns-query',
    'https://dns11.quad9.net/dns-query',
    // OpenDNS
    'https://doh.opendns.com/dns-query',
    'https://doh.familyshield.opendns.com/dns-query',
    // AdGuard
    'https://dns.adguard-dns.com/dns-query',
    // Independents
    'https://doh.libredns.gr/dns-query',
    'https://doh.cleanbrowsing.org/doh/security-filter/',
    'https://doh.cleanbrowsing.org/doh/family-filter/',
    'https://freedns.controld.com/p0',
    'https://public.dns.iij.jp/dns-query',
    'https://dns.digitale-gesellschaft.ch/dns-query',
    // DoT
    'tls://dns.google',
    'tls://1.1.1.1',
    'tls://dns.quad9.net',
    'tls://dns.adguard-dns.com',
];

// The window a proxied name is resolved through. Encrypted transports only: a query sent to a
// bare address crosses the local network and everything past it in the clear, and this is the
// list whose whole reason for existing is that the reader has decided the way there is not to
// be trusted. The same list is what the smart pick measures through the running node, so an
// entry here is also a decision about what that node is asked to carry.
// What went with them: the plain-address forms of the ordinary, family and secure resolvers.
// The first of those loses nothing that is not still here under another name — dns.google,
// 1.1.1.1 and dns.quad9.net are all in DOH_PRESETS. The other two do lose something: filtering
// is a policy, and there is no filtering DoH endpoint in the list below. If that is wanted back,
// Cloudflare and Quad9 both publish one (security.cloudflare-dns.com, family.cloudflare-dns.com,
// dns.quad9.net already being the filtered one of that pair) — those belong here as addresses
// in DOH_PRESETS, next to the names they filter for.
export const REMOTE_DNS_PRESETS: DnsPreset[] = [
    ...DOH_PRESETS.map(v => ({name: DOH_NAMES[v] ?? v, value: v, group: 'routing.dnsGroup.doh'})),
];

// The same shape for the domestic list. These are known by their numbers — "114",
// "223.5.5.5" — but a name column holding an address is a column with nothing to read: the
// address is in the next column already. Each is named for whoever runs it, and where one
// operator runs several the address column is what tells its rows apart, which is what it
// is for. `local` is the exception: it is sing-box's own keyword for the system's resolver,
// not anybody's service.
//
// The parenthetical is the one thing the operator's name cannot imply — the protocol in
// the encrypted group, the address family, or which of the two filtered answers the row
// holds.
export const DIRECT_DNS_NAMES: Record<string, string> = {
    'https://dns.alidns.com/dns-query': 'AliDNS (DoH)',
    'https://223.5.5.5/dns-query': 'AliDNS (DoH, 223.5.5.5)',
    'tls://dns.alidns.com': 'AliDNS (DoT)',
    'tls://223.5.5.5': 'AliDNS (DoT, 223.5.5.5)',
    'https://doh.pub/dns-query': 'DNSPod (DoH)',
    'https://1.12.12.12/dns-query': 'DNSPod (DoH, 1.12.12.12)',
    'https://120.53.53.53/dns-query': 'DNSPod (DoH, 120.53.53.53)',
    'tls://dot.pub': 'DNSPod (DoT)',
    'tls://119.29.29.29': 'DNSPod (DoT, 119.29.29.29)',
    'tls://120.53.53.53': 'DNSPod (DoT, 120.53.53.53)',
    'https://doh.360.cn/dns-query': '360 (DoH)',
    'local': 'Local',
    '223.5.5.5': 'AliDNS',
    '223.6.6.6': 'AliDNS',
    '119.29.29.29': 'DNSPod',
    '119.28.28.28': 'DNSPod',
    '182.254.116.116': 'Tencent',
    '182.254.118.118': 'Tencent',
    '114.114.114.114': '114DNS',
    '114.114.115.115': '114DNS',
    '180.76.76.76': 'Baidu',
    '1.2.4.8': 'CNNIC',
    '210.2.4.8': 'CNNIC',
    '117.50.11.11': 'OneDNS',
    '117.50.22.22': 'OneDNS',
    '2400:3200::1': 'AliDNS (IPv6)',
    '2400:3200:baba::1': 'AliDNS (IPv6)',
    '2402:4e00::': 'DNSPod (IPv6)',
    '2402:4e00:1::': 'DNSPod (IPv6)',
    '2400:da00::6666': 'Baidu (IPv6)',
    '114.114.114.110': '114DNS (Family)',
    '114.114.115.110': '114DNS (Family)',
    '117.50.10.10': 'OneDNS (Family)',
    '117.50.20.20': 'OneDNS (Family)',
    '114.114.114.119': '114DNS (Clean)',
    '114.114.115.119': '114DNS (Clean)',
    '52.80.66.66': 'OneDNS (Filtered)',
    '101.226.4.6': '360',
    '218.30.118.6': '360',
    '123.125.81.6': '360',
    '140.207.198.6': '360',
};

export function addressPresets(list: string[], group: string): DnsPreset[] {
    return list.map(value => ({name: DIRECT_DNS_NAMES[value] ?? value, value, group}));
}

// 国内 DNS, grouped as the list above is: what is encrypted, what answers as it is, and the
// two kinds that filter. Four services carry most of it — AliDNS (223.5.5.5), DNSPod
// (119.29.29.29), 114DNS and OneDNS — with Baidu, 360, CNNIC and Tencent's older pair
// beside them.

// 加密（DoH / DoT）. Both forms are here: the domestic services offer each of them over the
// same resolver, and which one to use is a question about the network in the way, not about
// the answer.
export const DNS_CN_ENCRYPTED = [
    'https://dns.alidns.com/dns-query',
    'https://223.5.5.5/dns-query',
    'https://doh.pub/dns-query',
    'https://1.12.12.12/dns-query',
    'https://120.53.53.53/dns-query',
    'https://doh.360.cn/dns-query',
    'tls://dns.alidns.com',
    'tls://223.5.5.5',
    'tls://dot.pub',
    'tls://119.29.29.29',
    'tls://120.53.53.53',
];

// 默认（不过滤）. `local` first because it is not a resolver but the system's own.
export const DNS_CN_DEFAULT = [
    'local',
    '223.5.5.5', '223.6.6.6',
    '119.29.29.29', '119.28.28.28',
    '182.254.116.116', '182.254.118.118',
    '114.114.114.114', '114.114.115.115',
    '180.76.76.76',
    '1.2.4.8', '210.2.4.8',
    '117.50.11.11', '117.50.22.22',
    '2400:3200::1', '2400:3200:baba::1',
    '2402:4e00::', '2402:4e00:1::',
    '2400:da00::6666',
];

// 家庭（拦截恶意与成人内容）
export const DNS_CN_FAMILY = [
    '114.114.114.110', '114.114.115.110',
    '117.50.10.10', '117.50.20.20',
];

// 安全（拦截恶意内容）
export const DNS_CN_SECURE = [
    '114.114.114.119', '114.114.115.119',
    '52.80.66.66',
    '101.226.4.6', '218.30.118.6',
    '123.125.81.6', '140.207.198.6',
];

export const DIRECT_DNS_PRESETS: DnsPreset[] = [
    ...addressPresets(DNS_CN_ENCRYPTED, 'routing.dnsGroup.doh'),
    ...addressPresets(DNS_CN_DEFAULT, 'routing.dnsGroup.default'),
    ...addressPresets(DNS_CN_FAMILY, 'routing.dnsGroup.family'),
    ...addressPresets(DNS_CN_SECURE, 'routing.dnsGroup.secure'),
];

// What the 自定义 DNS 配置 box is filled with when the reader asks for a starting point.
//
// Not an illustration but the shape the builder itself produces for the three fields above it
// (see buildDNS in the Go side), with everything that cannot be left out kept in. `dns-direct`
// is the tag the route's own fallback resolver points at, so a hand-written section without
// it is a config the core will not start on. `dns-local` is the one the other two resolve
// their own hostnames through. There is no `final` key: this core reads the *order* of
// `servers`, the first being the default for names no rule matched, which is how 兜底解析 is
// expressed — and an unknown key here is a startup failure rather than a warning.
export const DNS_OBJECT_TEMPLATE = `{
  "servers": [
    {
      "tag": "dns-remote",
      "type": "https",
      "server": "dns.google",
      "path": "/dns-query",
      "detour": "proxy",
      "domain_resolver": "dns-local"
    },
    {
      "tag": "dns-direct",
      "type": "https",
      "server": "doh.pub",
      "path": "/dns-query",
      "domain_resolver": "dns-local"
    },
    {
      "tag": "dns-local",
      "type": "local"
    }
  ],
  "rules": [
    {
      "domain_suffix": ["cn"],
      "server": "dns-direct"
    }
  ]
}`;

// The visual half of that box: its `servers`, as rows that can be added, changed and removed
// without knowing what the keys are called. The rules are deliberately not here — a rule's
// matchers are an open set (domain, domain_suffix, rule_set, ip_cidr, port, query_type,
// action …) and a form over them would drop whatever it did not know.
//
// Nothing about the box is authoritative but the text in it. A row carries the object it came
// from, and only the keys the row shows are rewritten, so anything else — an option this side
// has never heard of, or a field the row does not offer — survives being edited. An emptied
// field removes its key rather than writing an empty string, because the core reads these
// strictly: a field that does not belong to the server's type is a config that will not start.
export type DnsServerRow = {
    raw: Record<string, unknown>;
    tag: string;
    type: string;
    addr: string;
    path: string;
    detour: string;
    resolver: string;
};

// The types a dns server can be, in the vocabulary the config builder reads addresses in (see
// splitDNSAddress). A type chosen from here is one the core accepts — which `h2` is not: it is
// xray's spelling, sing-box has only `https` (with HTTP/2 negotiated) and `h3`. Choosing it
// used to be a config the core refuses to start on, because the forwarder rejected the
// address, the builder fell back to writing the reader's own, and `{"type":"h2"}` took the
// whole profile down with it.
export const DNS_SERVER_TYPES = ['https', 'h3', 'tls', 'quic', 'udp', 'tcp', 'local', 'fakeip'];

// The two that answer by themselves: they take no address, no path, no detour and nothing to
// resolve their own name with.
export const dnsTypeResolvesItself = (type: string) => type === 'local' || type === 'fakeip';
// Only the HTTP-shaped ones have a path.
export const dnsTypeUsesPath = (type: string) => type === 'https' || type === 'h3';

// A server object as a row. The port rides inside the address, which is how the rest of this
// card writes one, and an IPv6 host is bracketed on the way in: bracketed is the only way to
// tell a port from the address's own last group.
export function dnsServerToRow(server: unknown): DnsServerRow {
    const raw = (server && typeof server === 'object' && !Array.isArray(server))
        ? {...server as Record<string, unknown>} : {};
    const host = typeof raw.server === 'string' ? raw.server : '';
    const port = Number(raw.server_port) || 0;
    const addr = port > 0 ? (host.includes(':') ? `[${host}]:${port}` : `${host}:${port}`) : host;
    return {
        raw,
        tag: String(raw.tag ?? ''),
        type: String(raw.type ?? 'udp'),
        addr,
        path: String(raw.path ?? ''),
        detour: String(raw.detour ?? ''),
        resolver: String(raw.domain_resolver ?? ''),
    };
}

// …and back. Only the keys the row owns are touched, and each is dropped when its type has no
// use for it or its field is empty — an empty string and a zero are values the core would try
// to use, and a field left over from another type is one it would refuse.
export function dnsRowToServer(row: DnsServerRow): Record<string, unknown> {
    const out: Record<string, unknown> = {...row.raw, type: row.type};
    const drop = (...keys: string[]) => keys.forEach(k => delete out[k]);
    // An empty tag is dropped for the same reason as an empty address: a tag is a name, and a
    // name of nothing is not one. A server nothing refers to may have none at all.
    if (row.tag.trim() === '') drop('tag'); else out.tag = row.tag.trim();
    if (dnsTypeResolvesItself(row.type)) {
        drop('server', 'server_port', 'path', 'detour', 'domain_resolver');
        return out;
    }
    let host = row.addr.trim();
    let port = 0;
    if (host.startsWith('[')) {
        const end = host.indexOf(']');
        if (end > 0) {
            const rest = host.slice(end + 1);
            host = host.slice(1, end);
            if (rest.startsWith(':')) port = Number(rest.slice(1)) || 0;
        }
    } else if ((host.match(/:/g) ?? []).length === 1) {
        const at = host.indexOf(':');
        const p = Number(host.slice(at + 1));
        if (host.slice(at + 1).trim() !== '' && Number.isFinite(p) && p > 0) {
            port = p;
            host = host.slice(0, at);
        }
    }
    if (host === '') drop('server'); else out.server = host;
    if (port > 0) out.server_port = port; else drop('server_port');
    if (dnsTypeUsesPath(row.type) && row.path.trim() !== '') out.path = row.path.trim();
    else drop('path');
    if (row.detour.trim() !== '') out.detour = row.detour.trim(); else drop('detour');
    if (row.resolver.trim() !== '') out.domain_resolver = row.resolver.trim();
    else drop('domain_resolver');
    return out;
}

// What gets measured: the entries of a list that can be asked over the path the core would
// use. `local` never — it is a keyword for whatever the system's resolver happens to be, and
// has no latency of its own to compare. The filtering groups do: they are ranked like
// anything else, they are only kept out of the *pick* (see pickable).
//
// The domestic list is measured whole, directly. The foreign one is measured through the
// running node, whose proxy carries a stream and no datagrams, so a plain resolver there
// would have to be asked over TCP — and that is not the question. Measured against the
// domestic resolvers, six of thirteen do not answer DNS over TCP at all (119.29.29.29
// closes the connection; 117.50.11.11, 180.76.76.76 and others never reply), so a foreign
// plain resolver ranked by TCP would carry a verdict it never gave. Those rows stay in the
// catalogue, unmeasured, with a dash where the number goes — which is the truth about them.
export function dnsCandidates(presets: DnsPreset[], encryptedOnly: boolean): string[] {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const p of presets) {
        if (encryptedOnly && p.group !== 'routing.dnsGroup.doh') continue;
        if (p.value === 'local' || seen.has(p.value)) continue;
        seen.add(p.value);
        out.push(p.value);
    }
    return out;
}

// Whether a measured entry may be *chosen* by a pick, as opposed to merely ranked by its
// number: everything but the two filtering groups. A resolver that edits its answers is a
// decision and not a speed, and being handed one silently, as simply the fastest, would
// change what the reader's DNS does to those answers.
export function pickable(preset: DnsPreset | undefined): boolean {
    return preset?.group !== 'routing.dnsGroup.family' && preset?.group !== 'routing.dnsGroup.secure';
}

// The name a picked address is filed under, for saying which one won.
export function presetName(presets: DnsPreset[], value: string): string {
    return presets.find(p => p.value === value)?.name ?? value;
}

// How often the smart pick is made again when the profile does not say: half an hour. What
// is being measured is the path to a resolver, which does not change by the minute, and one
// round asks fifty-odd resolvers three questions each. The card has a box for it; this is
// what an untouched profile — or one written before the box existed — means.
export const SMART_PICK_MINUTES = 30;

// How often the schedule is looked at, as opposed to how often it fires. The interval is the
// profile's own setting and can be changed while a long one is pending, so the timer cannot
// be a fixed interval: it wakes up, reads what the profile says, and decides. A minute of
// checking costs one file read and never misses a round by more than that.
export const SMART_TICK_MS = 60 * 1000;

// A winner, and what it was won with.
export type PickWin = {value: string; name: string; ms: number};

// Which of the card's two lists a thing is about. They are measured over different paths —
// the foreign one through the running node, the domestic one straight out — so the two are
// named apart wherever a round is asked for.
export type Side = 'remote' | 'direct';

// One round's results.
export type Round = {
    remote: PickWin | null;
    direct: PickWin | null;
    latency: Record<string, number>;
    note: string;
};

// How high a latency has to be before the resolver in the field counts as too slow to keep,
// and how much quicker an alternative has to be before it is worth moving to. Two numbers
// rather than one so that a round cannot move the resolver for a difference nobody could
// feel: 智能切换 is meant to notice a resolver going bad, not to chase a millisecond.
export const SMART_SWITCH_AT_MS = 200;
export const SMART_SWITCH_BY_MS = 30;

// What a round should write for one side, given what it measured and what the profile is set
// to. Three answers, and they are the whole of what the two switches mean:
//
//   优选 on          — the fastest, every round, unconditionally. The reader has said they
//                      want the app to choose.
//   优选 off, 切换 on — whatever is in the field already, unless it has gone slow: above
//                      SMART_SWITCH_AT_MS with something at least SMART_SWITCH_BY_MS quicker
//                      to move to. The reader's own choice is kept until keeping it costs
//                      them something they would notice.
//   neither          — nothing. The fields are the reader's, and a round made for the ranking
//                      is not a reason to touch them.
export function roundChoice(side: Side, route: any, round: Round): PickWin | null {
    const win = side === 'remote' ? round.remote : round.direct;
    if (!win) return null;
    if (route.dns_smart_select) return win;
    if (!route.dns_smart_switch) return null;
    const current = side === 'remote' ? route.remote_dns : route.direct_dns;
    const nowMs = round.latency[current];
    // Three readings, three meanings, and they used to be two:
    //   -1  measured, and it did not answer — the resolver is broken, which is
    //       the one thing 智能切换 exists to notice; any working winner beats it.
    //   undefined / 0  never asked, or asked and nothing came back to record —
    //       no evidence either way, and not a reason to move.
    // Anything else is a real number: move only if it has gone slow.
    //
    // Folding -1 in with "unmeasured" (the old `?? 0` then `<= 0`) is why the
    // switch never replaced a resolver that had stopped answering: the one case
    // it was written for was the one case it refused.
    if (nowMs === -1) return win;
    if (nowMs === undefined || nowMs === 0) return null;
    if (nowMs < SMART_SWITCH_AT_MS) return null;
    if (win.ms > nowMs - SMART_SWITCH_BY_MS) return null;
    return win;
}

// The smart pick itself: ask every candidate on the sides named, keep the fastest of each,
// and hand back every number that came in. What is done with the answer is the caller's —
// the DNS card writes it into the form it is showing, the timer in App writes it into the
// profile on disk — because the two hold different copies of the same profile and there is
// only one measurement to make.
//
// The sides are asked at once because they are independent, and they are told apart on the
// way back: the foreign one has nothing to measure through when no node is running, and that
// is something to say rather than a reason to leave the domestic side unpicked too.
//
// Which sides is a parameter rather than always both, because each table in the card carries
// a button of its own now and the two are not interchangeable: one is measured through the
// running node, the other straight out. A switch or the timer asks for both, which is a round
// over the whole card.
export async function pickFastestDNS(current: {remote?: string; direct?: string} = {},
                             sides: ('remote' | 'direct')[] = ['remote', 'direct']): Promise<Round> {
    // What is in the field is measured too, whether or not it is one of the catalogue's own
    // entries: 智能切换 is about the resolver the reader is actually using, and a hand-typed
    // one is measured by nothing else.
    // `local` is a keyword, not a resolver: it cannot be asked, and adding it
    // to the list made the direct table's "Local" row read -1 — "measured, and
    // it failed" — after every round. It is left out here rather than measured
    // and explained away, so the row shows the dash that means "not measured".
    const withCurrent = (list: string[], value?: string) =>
        value && value !== 'local' && !list.includes(value) ? [...list, value] : list;
    // A side that was not asked for resolves to an empty list rather than to a call: nothing
    // is measured, nothing is published, and nothing is written for it.
    const none: {server: string; ms: number; err?: string}[] = [];
    const ask = (side: 'remote' | 'direct') => side === 'remote'
        ? API.TestDNS(withCurrent(dnsCandidates(REMOTE_DNS_PRESETS, true), current.remote), true)
        : API.TestDNS(withCurrent(dnsCandidates(DIRECT_DNS_PRESETS, false), current.direct), false);
    const [remote, direct] = await Promise.allSettled([
        sides.includes('remote') ? ask('remote') : Promise.resolve(none),
        sides.includes('direct') ? ask('direct') : Promise.resolve(none),
    ]);
    // Every measurement that came in, one per address: what the two tables are ranked by.
    // A resolver that did not answer is a -1, which is "unavailable" in the vocabulary the
    // node table already uses (see latencyText) — and a resolver that was never asked has no
    // entry at all, which is the dash.
    const latency: Record<string, number> = {};
    const fastest = (r: PromiseSettledResult<{server: string; ms: number; err?: string}[]>,
                     presets: DnsPreset[]): PickWin | null => {
        if (r.status !== 'fulfilled') return null;
        for (const x of r.value) latency[x.server] = x.err || x.ms <= 0 ? -1 : x.ms;
        const won = r.value
            .filter(x => !x.err && x.ms > 0 && pickable(presets.find(p => p.value === x.server)))
            .sort((a, b) => a.ms - b.ms)[0];
        return won ? {value: won.server, name: presetName(presets, won.server), ms: won.ms} : null;
    };
    return {
        remote: fastest(remote, REMOTE_DNS_PRESETS),
        direct: fastest(direct, DIRECT_DNS_PRESETS),
        latency,
        note: remote.status === 'rejected' ? errText(remote.reason) : '',
    };
}

export const ROUTE_LISTS = [
    {key: 'direct_domain', label: 'routing.directDomain'},
    {key: 'proxy_domain', label: 'routing.proxyDomain'},
    {key: 'block_domain', label: 'routing.blockDomain'},
    {key: 'direct_ip', label: 'routing.directIP'},
    {key: 'proxy_ip', label: 'routing.proxyIP'},
    {key: 'block_ip', label: 'routing.blockIP'},
];

// The rule items that come up over and over, offered under the list they belong to.
// They are what nekoray's own preset is built from (see PresetRouting on the Go side)
// plus the categories most readers add by hand; anything unusual is still typed into
// the box.
//
// direct_ip carries geoip:cn because that one line is what "everything foreign through
// the node" rests on: with it in the direct list and the default outbound left at proxy,
// domestic addresses go straight out and every other address falls through to the proxy.
// There is no mirror entry for proxy_ip — a rule item cannot be negated here (see
// addRuleSet on the Go side: it strips a leading "!" as decoration, so writing
// "geoip:!cn" would quietly mean plain cn), and a list of every foreign range is not a
// thing this core can be handed.
export const ROUTE_LIST_PRESETS: Record<string, string[]> = {
    direct_domain: [],
    direct_ip: [],
    proxy_domain: ['geosite:geolocation-!cn', 'geosite:google', 'geosite:openai',
        'geosite:telegram', 'geosite:netflix', 'geosite:youtube', 'geosite:github'],
    block_domain: ['geosite:category-ads-all', 'geosite:category-porn'],
};

// 直连规则的国家预设：一面国旗一套完整直连套组——该国域名表（若上游有）、内网、
// Apple 与 Microsoft 一并以默认值加入，网络范围同理（geoip:private 总是随行，内网
// 永远直连）。私有/苹果/微软三个旧文本 chips 因而撤销：它们不再单独提供，而是每面
// 国旗的默认随件。美国在上游没有国家级域名表，这面旗只带网络范围与三张通用表。
export const COUNTRY_PRESETS = [
    {code: 'cn', domain: ['geosite:cn', 'geosite:private', 'geosite:apple', 'geosite:microsoft'], ip: ['geoip:cn', 'geoip:private']},
    {code: 'ru', domain: ['geosite:category-ru', 'geosite:private', 'geosite:apple', 'geosite:microsoft'], ip: ['geoip:ru', 'geoip:private']},
    {code: 'us', domain: ['geosite:private', 'geosite:apple', 'geosite:microsoft'], ip: ['geoip:us', 'geoip:private']},
];

// The cards the routing section draws, in the order it draws them: the three directions
// the egress mode can take. The block card is drawn beside them but is not in this list,
// because it is not a direction — it has settings and switches nothing (see the markup).
// The order is the reader's, not the status bar's: rules first because that is where the
// lists are, then the two one-word modes, and the layer last.
export const ROUTE_CARDS = ['rule', 'global', 'direct'];

// The three sniffing modes, as the Go side numbers them (see the constants in setting.go):
// off, for routing, for the destination. The numbers are what the profile stores, so they
// are the values here too and only the labels are looked up.
export const SNIFF_MODES = [
    {value: '0', label: 'routing.sniffOff'},
    {value: '1', label: 'routing.sniffRouting'},
    {value: '2', label: 'routing.sniffDestination'},
];
