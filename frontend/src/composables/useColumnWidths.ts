// The node table's column widths, frozen once per data set — extracted from
// App.tsx (the writeColumnWidths / measureColumns block and its three effects).
import {onBeforeUnmount, onMounted, watch, type Ref} from 'vue';
import type {main} from '../../wailsjs/go/models';
import {LIVE_COLS, LIVE_SLACK, RESIZE_SETTLE_MS, speedCell, TEXT_COLS, trafficCell} from '../lib/metrics';

export function useColumnWidths(deps: {
    tableRef: Ref<HTMLTableElement | null>;
    profiles: Ref<main.ProfileInfo[]>;
    listKey: Ref<number>;
    gid: Ref<number>;
}) {
    // The table gives up width when a panel takes the seat, and that is a change to
    // its container, so it has to be a layout change on every frame of the move.
    // Under the auto table layout that means re-measuring every cell of every row to
    // recompute ten columns, which is enough CPU per frame to drop frames — the
    // table visibly stuttered as it came back. So the widths are measured here, once,
    // under the natural algorithm with the table unconstrained, and then frozen: from
    // then on the same move is arithmetic on ten numbers.
    //
    // Written straight to the DOM rather than through state: it is ten numbers on ten
    // cells, it changes when the data does (a group switch, via listKey) and not on
    // every poll of the traffic counters, and routing it through state would repaint
    // the whole list to change nothing an eye can see.
    //
    // The measurement is taken between two style writes inside a layout effect, so
    // it never reaches the screen: the browser lays out once for the measurement and
    // then once for the frozen table, both before the first paint.
    // The widths the last measurement produced, and the root font size they were taken
    // at. They are what a resize scales from: every type size in the sheet is a fraction
    // of that one number, so a column's natural width is proportional to it and to
    // nothing else — not to the window's width, which is the whole point. Scaling the
    // frozen widths by the ratio of the two sizes is the answer the measurement would
    // give, for none of its cost.
    const natural: {current: {widths: number[]; fs: number} | null} = {current: null};
    // The widest metric text seen since the last calibration, as the signature
    // the widening watcher below compares against: bytes in one unit are the
    // same length wide, so a counter counting up inside "MB" is noiseless,
    // while data arriving, a unit step and a reset are all strokes.
    const metricSig: {current: number} = {current: 0};

    // The half of the column logic that writes rather than measures: ten numbers divided
    // into ten shares and written onto ten cells. Nothing here reads the layout, so a
    // caller that already has the widths can run it every frame of a window resize.
    const writeColumnWidths = (widths: number[], drawn: number) => {
        const t = deps.tableRef.value;
        if (!t) return;
        const ths = Array.from(t.querySelectorAll('thead th')) as HTMLElement[];
        // A list that changed shape under a stale measurement: the columns the widths
        // belong to are not the columns on screen, and writing them would put one
        // group's shares onto another's headings.
        if (ths.length !== widths.length) return;
        const total = widths.reduce((x, y) => x + y, 0) || 1;
        const surplus = drawn - total;
        if (surplus <= 0) {
            // A table narrower than its own content. The proportional shrink this table
            // has always done charged that deficit to every column alike, and on the
            // value columns that charged badly: 'AnyTLS' is one word — show it whole at
            // its natural width or cut one word off, nothing in between — while the three
            // text columns are built to ellipsize and can give up hundreds of pixels
            // without losing the read. So the deficit goes to the text columns first,
            // each down to a small floor, waterfilled by what each still has above it;
            // only any part they cannot pay comes back as one fold applied to every
            // column, which is where deep sub-600px tables land and something simply
            // has to be cut. On a window that fits, nothing is charged and none of this
            // moves.
            const deficit = -surplus;
            // The floor keeps a text column from paying itself out of existence: about
            // one word of mono or two of CJK past the padding, scaling with the window
            // but never under 36px — past that the `title` on the cell carries the value.
            const floor = Math.max(36, drawn * 0.04);
            const textIdx = widths.map((_, i) => i).filter(i => TEXT_COLS.has(i));
            const give = widths.map(() => 0);
            let need = deficit;
            // Proportional-to-remaining-room per pass is the waterfilling: the first
            // pass stops every column together at even progress toward each floor, and
            // a column that hits its floor early drops out of the next pass while the
            // others keep paying. Three columns, one floor each — it converges before
            // the passes run out.
            for (let pass = 0; pass < 8 && need > 0.5; pass++) {
                const pool = textIdx.filter(i => widths[i] - give[i] > floor + 0.5);
                const cap = pool.reduce((a, i) => a + widths[i] - give[i] - floor, 0);
                if (cap <= 0) break;
                const take = Math.min(need, cap);
                pool.forEach(i => {
                    give[i] += take * (widths[i] - give[i] - floor) / cap;
                });
                need -= take;
            }
            const afterText = widths.map((w, i) => w - give[i]);
            const afterSum = afterText.reduce((x, y) => x + y, 0);
            const scale = afterSum > drawn ? drawn / afterSum : 1;
            ths.forEach((th, i) => {
                th.style.width = (afterText[i] * scale / drawn * 100).toFixed(2) + '%';
            });
            return;
        }
        // There is room to give out. The columns with slack borrow from it first — that
        // is what keeps a rate that grows, or a protocol word, from being cut off — and
        // only what they do not need goes to the text columns, in their own proportions.
        // Which means a window that has room for the columns and no more gives it all to
        // those with slack, and the text columns sit at their natural widths rather than
        // being squeezed to pay for a headroom they were never asked about.
        const liveNeed = ths.reduce((a, _, i) => a + (LIVE_COLS.has(i) ? widths[i] * (LIVE_SLACK - 1) : 0), 0);
        const liveGiven = Math.min(liveNeed, surplus);
        const liveRoom = liveNeed > 0 ? liveGiven / liveNeed : 0;
        const textSurplus = surplus - liveGiven;
        const textNeed = ths.reduce((a, _, i) => a + (TEXT_COLS.has(i) ? widths[i] : 0), 0);
        ths.forEach((th, i) => {
            let px = widths[i];
            if (LIVE_COLS.has(i)) px += widths[i] * (LIVE_SLACK - 1) * liveRoom;
            else if (TEXT_COLS.has(i) && textNeed > 0) px += textSurplus * (widths[i] / textNeed);
            th.style.width = (px / drawn * 100).toFixed(2) + '%';
        });
        // No min-width is written, and none is wanted: a floor here would put the
        // overflow back, and the table would stop shrinking with the window.
    };

    // The measurement itself, taken at the two moments that need it: the list arriving
    // under the table, and the window holding still after being resized (see the resize
    // effect). The root font size follows the window (see style.css), so the columns'
    // natural widths do too, and a list measured at one window is out at another — but
    // only in proportion to that one number, which is what lets the resize scale from the
    // last measurement instead of repeating this.
    //
    // What neither moment does is re-render. This used to be a counter in state that the
    // window's resize event incremented, and an effect keyed on it, which meant every
    // frame of a dragged window edge re-rendered the whole application to go and set ten
    // widths that only ever reach the DOM.
    const measureColumns = () => {
        const t = deps.tableRef.value;
        if (!t) return;
        const ths = Array.from(t.querySelectorAll('thead th')) as HTMLElement[];
        if (!ths.length) return;
        // The headings are where a column width starts, not where it ends: a
        // metric cell may be wider than its own caption (a traffic counter that
        // started counting is "↑ 999.9 MB / ↓ 999.9 MB" against "流量"), and a
        // width measured from the heading alone would crop that cell under the
        // frozen shares. The same column's cells are folded into the max below.
        // Nothing to measure until the list has rows. The first run of this happens on
        // mount, before loadProfiles has answered, and a table measured empty hands back
        // the headings: every column came out the width of its own caption, and the type
        // column — 37px, which is "类型" and its padding — left the badge about twenty
        // pixels for a word that needs forty. The widths written from that measurement are
        // absolute shares, so they did not quietly scale down: they stayed, and the badge
        // showed "V…" until the list was measured again.
        //
        // A row of the list, not any row: the "no nodes" / "no match" placeholder is also
        // a tr, and measuring under it measures one colSpan=10 cell — the placeholder's own
        // words distributed over ten headings, which put 29% of the table under 订阅名称.
        // The placeholder carries no column content, so there is nothing there to measure.
        if (!t.querySelector('tbody tr[data-pid]')) return;
        // How wide the table is drawn, taken before the measurement below flips it: the
        // share a column wants is its own width over the row it will actually get.
        const drawn = Math.max(1, t.clientWidth);
        const prevLayout = t.style.tableLayout;
        const prevWidth = t.style.width;
        // max-content is what asks for the natural width: without it the auto layout
        // would hand back the widths the current container allows, and a container
        // that is mid-gutter would be measured instead of the content.
        t.style.tableLayout = 'auto';
        t.style.width = 'max-content';
        // In max-content, a column is as wide as the widest of its own cells;
        // the placeholder row carries one colSpan across all ten columns and is
        // skipped for that — its words are not the trunk any column should grow
        // out of.
        const bodyRows = Array.from(t.tBodies[0]?.rows ?? []);
        const tdOf = (row: HTMLTableRowElement, i: number): HTMLElement | null => {
            if (row.cells.length !== ths.length) return null;
            return row.cells[i] as HTMLElement;
        };
        const widths = ths.map((th, i) => {
            let w = Math.ceil(th.getBoundingClientRect().width);
            for (const row of bodyRows) {
                const td = tdOf(row, i);
                if (td) {
                    w = Math.max(w, Math.ceil(td.getBoundingClientRect().width));
                }
            }
            return w;
        });
        t.style.tableLayout = prevLayout;
        t.style.width = prevWidth;
        // Kept for the resize to scale from. The font size is sampled here rather than at
        // the resize because this is the one moment the two are known to belong together.
        natural.current = {
            widths,
            fs: parseFloat(getComputedStyle(document.documentElement).fontSize) || 0,
        };
        writeColumnWidths(widths, drawn);
    };

    // The two moments this has to be called from: the list arriving under it, and the
    // window settling after a resize (the resize effect below). The row count is in the
    // first because the list comes asynchronously — the measurement finds the table empty
    // and returns, and nothing else would bring it back, since neither listKey nor a
    // settled resize has happened in between.
    // Length rather than the list itself: the traffic poll replaces the array every 1.5s,
    // and re-measuring ten headers on every poll is the cost this freeze exists to avoid.
    // gid is in there for the one other change of list that keeps the count: a hover
    // switch to a subscription with the same number of nodes is a different table, and
    // its columns — an address column above all — can be wider or narrower than the ones
    // measured. The poll never changes it, so it costs nothing on that path.
    onMounted(() => measureColumns());
    watch([deps.listKey, () => deps.profiles.value.length, deps.gid], () => {
        measureColumns();
    }, {flush: 'post'});

    // The metric columns live on their cells, not on the headings: a traffic
    // counter starting to count ("-" becomes "↑ 12 B / ↓ 3 B") widens the cell
    // past anything a heading-driven calibration has seen, and now that the
    // calibration folds the cells in (see measureColumns), a change in the
    // widest cell's text is the moment to re-run it. The signature is the
    // longest metric text of any row, each part in its own fixed format — so
    // a counter counting up inside one unit changes nothing, while the
    // arrival of data, a unit step or a cleared column each does. One
    // layout per step, none at rest.
    watch(deps.profiles, profiles => {
        const t = deps.tableRef.value;
        if (!t || !t.querySelector('tbody tr[data-pid]')) return;
        let sig = 0;
        for (const p of profiles) {
            const len = trafficCell(p.up, p.down).length + speedCell(p.speed).length;
            if (len > sig) sig = len;
        }
        if (sig === metricSig.current) return;
        metricSig.current = sig;
        measureColumns();
    }, {flush: 'post', immediate: true});

    // The measurement races the bundled face. The address column is set in JetBrains Mono
    // NF (see --font-mono), and that face arrives with font-display: swap: a fallback is
    // painted first and the real font swapped in once the file has loaded. A column
    // measured in between is measured in the wrong metrics — the same hostname measured
    // 395px in the fallback and 430px in the real font — and the frozen widths are then
    // shares of a table that was never on screen. Nothing about the list changes when the
    // swap lands, so nothing measured again: the address column stayed 45px narrow and
    // ellipsised hostnames until a window resize happened to re-measure it.
    //
    // load() rather than ready(): ready settles when nothing is pending, and before the
    // list arrives nothing on screen is asking for the face — so it had already resolved
    // by the time the first measurement ran, and there was no second chance to take.
    // load() asks for the face itself and settles when the file is in, which is the
    // moment the swap lands; measuring once more then is one layout, once per run. If the
    // face is already in, this is the same one layout measuring what is already correct.
    let fontLoadAlive = true;
    onMounted(() => {
        const fonts = document.fonts;
        if (!fonts) return;
        fonts.load('500 1em "JetBrains Mono NF"')
            .then(() => {
                if (fontLoadAlive) measureColumns();
            })
            .catch(() => {
                // A face that cannot load is a face the fallback is already standing in
                // for; measuring again would measure the same fallback.
            });
    });
    onBeforeUnmount(() => {
        fontLoadAlive = false;
    });

    // The resize redraws; it does not measure. Measuring here means flipping the table to
    // `max-content` and reading nine header cells back, which lays the whole list out
    // twice — about thirteen milliseconds inside the handler, once for every frame of a
    // maximise, which is what made the window stutter as it opened. Nothing about the
    // columns depends on the window's width, only on the root font size, so the widths of
    // the last measurement are scaled by the ratio of the two sizes and written back; the
    // exact measurement is taken once the move has stopped, where it costs nothing an eye
    // or a frame can see.
    let frame = 0;
    let settle = 0;
    const onResize = () => {
        if (!frame) {
            frame = requestAnimationFrame(() => {
                frame = 0;
                const nat = natural.current;
                const t = deps.tableRef.value;
                if (!nat || !t) return;
                const fs = parseFloat(getComputedStyle(document.documentElement).fontSize) || nat.fs;
                const k = nat.fs > 0 ? fs / nat.fs : 1;
                writeColumnWidths(nat.widths.map(w => w * k), Math.max(1, t.clientWidth));
            });
        }
        clearTimeout(settle);
        settle = window.setTimeout(measureColumns, RESIZE_SETTLE_MS);
    };
    let tableRo: ResizeObserver | null = null;
    onMounted(() => {
        window.addEventListener('resize', onResize);
        // The table's own box is watched as well as the window: the drawer seat
        // (settings/subscriptions) and the log panel promise no resize event of
        // the window, yet they change the width the table has to live in — an
        // observer answers every such move with the same two-half response.
        tableRo = new ResizeObserver(onResize);
        if (deps.tableRef.value) tableRo.observe(deps.tableRef.value);
    });
    onBeforeUnmount(() => {
        window.removeEventListener('resize', onResize);
        tableRo?.disconnect();
        if (frame) cancelAnimationFrame(frame);
        clearTimeout(settle);
    });

    return {writeColumnWidths, measureColumns};
}
