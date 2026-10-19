// Best-effort first label of a subscription URL's host, used as the default group
// name: "https://example-sub.example.com/feed/xxx" -> "example-sub". Prefixing a
// scheme when the user pasted a bare host keeps URL() from rejecting it.
// The address a log line is about. Logs state the destination in several shapes —
// "connection to example.com:443", "outbound/direct → 142.250.0.1:80" — so this looks
// for the shape of an address rather than for a position in the line, and takes the
// first one: the line a reader right-clicks is the line whose first address is the one
// they are asking about. File extensions are excluded, because a line about the core
// ("starting sing-box.exe") would otherwise read as a domain — the dot in a filename
// is not a domain's dot.
export const IP_IN_LINE = /\b\d{1,3}(?:\.\d{1,3}){3}\b/;
export const DOMAIN_IN_LINE = /\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}\b/i;
export const NOT_A_DOMAIN = /\.(exe|dll|log|json|txt|conf|dat|db|srs|yaml|yml|js|ts|tsx|css|html|go|md|png|jpg)$/i;

export function addressInLine(line: string): string {
    const ip = line.match(IP_IN_LINE);
    if (ip) return ip[0];
    const m = line.match(DOMAIN_IN_LINE);
    if (!m || NOT_A_DOMAIN.test(m[0])) return '';
    return m[0].toLowerCase();
}

export function hostFirstSegment(raw: string): string {
    const s = (raw ?? '').trim();
    if (!s) return '';
    try {
        const u = new URL(s.includes('://') ? s : `https://${s}`);
        return (u.hostname || '').split('.')[0] || '';
    } catch {
        return '';
    }
}
