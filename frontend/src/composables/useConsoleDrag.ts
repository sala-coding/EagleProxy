// The log panel's height, extracted from App.tsx: the drag on the panel's top
// edge, the keyboard adjustment, and the measured height the handle reports.
import {ref, watch, type Ref} from 'vue';
import {clampConsoleHeight} from '../lib/layout';

export function useConsoleDrag(showLogs: Ref<boolean>) {
    // The log panel's height in px, or null while the CSS default (a fraction of
    // the window) still stands. A drag sets it, and it is remembered so the panel
    // comes back the size it was left at.
    const savedH = Number(window.localStorage.getItem('console-height'));
    const consoleH = ref<number | null>(Number.isFinite(savedH) && savedH > 0 ? clampConsoleHeight(savedH, window.innerHeight) : null);
    const consoleDragging = ref(false);

    // The panel element and the drag in progress: where the pointer went down,
    // and how tall the panel was then, so every move is measured from the start
    // rather than accumulated from the last one.
    const consoleRef = ref<HTMLElement | null>(null);
    let consoleDrag: { y: number; h: number } | null = null;
    // The panel's height as the browser has actually laid it out, which is what the
    // handle reports. Read in a layout effect so the reported value is the height
    // the panel has rather than the one it is about to have — and so it is right
    // even when the height came from the stylesheet rather than from a drag.
    const consoleNow = ref(0);
    watch([showLogs, consoleH], () => {
        if (!showLogs.value) return;
        consoleNow.value = Math.round(consoleRef.value?.getBoundingClientRect().height ?? 0);
    }, {flush: 'post', immediate: true});

    // Dragging the panel's top edge. The pointer is captured by the handle, so
    // every move and the release arrive there even once the pointer has left it —
    // that is what makes the panel feel held rather than chased.
    const onConsoleGripDown = (e: PointerEvent) => {
        const el = consoleRef.value;
        if (!el) return;
        e.preventDefault();          // no text selection along the dragged edge
        // Capture keeps the moves coming to the handle after the pointer leaves
        // it. The browser can refuse a capture it no longer recognises, and the
        // drag is still usable — it would just end at the handle's edge.
        try {
            (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
        } catch { /* the drag runs without it */ }
        consoleDrag = {y: e.clientY, h: el.getBoundingClientRect().height};
        consoleDragging.value = true;
    };

    const onConsoleGripMove = (e: PointerEvent) => {
        const d = consoleDrag;
        if (!d) return;
        // Up makes the panel taller: the edge follows the pointer, and the panel
        // grows away from the bottom, which does not move.
        consoleH.value = clampConsoleHeight(d.h + (d.y - e.clientY), window.innerHeight);
    };

    const onConsoleGripUp = (e: PointerEvent) => {
        if (!consoleDrag) return;
        consoleDrag = null;
        consoleDragging.value = false;
        try {
            const tgt = e.currentTarget as HTMLElement;
            if (tgt.hasPointerCapture?.(e.pointerId)) {
                tgt.releasePointerCapture(e.pointerId);
            }
        } catch { /* nothing left to release */ }
        // What the drag asked for, not what the browser laid out. The stylesheet clamps the
        // panel to a share of the window (see .console), so the measured height is the one
        // this window happens to allow — storing that quietly rewrote the reader's choice
        // down to it for good: a panel dragged to the top in a small window came back
        // short in every roomier one after, and could not be brought back by dragging
        // (the CSS clamp simply cut it again). The state value is already clamped to the
        // same range the stylesheet uses (see clampConsoleHeight).
        //
        // And only when a drag actually set one: a pointer that landed on the grip and
        // came up again is not a choice of height, and writing the automatic share out as
        // a fixed one is how the panel stopped following the window for good.
        if (consoleH.value !== null && consoleH.value > 0) {
            window.localStorage.setItem('console-height', String(consoleH.value));
        }
    };

    // The same adjustment from the keyboard, because a separator you can only
    // drag is a separator some people cannot use. Shift for the larger step, and
    // Home to hand the height back to the window's share of it.
    const onConsoleGripKey = (e: KeyboardEvent) => {
        const step = e.shiftKey ? 48 : 16;
        const cur = consoleRef.value?.getBoundingClientRect().height ?? 0;
        let next: number | null = null;
        if (e.key === 'ArrowUp') next = cur + step;
        else if (e.key === 'ArrowDown') next = cur - step;
        else if (e.key === 'Home') next = null;
        else return;
        e.preventDefault();
        const h = next === null ? null : clampConsoleHeight(next, window.innerHeight);
        consoleH.value = h;
        window.localStorage.setItem('console-height', h === null ? '' : String(h));
    };

    // The double-click on the grip: hand the height back to the stylesheet's share.
    const resetConsoleH = () => {
        consoleH.value = null;
        window.localStorage.setItem('console-height', '');
    };

    return {consoleH, consoleDragging, consoleNow, consoleRef,
        onConsoleGripDown, onConsoleGripMove, onConsoleGripUp, onConsoleGripKey, resetConsoleH};
}
