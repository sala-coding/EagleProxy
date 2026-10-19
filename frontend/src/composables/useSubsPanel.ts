// The subscriptions drawer's width and the name column it measures, extracted
// from App.tsx (the panel grip block and the nameCols block).
import {computed, onBeforeUnmount, onMounted, ref, watch, type Ref} from 'vue';
import {clampPanelW, PANEL_W_SHARE} from '../lib/layout';
import {RESIZE_SETTLE_MS} from '../lib/metrics';
import {SUB_NAME_COLS, SUB_NAME_COLS_MIN, SUB_NAME_COLS_MAX} from '../lib/subdraw';

export function useSubsPanel(subsDrop: Ref<boolean>) {
    // --- the subscriptions drawer's width ---

    // Two states, and the difference is the point: null means "automatic" — a share of
    // the window, recomputed while the window changes — while a number is the reader's
    // own drag, which is kept as it was made and must not be re-derived from the window
    // behind their back. A stored value is clamped on read rather than trusted, so a
    // number left over from a larger screen can never make the table beside it unusable.
    // Nothing is stored in the automatic state: the absent key is what says "the share,
    // please".
    const savedW = Number(window.localStorage.getItem('panel-w'));
    const panelWUser = ref<number | null>(savedW > 0 ? clampPanelW(savedW) : null);
    // The width the drawer is actually drawn at: the reader's own, or the share of the
    // window that the floor and the ceiling leave standing. The share used to be computed
    // here from a subscribed window width, which meant a setState on every frame of a
    // resize to produce a number the stylesheet was about to compute anyway — see --panel-w,
    // which does it in a clamp() and cannot fall out of step with the window. What is left
    // to read the window for is the separator's aria-valuenow, and that is a snapshot: it
    // reads the width as of the last render rather than following a drag of the window's
    // edge, which costs a re-render per frame to keep current. The drawn width never
    // depends on it.
    // (Vue: the share is read off a winW ref kept current by the resize listener below —
    // the same snapshot a React render took of window.innerWidth.)
    const winW = ref(window.innerWidth);
    const panelW = computed(() => panelWUser.value ?? clampPanelW(winW.value * PANEL_W_SHARE));
    const panelResizing = ref(false);
    const panelRef = ref<HTMLDivElement | null>(null);
    // Where the pointer went down and how wide the drawer was then, so every move
    // is measured from the start rather than accumulated from the last one — the
    // clamp must not eat a little of every step.
    let panelDrag: {x: number; w: number} | null = null;

    const onWinResize = () => {
        winW.value = window.innerWidth;
    };
    onMounted(() => {
        window.addEventListener('resize', onWinResize);
    });
    onBeforeUnmount(() => {
        window.removeEventListener('resize', onWinResize);
    });

    const onPanelGripDown = (e: PointerEvent) => {
        const el = panelRef.value;
        if (!el) return;
        e.preventDefault();          // no text selection along the dragged edge
        try {
            (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
        } catch { /* the drag runs without it */ }
        panelDrag = {x: e.clientX, w: el.getBoundingClientRect().width};
        panelResizing.value = true;
    };

    const onPanelGripMove = (e: PointerEvent) => {
        const d = panelDrag;
        if (!d) return;
        // Right makes the drawer wider: the right edge follows the pointer and the
        // drawer grows away from the rail, which does not move.
        panelWUser.value = clampPanelW(d.w + (e.clientX - d.x));
    };

    const onPanelGripUp = (e: PointerEvent) => {
        if (!panelDrag) return;
        panelDrag = null;
        panelResizing.value = false;
        try {
            const tgt = e.currentTarget as HTMLElement;
            if (tgt.hasPointerCapture?.(e.pointerId)) {
                tgt.releasePointerCapture(e.pointerId);
            }
        } catch { /* nothing left to release */ }
        // Written on release rather than on every move: localStorage is synchronous
        // and a drag would otherwise hit it once per frame. What is stored is the
        // state value, not the measured one — the measured width is the stylesheet
        // clamping this draw to fit the current window, and persisting that would
        // quietly rewrite the reader's width down to whatever this window happened
        // to allow, for good.
        // And only when something was actually dragged. A pointer that landed on the
        // grip and came up again is not a choice of width: writing the automatic share
        // out as a user width is what made the drawer stop following the window for
        // good — panelWUser is null until a move happens (see onPanelGripMove), and the
        // way back is Home or a double click, which a reader who never dragged has no
        // reason to look for.
        if (panelWUser.value !== null) {
            window.localStorage.setItem('panel-w', String(panelW.value));
        }
    };

    // The same adjustment from the keyboard, because a separator you can only drag
    // is a separator some people cannot use. Shift for the larger step, and Home to hand
    // the width back to the window's share of it.
    const onPanelGripKey = (e: KeyboardEvent) => {
        const step = e.shiftKey ? 48 : 16;
        // Home is not a width: it is the way back to the automatic share, which is what
        // the drawer has before anyone sizes it (see panelWUser). Written as the absence
        // of the key rather than as the number that share happens to work out to now, so
        // it stays automatic the next time the window changes size.
        if (e.key === 'Home') {
            e.preventDefault();
            panelWUser.value = null;
            window.localStorage.removeItem('panel-w');
            return;
        }
        // the drawn width rather than 0 when the panel cannot be measured: a zero here
        // would clamp up to the floor and the first keypress would jump the drawer to its
        // narrowest
        const cur = panelRef.value?.getBoundingClientRect().width ?? panelW.value;
        let next: number;
        if (e.key === 'ArrowRight') next = cur + step;
        else if (e.key === 'ArrowLeft') next = cur - step;
        else return;
        e.preventDefault();
        const w = clampPanelW(next);
        panelWUser.value = w;
        window.localStorage.setItem('panel-w', String(w));
    };

    // The double-click on the grip: hand the width back to the automatic share.
    const resetPanelW = () => {
        panelWUser.value = null;
        window.localStorage.removeItem('panel-w');
    };

    // How wide the name column is, in display columns — the same ten-ish the standard
    // asks for, but taken from the row it is drawn in rather than fixed at it.
    //
    // Fixed was wrong twice over: the reader drags the drawer, and a narrow enough window
    // has the stylesheet clamp it, and a column of cards that kept its ten through both
    // would spend a narrow drawer on filler while leaving a wide one's room unused. What
    // the count has to be is what the row can hold.
    //
    // Measured off the row itself — the first card's name cell, less its padding, the
    // hairline, the row's gap and the slot at the end — rather than worked out from the
    // drawer's width and a list of the parts, which would be a second copy of the layout
    // that goes stale the next time one of those parts is retuned. The unit is half an em:
    // that is the column the cutting and the filler have always counted in, a full-width
    // glyph being two of them and a Latin one.
    const nameCols = ref(SUB_NAME_COLS);

    watch(subsDrop, (_, __, onCleanup) => {
        const measure = () => {
            const cell = document.querySelector<HTMLElement>('.subdrop-table td.name');
            const tail = cell?.querySelector<HTMLElement>('.row-tail');
            const sep = cell?.querySelector<HTMLElement>('.sub-sep');
            if (!cell || !tail || !sep) return;
            const cs = getComputedStyle(cell);
            const inner = cell.clientWidth
                - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
            // gap reports the keyword "normal" rather than a number wherever the row is
            // not actually a flex row, and that has to fall back rather than turn the
            // arithmetic into a NaN and the column with it
            const available = inner - tail.offsetWidth - sep.offsetWidth
                - (parseFloat(cs.columnGap) || 0);
            // A column is half an em for the name and six tenths for the filler that
            // pads it — a CJK glyph is one em and counts as two columns, a dash is the
            // monospaced advance and counts as one — so the count is taken against the
            // wider of the two. Measuring against half an em instead overstated the
            // room by a fifth, which is how a label that was meant to fill the row
            // came to be cut off by its own ellipsis.
            //
            // Two of the columns are in brackets that the count does not include (see
            // fitName), so they come off here: the label is the name's column *plus* its
            // brackets, and it is the whole label that has to fit.
            const cols = Math.floor(available / (parseFloat(cs.fontSize) * 0.6)) - 2;
            if (!Number.isFinite(cols)) return;
            nameCols.value = Math.max(SUB_NAME_COLS_MIN, Math.min(SUB_NAME_COLS_MAX, cols));
        };
        // There is nothing to measure until the drawer is in the window, so this runs
        // again when it opens; from then on the drawer's own box is what moves. The first
        // run is immediate — the labels have to be right before they are drawn — and the
        // ones a resize brings are settled first: the count is what the row can hold, and
        // a count taken while the width is still moving is a re-render spent on a number
        // the next frame replaces.
        measure();
        let settle = 0;
        const onBoxMove = () => {
            clearTimeout(settle);
            settle = window.setTimeout(measure, RESIZE_SETTLE_MS);
        };
        const ro = new ResizeObserver(onBoxMove);
        if (panelRef.value) ro.observe(panelRef.value);
        onCleanup(() => {
            ro.disconnect();
            clearTimeout(settle);
        });
    }, {flush: 'post', immediate: true});

    return {panelWUser, panelW, panelResizing, panelRef, nameCols,
        onPanelGripDown, onPanelGripMove, onPanelGripUp, onPanelGripKey, resetPanelW};
}
