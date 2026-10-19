import type {Component} from 'vue';
import {ArrowRightFilled, CloudFilled, ForbidCircleFilled, LayersFilled} from './icons';

// Log panel categories, in tab order. The ids match the file names the backend
// writes under text-logs/ (see speedtestCategory). Every category streams its
// lines live as they are produced; the test categories are re-read from their
// file as well, when a run ends and when the panel opens — which is why each
// line has exactly one home in the frontend state, and the merge shows it once.
// the categories the log stream carries, in reading order. They are a key for
// the colours now, not a set of tabs: everything is shown at once, so there is
// nothing to switch between.
export const LOG_CATEGORIES = [
    {id: 'core', label: 'logcat.core'},
    {id: 'speedtest-url', label: 'logcat.latency'},
    {id: 'speedtest-tcp', label: 'logcat.tcp'},
    {id: 'speedtest-full', label: 'logcat.full'},
    {id: 'speedtest-download', label: 'logcat.download'},
];

// One line in the log panel. cat is the backend category that produced the
// line ("core" or a speedtest category); the panel colours each differently.
export type LogLine = {line: string; cat: string};

// One line as the panel shows it. key is the identity the line keeps across the
// re-sorts the merged stream goes through: it is what React keys the row by, and what
// the entrance queue uses to tell a line that has just arrived from one that was
// already on screen. See the merge below for why it is the text rather than a place.
export type ShownLine = LogLine & {key: string};

// nowStamp is the timestamp the store puts in front of every line it writes:
// HH:MM:SS.mmm. The panel splits a line on it to dim the time into its own column,
// so a line this side adds has to carry one as well — the log-directory marker was
// the one line in the stream with nothing for the panel to split on.
export const nowStamp = () => {
    const d = new Date();
    const pad = (n: number, width = 2) => String(n).padStart(width, '0');
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
};

// file-backed categories, in no particular order
export const FILE_CATS = ['speedtest-url', 'speedtest-tcp', 'speedtest-full', 'speedtest-download'];

