// The log panel, extracted from App.tsx: the stream state, the merge and the two
// filters, the follow-the-end scrolling, the batching of the live stream, and the
// panel's own actions (clear / copy / save-as-route / export / open).
import {computed, onScopeDispose, ref, watch} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {setting} from '../../wailsjs/go/models';
import {t} from '../i18n';
import {tip} from '../tips';
import {applyRoute, errText} from '../lib/shared';
import {addressInLine, IP_IN_LINE} from '../lib/text';
import {FILE_CATS, lineLevelOf, logMatchesFilter, tsOf, type LogLine, type ShownLine} from '../lib/logfilter';
import {LOG_FOLLOW_SLACK_PX} from '../lib/logpanel';

type Notify = (text: string, kind?: 'ok' | 'err') => void;

export function useLogs(notify: Notify) {
    const logs = ref<LogLine[]>([]);
    const showLogs = ref<boolean>(false);
    // The log panel's filter. "all" is the panel as it was: every category, every
    // line. The choices are a column beside the log rather than a menu over it, so
    // there is no open/closed to keep track of and nothing to dismiss.
    const logFilter = ref('all');
    // The other narrowing of the same list: how loud a line is. "all" is the whole
    // stream; the three below it are the levels a reader files lines by, and one of
    // them shows that level and nothing else. The two filters are separate axes of
    // the one list — a reader chasing a failure wants it by route as much as by how
    // loud it was — so they multiply rather than replace each other.
    const logLevel = ref('all');
    // The panel shows every category in one stream, so there is no "which one"
    // any more — only the two halves it is built from: the live core buffer and
    // the day's file behind each test category.
    const catLogs = ref<Record<string, LogLine[]>>({});

    // Whether the reader has stopped the panel from following the tail. A stream that
    // scrolls under the eye is unreadable the moment one line of interest arrives, and
    // holding it still is the only way to read that line: nothing is dropped while this is
    // on, the view simply stops moving.
    //
    // Declared here rather than beside the panel's other state further down, because the
    // list below is what it acts on: visibleLogs freezes the stream, and a hook cannot read
    // a variable declared after it.
    const logPaused = ref(false);

    // The stream the panel shows: every category together, ordered by the time each
    // line carries. A line with no time keeps its place at the end instead of jumping
    // to the top, and the sort is stable, so the order the backend wrote two lines in
    // survives even when their stamps match to the millisecond.
    const mergedLogs = computed<ShownLine[]>(() => {
        const all: LogLine[] = [...logs.value, ...FILE_CATS.flatMap(c => catLogs.value[c] ?? [])];
        all.sort((a, b) => {
            const ta = tsOf(a.line);
            const tb = tsOf(b.line);
            if (!ta) return tb ? 1 : 0;
            if (!tb) return -1;
            return ta.localeCompare(tb);
        });
        // The identity each line keeps, assigned here because this is where the order is
        // decided: the line's own text, plus which occurrence of that text it is. A place
        // in the list would not do — the stream is re-sorted by time on every arrival, so
        // a line that lands in the middle moves every line under it onto a different row,
        // and the rows the panel then saw as new were the ones least recently arrived.
        // That is what a line popping out ahead of its turn was: not the entrance running
        // early, but the wrong line being given one.
        const seen = new Map<string, number>();
        return all.map((l): ShownLine => {
            const base = `${l.cat}\u0000${l.line}`;
            const n = seen.get(base) ?? 0;
            seen.set(base, n + 1);
            return {...l, key: n === 0 ? base : `${base}\u0000${n}`};
        });
    });

    // Pausing holds the stream itself still, not just the scrollbar. Holding scrollTop
    // alone was not enough, and the reason is the merge below it: the stream is re-sorted
    // by timestamp as lines arrive, so a line carrying an earlier stamp is inserted *above*
    // what the reader is looking at and pushes it down. The box does not move and no scroll
    // event fires — but the text in it does change, which is what "it keeps scrolling"
    // reads as. A snapshot of the list, taken when the pause went down, is what actually
    // stops it.
    //
    // Taken before the filter, so the filter keeps working while paused: what is frozen is
    // the stream, and which part of it is on show is still the reader's to choose. Lines
    // that arrive while paused are not lost — logs and catLogs keep filling up behind this —
    // so resuming shows all of them at once.
    let frozenLogs: ShownLine[] | null = null;
    const visibleLogs = computed<ShownLine[]>(() => {
        if (!logPaused.value) {
            frozenLogs = null;
            return mergedLogs.value;
        }
        if (!frozenLogs) frozenLogs = mergedLogs.value;
        return frozenLogs;
    });

    // What the panel shows: the whole stream, narrowed by the class of line it is and by
    // how loud it is. The two are asked of the one list rather than being alternatives,
    // so a reader can have the errors of the proxy while the direct ones are out of the
    // way.
    //
    // The level comes from lineLevelOf and from nowhere else — the same call the body
    // colours by (see the line list below) — and that is what makes the dropdown mean what
    // it looks like it means. The two used to be separate rules: the filter read only the
    // main core's own "INFO[0048]" shape and filed every line the TUN front-end writes —
    // its "ERROR [123456 4ms]" — under 正常, while the body, which coloured by words,
    // painted those same lines in the alarm colour. A reader who chose 错误 to go and find
    // them watched them leave the screen.
    //
    // A line that names no level falls back to its own words, and a run's heading is 正常
    // whatever it says, which keeps the three levels total: every line is in exactly one
    // of them, so picking a level never leaves something on screen that is not that level.
    // This is also the list the body renders and the clipboard copies, so there is no
    // second notion of "the log" anywhere.
    const shownLogs = computed<ShownLine[]>(() => {
        const byClass = logFilter.value === 'all'
            ? visibleLogs.value
            : visibleLogs.value.filter(l => logMatchesFilter(l.line, logFilter.value));
        if (logLevel.value === 'all') return byClass;
        return byClass.filter(l => lineLevelOf(l.line) === logLevel.value);
    });

    // Which categories have fresh lines: the legend dot breathes while a
    // category's stream is arriving and goes still shortly after it stops.
    // freshness is decided by the unfiltered stream (mergedLogs), so switching
    // the outbound filter never starts or stops a breath by itself.
    const catLive = ref<Record<string, boolean>>({});
    const catLiveTimers: Record<string, number> = {};
    const prevLogLen = {current: 0};
    watch(mergedLogs, (merged) => {
        const prev = prevLogLen.current;
        prevLogLen.current = merged.length;
        // A shrink is a clear or a restart: nothing is fresh any more.
        if (merged.length === 0 || merged.length <= prev) {
            if (Object.keys(catLive.value).length) catLive.value = {};
            return;
        }
        const cats = new Set<string>();
        for (const l of merged.slice(prev)) cats.add(l.cat);
        if (cats.size === 0) return;
        let changed = false;
        const next = {...catLive.value};
        for (const c of cats) {
            if (!next[c]) {
                next[c] = true;
                changed = true;
            }
        }
        if (changed) catLive.value = next;
        // Hold the breath while lines keep coming; the last arrival lets the
        // current cycle run out and the dot settle.
        for (const c of cats) {
            clearTimeout(catLiveTimers[c]);
            catLiveTimers[c] = window.setTimeout(() => {
                delete catLiveTimers[c];
                if (catLive.value[c]) catLive.value = {...catLive.value, [c]: false};
            }, 1500);
        }
    });
    // The breaths are per category, and each one runs its own course: a
    // category whose lines stopped goes still on its own timer, whatever the
    // other categories are doing.
    //
    // They used to be cleared by the watcher's own cleanup, which runs before
    // the watcher's next turn — and that turn is about a different batch. The
    // timer that would have put a dot out was thrown away with it, so the
    // category's dot stayed lit for good: with two categories streaming at
    // once — the core's lines and a speed test's — one of them could never go
    // still. The timers are cleared here instead, when the scope ends.
    onScopeDispose(() => {
        for (const t of Object.values(catLiveTimers)) clearTimeout(t);
    });

    // context menu over the log content; clearing lives here rather than on a
    // header button because it is destructive and only relevant while the log is
    // actually on screen
    // line carries the text that was right-clicked, when a line was: the menu then
    // offers to turn the address in it into a routing rule (see addLogLineToRoute).
    // It is absent when the click landed on the panel's own background.
    const logCtx = ref<{ x: number; y: number; line?: string } | null>(null);

    // One view, so one clear: the memory buffer plus the day's file behind every
    // test category. Anything less would leave lines on screen that the button in
    // the log's own menu had just claimed to remove.
    const clearLogs = () => {
        logCtx.value = null;
        // The backend's clear is caught rather than left floating: these are
        // fire-and-forget bindings, and a failed one was an unhandled rejection
        // besides — with the view cleared anyway, lines that are still on disk came
        // back on the next open and nothing had said why.
        void Promise.all([
            API.ClearLogs(),
            ...FILE_CATS.map(c => API.ClearCategoryLogs(c)),
        ]).catch(e => notify(errText(e), 'err'));
        logs.value = [];
        catLogs.value = {};
    };

    // Copies exactly what the panel shows, so a bug report carries the lines
    // that were on screen — including which category they came from. The async
    // clipboard API is unavailable on the app's custom scheme, so the backend's
    // clipboard binding is the fallback rather than an error.
    const copyLogs = async () => {
        logCtx.value = null;
        const lines = shownLogs.value.map(l => l.line);
        if (lines.length === 0) {
            notify(tip('log.nothingToCopy'));
            return;
        }
        const text = lines.join('\n');
        try {
            await navigator.clipboard.writeText(text);
            notify(tip('log.copied'));
        } catch {
            try {
                await API.WriteClipboard(text);
                notify(tip('log.copied'));
            } catch {
                notify(tip('log.copyFailed'), 'err');
            }
        }
    };

    // The one action a log line offers: the address in it becomes a rule of the active
    // routing profile. It is how "the connection to example.com went direct" turns
    // into the proxy-list entry that should have been there, without copying the name
    // into the routing card by hand — the gesture nekoray's log menu calls "save as
    // route", kept here on the line the reader is actually looking at. Which list it
    // goes into is the reader's word, and the address decides whether it is written as
    // a domain or as an IP: the two fields are separate in the profile, and a domain
    // put in the IP list would simply never match.
    const addLogLineToRoute = async (line: string, list: 'direct' | 'proxy' | 'block') => {
        logCtx.value = null;
        const value = addressInLine(line);
        if (value === '') {
            notify(tip('route.nothingFound'), 'err');
            return;
        }
        try {
            const name = await API.GetActiveRouting();
            if (!name) {
                notify(tip('route.none'), 'err');
                return;
            }
            const r: any = await API.GetRouting(name);
            const key = `${list}_${IP_IN_LINE.test(value) ? 'ip' : 'domain'}`;
            const lines = String(r[key] ?? '')
                .split('\n')
                .map((s: string) => s.trim())
                .filter((s: string) => s !== '');
            if (lines.includes(value)) {
                notify(tip('route.exists', value));
                return;
            }
            lines.push(value);
            await applyRoute(name, new setting.Routing({...r, [key]: lines.join('\n')}));
            notify(tip('route.added', t(`log.route.${list}`), value));
        } catch (e) {
            notify(errText(e), 'err');
        }
    };

    // Saving what is on screen. The text comes from the same list the panel draws, so
    // what lands in the file is exactly what was being read — category, search and order
    // included. The dialog belongs to the backend, and a dismissed one comes back as an
    // empty path rather than as a failure.
    const exportLogs = async () => {
        logCtx.value = null;
        try {
            const text = shownLogs.value.map(l => l.line).join('\n') + '\n';
            const path = await API.ExportLogs(text);
            if (path) notify(tip('log.exported', path));
        } catch (e) {
            notify(errText(e), 'err');
        }
    };

    const logRef = ref<HTMLDivElement | null>(null);
    let logReload = false;
    // Whether the panel is still following the end of the log — which it stops doing the
    // moment the reader takes the box somewhere else, and takes up again when they come back
    // — and the position the panel's own rolling last wrote, which is how a scroll is known
    // to be the reader's rather than the panel's.
    let logFollow = true;
    let logWrote = Number.NaN;

    // --- the column ---
//
// Plain text on a plain scroll box: rows are laid out where the stream puts
// them, the box's scrollTop is all the ride a line gets, and the panel follows
// the end of the log directly.


    // Where the end of the log is, in the box's own coordinates — which is not what
    // scrollHeight says. A row tilted on the drum is a taller rectangle than the one it was
    // laid out in, and the browser counts that as scrollable overflow: the box's scrollHeight
    // moved by hundreds of pixels as the tilt changed with the scroll, and a roll aiming at a
    // wandering end rocks back and forth instead of turning. The rows' layout is the end —
    // offsetTop is a layout figure, unaffected by the transforms above it, and it is measured
    // against this box because the box is positioned (see .console-body in App.css).
    const logEnd = (box: HTMLElement) => {
        const last = box.lastElementChild as HTMLElement | null;
        if (!last) return 0;
        const padding = parseFloat(getComputedStyle(box).paddingBottom) || 0;
        return Math.max(0, last.offsetTop + last.offsetHeight + padding - box.clientHeight);
    };

    // The panel is resized while nothing scrolls, and a follow that is still on
    // must keep the view at the end of the log across the change rather than
    // leave it at the height the box used to have.
    watch([showLogs, logPaused], (_, __, onCleanup) => {
        if (!showLogs.value) return;
        const box = logRef.value;
        if (!box) return;
        const ro = new ResizeObserver(() => {
            if (logFollow && !logPaused.value) {
                box.scrollTop = logEnd(box);
                logWrote = box.scrollTop;
            }
        });
        ro.observe(box);
        onCleanup(() => ro.disconnect());
    }, {flush: 'post', immediate: true});

    // The newest line is the bottom of the box, and the box is taken there whenever one
    // arrives: this stream reads downwards, and a panel that keeps its own place while
    // lines land below the fold is one the reader has to chase.
    const rollToEnd = () => {
        const el = logRef.value;
        if (!el) return;
        el.scrollTop = logEnd(el);
        logWrote = el.scrollTop;
    };

    // What a scroll of the box means: whose it is, and whether the panel is still following
    // the end of the log or has been taken somewhere else by the reader.
    const onLogScroll = () => {
        const el = logRef.value;
        if (!el) return;
        // The panel's own writes land on the value it last wrote; anything else is the
        // reader's — a wheel, the bar, a selection dragging the view along with it.
        if (Math.abs(el.scrollTop - logWrote) <= 2) return;
        if (logEnd(el) - el.scrollTop < LOG_FOLLOW_SLACK_PX) {
            // Back at the end of it: following again, which is the one place it resumes.
            if (!logFollow) {
                logFollow = true;
                if (!logPaused.value) rollToEnd();
            }
            return;
        }
        // And the reader has taken the view away: everything they do with the box
        // between here and the end of the log is theirs alone.
        logFollow = false;
    };

    // Laid out rather than painted (useLayoutEffect): the follow is applied in the
    // same frame the new line takes its place.
    watch([shownLogs, logPaused], () => {
        const el = logRef.value;
        if (!el) return;
        if (logPaused.value) return;
        const fresh = logReload;
        logReload = false;
        if (fresh) {
            logFollow = true;
        }
        if (!logFollow) {
            // The reader is at some other point of the log: the arrivals land below
            // the fold and wait for them to come back to the end of it.
            return;
        }
        rollToEnd();
    }, {flush: 'post'});

    // Core logs arrive one line at a time, and a busy node writes dozens a second. Each
    // one used to replace the whole list — a new array into a sort over up to a thousand
    // lines, and a re-render of this window — with the panel closed. Batching keeps the
    // text live and the work bounded; 120ms is below what reads as lag in a log.
    const pendingLogs: LogLine[] = [];
    let logTimer: number | undefined;

    // One update for the lines that arrived since the last beat (see pendingLogs).
    const drainLogs = () => {
        logTimer = undefined;
        // The batch is the queue's own contents, taken out of it. Truncating
        // the queue in place does not empty a copy of it: `batch` and
        // `pendingLogs` are one and the same array, so `pendingLogs.length = 0`
        // emptied the batch too — and every line that had arrived was dropped
        // on the floor before it could reach the panel. The panel therefore
        // only ever showed the snapshot it read when it was opened, and the
        // live stream never moved. splice(0) hands the lines over in a new
        // array and leaves the queue empty, which is what this wanted.
        const batch = pendingLogs.splice(0);
        if (!batch.length) return;
        const main: LogLine[] = [];
        const byCat: Record<string, LogLine[]> = {};
        for (const l of batch) {
            if (FILE_CATS.includes(l.cat)) {
                if (!byCat[l.cat]) byCat[l.cat] = [];
                byCat[l.cat].push(l);
            } else {
                main.push(l);
            }
        }
        if (main.length) logs.value = [...logs.value.slice(-999), ...main];
        const cats = Object.keys(byCat);
        if (cats.length) {
            const next = {...catLogs.value};
            for (const c of cats) next[c] = [...(next[c] ?? []), ...byCat[c]];
            catLogs.value = next;
        }
    };

    // The core:log event handler (App.vue wires it): a line's category decides which
    // half of the stream it belongs to, and it belongs to exactly one of them — the
    // test categories live in catLogs, where the file re-read puts the same text once
    // the run ends. Landing in `logs` as well was how an open panel showed every test
    // line twice — the merge adds the two halves together, and the streamed copy and
    // the file copy carry the very same stamped text.
    const queueLogLine = (line: string, cat: string) => {
        pendingLogs.push({line, cat: cat || 'core'});
        if (logTimer) return;
        logTimer = window.setTimeout(drainLogs, 120);
    };

    // The teardown of the event wiring in App.vue: the lines still in the batch
    // belong to the stream and are handed over rather than dropped.
    const flushPendingLogs = () => {
        if (logTimer) {
            clearTimeout(logTimer);
            drainLogs();
        }
    };

    // Re-read the test categories' files. Every speed test truncates and rewrites
    // them, so this is not a cache to be trusted between runs: a run that finished
    // a moment ago has to be able to replace the output of the one before it.
    const loadFileCats = async () => {
        // A stream that is being replaced is not a run of arrivals: the view goes to its
        // end at once rather than being glided through the whole of it. See logReloadRef.
        logReload = true;
        try {
            const all = await Promise.all(FILE_CATS.map(async c =>
                ((await API.GetCategoryLogs(c)) ?? []).map(l => ({line: l, cat: c}))
            ));
            const next: Record<string, LogLine[]> = {};
            FILE_CATS.forEach((c, i) => {
                next[c] = all[i];
            });
            // A flush goes through the backend before the read, so the file holds the
            // whole run the moment it ends; the streamed copies this replaces are the
            // same stamped text, and the line they belonged to is the live half of the
            // panel until this read lands.
            catLogs.value = next;
        } catch (e) {
            catLogs.value = {};
            notify(errText(e), 'err');
        }
    };

    // Opening the panel re-reads the core buffer as well, so coming back to it
    // never presents a stale snapshot of the part that does not stream.
    const loadAllLogs = async () => {
        logReload = true;
        try {
            logs.value = ((await API.GetLogs()) ?? []).map(l => ({line: l, cat: 'core'}));
        } catch {
            // the live stream is still arriving, so there is nothing to report
        }
        await loadFileCats();
    };

    // Opening the log panel re-reads it, so coming back never presents a stale
    // snapshot. The refresh is part of what "open" means rather than a detail of the
    // button that asks for it, which is why the one entry left — the status bar's —
    // goes through here too.
    const toggleLogs = () => {
        const next = !showLogs.value;
        if (!next) {
            // The panel goes in the frame it is asked for: no exit to wait out, no
            // closing class to hold it mounted.
            showLogs.value = false;
            return;
        }
        showLogs.value = true;
        loadAllLogs();
    };

    return {
        logs, catLogs, showLogs, logFilter, logLevel, logPaused,
        mergedLogs, shownLogs, catLive,
        logRef, onLogScroll, rollToEnd,
        clearLogs, copyLogs, addLogLineToRoute, exportLogs,
        queueLogLine, drainLogs, flushPendingLogs,
        loadFileCats, loadAllLogs, toggleLogs,
        // logCtx is shared with App.vue's context-menu plumbing (see below).
        logCtx,
    };
}
