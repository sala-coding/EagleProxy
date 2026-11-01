// The subscriptions box: dropped from the top edge by the rail's grid button.
// It is a panel rather than a dialog — nothing behind it is dimmed, and
// nothing behind it is blocked either, so the table beside it stays usable
// while it is out. It closes from its own rail button, from Escape, and from
// picking a card.
// Extracted from App.tsx (the subsDrop block).
import {onBeforeUnmount, onMounted, ref, watch, type Ref} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {main} from '../../wailsjs/go/models';
import {type PanelExit} from '../lib/subdraw';

export function useSubsDrawer(deps: {
    connOpen: Ref<boolean>;
    connExit: Ref<PanelExit | null>;
    groups: Ref<main.GroupInfo[]>;
}) {
    const subsDrop = ref(false);
    // How the drawer is leaving, if it is. The unmount has to wait for the exit
    // animation or the element disappears on the frame the click lands and CSS never
    // gets to render the last one — the same reason Modal carries a closing state.
    //
    // Only being dismissed needs a state at all. Being displaced by the panel that
    // shares this seat does not: the reader asked for the new panel, not for the old
    // one to perform an exit, so it leaves in the frame the replacement arrives (see
    // openSubs). hardCloseSubs is the third way out, where the next thing on screen
    // is a centred dialog and not a panel at all.
    const subsClosing = ref<PanelExit | null>(null);

    // The duration is App.css's: doorFlipOut's .44s. Shortening it here without
    // shortening it there would cut the swing off mid-turn.
    watch(subsClosing, (closing, _, onCleanup) => {
        if (!closing) return;
        const timer = window.setTimeout(() => {
            subsClosing.value = null;
            subsDrop.value = false;
        }, 440);
        onCleanup(() => window.clearTimeout(timer));
    });

    // In a plain function rather than behind a ref now: the Vue listener below reads
    // it at call time, so it is always the current one — the same guarantee the
    // useCallback gave the React listener.
    const openSubs = () => {
        // Reopening mid-exit cancels it, and the entrance swings the panel back out
        // from wherever the turn had got to.
        subsClosing.value = null;
        subsDrop.value = true;
        // The connections window is in this seat too, so it goes — at once, with no exit
        // of its own. It used to be pushed out of the way with a drop, which put a second
        // animation on screen for a panel the reader did not ask to close: the drawer was
        // still turning in while the window sank past it, and which of the two motions had
        // been requested was not something the eye could tell. It leaves in the same frame
        // the drawer arrives in, which is also all it takes — the two share a width, so the
        // seat the table holds does not change hands at all.
        if (deps.connOpen.value) {
            deps.connExit.value = null;
            deps.connOpen.value = false;
        }
    };
    const closeSubs = () => {
        // A second request while the exit is running means the reader is impatient:
        // give it to him at once rather than making him watch the turn.
        if (subsDrop.value && !subsClosing.value) subsClosing.value = 'flip';
        else subsDrop.value = false;
    };
    // The group editor is a centred dialog, not a panel, and it does not arrive in
    // this seat: the drawer simply leaves, at once. Animating a 440ms exit under a
    // modal that is already open would only delay the dialog the reader asked for.
    const hardCloseSubs = () => {
        subsClosing.value = null;
        subsDrop.value = false;
    };

    // The rail's subscriptions entry: resident while there is a subscription to
    // open, null in a rail with nothing behind the icon (see the conditional).
    const subsBtnRef = ref<HTMLButtonElement | null>(null);

    // The first Tab press of a session lands on "all subscriptions" and drops the
    // box in the same keystroke. With nothing focused yet — activeElement is the
    // body — that press had nothing to spend: it was only ever going to put the
    // focus on the first focusable thing in the window, which is this button. From
    // then on the button's own handler does the toggling (see the rail below), so
    // the two presses together read as one switch.
    //
    // The gesture waits with the button: with no icon in the rail the ref is null
    // and the press falls through to the traversal it would always take.
    //
    // Everywhere else Tab is left exactly as it was: a reader whose focus is in a
    // table row, on a rail switch, or inside a dialog keeps the traversal they
    // expect, and only the empty-focus case is answered here.
    let tabHandler: ((e: KeyboardEvent) => void) | null = null;
    onMounted(() => {
        tabHandler = (e: KeyboardEvent) => {
            if (e.key !== 'Tab' || e.shiftKey) return;
            if (document.activeElement !== null && document.activeElement !== document.body) return;
            if (!subsBtnRef.value) return;
            e.preventDefault();
            subsBtnRef.value?.focus();
            openSubs();
        };
        window.addEventListener('keydown', tabHandler);
    });
    onBeforeUnmount(() => {
        if (tabHandler) window.removeEventListener('keydown', tabHandler);
    });

    // The subscriptions box answers to Escape. Outside clicks used to be a
    // catcher's job; there is no catcher now, so this and the rail button are
    // what put it away without touching the table.
    watch(subsDrop, (drop, _, onCleanup) => {
        if (!drop) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') closeSubs();
        };
        window.addEventListener('keydown', onKey);
        onCleanup(() => window.removeEventListener('keydown', onKey));
    });

    return {subsDrop, subsClosing, openSubs, closeSubs, hardCloseSubs, subsBtnRef};
}