// The time a line carries, as a sortable key. The store stamps "15:04:05.000",
// which is what orders a mixed stream; a line without one keeps its place at the
// end of the stream rather than jumping to the top of it.
export const tsOf = (line: string) => line.match(/^\[?(\d{2}):(\d{2}):(\d{2})(?:[.:](\d{1,3}))?/)?.[0] ?? '';

// The stamp split off a line, so the panel can dim it. What follows the time is
// the message; a bare separator left between them would read as part of it.
export const splitStamp = (line: string): [string, string] => {
    const m = line.match(/^\[?\d{2}:\d{2}:\d{2}[.:]\d{1,3}\]?/);
    if (!m) return ['', line];
    return [m[0], line.slice(m[0].length).replace(/^[\s:：|｜-]+/, '')];
};

// The heading a run is marked with, or null when the line is output. The store
// writes "## 分组 …"; the banner it used to write ("====== 分组 … ======") is
// recognised as well, so a log file written before that change still reads as
// headings rather than as a line of punctuation.
export const sectionText = (line: string): string | null => {
    if (line.startsWith('## ')) return line.slice(3);
    const m = line.match(/^=+\s*(.*?)\s*=+$/);
    return m ? m[1] : null;
};

// The three classes of line the log filter sorts by. Blocked traffic is not one of
// them: the core refuses those matches without logging a line of its own.
// These are sing-box's outbound tags: the core log names the one it chose in the
// line itself ("… outbound=proxy …"), so filtering is a word-boundary match on
// that name rather than a second tagging pass over the stream — the log is text,
// and the tag is already in it. "all" is the unfiltered panel, and it has to be an
// entry of its own or a filter could never be taken back off.
export const LOG_FILTERS = [
    {id: 'all', label: 'logcat.all'},
    {id: 'proxy', label: 'logfilter.proxy'},
    {id: 'direct', label: 'logfilter.direct'},
];

// Boundaries rather than a substring, so "direct" does not match "direction" and
// "proxy" does not match "proxy-protocol" in some other tool's wording.
export const logMatchesFilter = (line: string, filter: string) =>
    filter === 'all' ||
    new RegExp(`(^|[^a-z])${filter}([^a-z]|$)`, 'i').test(line);

// The level a line is shown as — one answer, for the colour it wears and for the level
// the filter files it under. Three steps, because that is how a reader files them: normal
// is the chatter (a core's trace and debug lines are the same news told louder, and a line
// that says nothing about how bad it is is the ordinary course of things), warning is the
// warning step, and error is everything from an error upwards — a fatal or panic line is
// an error the program could not carry on from, not a fourth thing to look for. "all" is
// the unfiltered panel again, and the dropdown's default.
export const LOG_LEVELS = [
    {id: 'all', label: 'loglevel.all'},
    {id: 'normal', label: 'loglevel.normal'},
    {id: 'warn', label: 'loglevel.warn'},
    {id: 'error', label: 'loglevel.error'},
];

export const LINE_LEVEL: Record<string, string> = {
    trace: 'normal',
    debug: 'normal',
    info: 'normal',
    warn: 'warn',
    warning: 'warn',
    error: 'error',
    fatal: 'error',
    panic: 'error',
};

// The words that decide a line's level when the line names no level of its own. These are
// the words the panel has always coloured by — they are what make one of this app's own
// summaries ("启动失败: profile N not found") look like the failure it is — and the filter
// reads the same two lists now, rather than a second and stricter rule of its own.
const ERROR_WORDS = /\b(error|failed|fatal)\b|失败|错误/i;
const WARN_WORDS = /\bwarn(?:ing)?\b|警告/i;

// How loud each step is, for the comparison below.
const LOUDNESS: Record<string, number> = {normal: 0, warn: 1, error: 2};

// One line's level, and the only place that decides it. Never null: every line falls in
// exactly one of the dropdown's three steps, which is what picking a level promises.
//
// The module's own word is read in both shapes sing-box writes it. The main core's log
// spells it "INFO[0048]" (the seconds it has been running), and the TUN front-end's log —
// which the panel tails in line by line — spells it with its own timestamp column in
// front: "ERROR [123456 4ms]" (a connection id and how long it took). That space is the
// whole reason the panel used to disagree with itself: the filter demanded the word and
// the bracket be adjacent, so every front-end line, errors included, was read as "says
// nothing about itself" and filed under 正常, while the body — which coloured by words —
// drew that same line in the alarm colour. A reader who chose 错误 to go and find those
// errors watched them leave the screen instead.
//
// A line with both answers — a level of its own and words that say something else — is
// filed by the louder of the two. It has to be: sing-box writes a resolver's failure at
// INFO ("dns: exchange failed for …"), and a filter for failures that hides the failures
// the program was calm about is the same fault in a smaller size. Reading the words at all
// is what made those lines filterable; reading them as a floor is what keeps a reader
// looking for 错误 from being sent to 正常 for the lines they came for.
export const lineLevel = (line: string): string => {
    const byWord = ERROR_WORDS.test(line) ? 'error' : WARN_WORDS.test(line) ? 'warn' : 'normal';
    const m = line.match(/\b(trace|debug|info|warn(?:ing)?|error|fatal|panic)\s*\[\d+(?:\s|\])/i);
    if (!m) return byWord;
    const written = LINE_LEVEL[m[1].toLowerCase()] ?? 'normal';
    return LOUDNESS[written] >= LOUDNESS[byWord] ? written : byWord;
};

// The same answer for a line as the panel holds it, with the one rule the panel adds: a
// run's heading is a divider between runs rather than output, so it is filed under 正常
// whatever its words happen to be (see sectionText). The filter and the body both go
// through here, so a line's colour and its level cannot drift apart again.
export const lineLevelOf = (line: string): string => {
    const [, msg] = splitStamp(line);
    return sectionText(msg) === null ? lineLevel(msg) : 'normal';
};


// An icon per destination, spoken in the same visual language as the lines: "all" is
// every layer, proxy rides through a cloud, direct is a straight arrow, block is the
// round forbidding sign. These ride the log's context menu, which offers to file the
// line under one of them; the filter itself is a column of words and colours (LOG_FILTERS).
export const FILTER_ICONS: Record<string, Component<{ className?: string }>> = {
    all: LayersFilled,
    proxy: CloudFilled,
    direct: ArrowRightFilled,
    block: ForbidCircleFilled,
};
