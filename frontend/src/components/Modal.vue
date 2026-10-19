<script lang="ts">
// paneAnchor works out the coordinates a docked dialog should open at: the
// subscriptions list's own right edge for x, the clicked row's top edge for y.
//
// The x is that edge less its 1px border, so the dialog's own left border lands
// exactly on top of the panel's instead of stacking a second line beside it — the
// two panes then read as one edge rather than as two cards that happen to be
// touching.
//
// It is read from the panel's offset geometry, not from its bounding rect: the
// drawer arrives on a translate that lasts most of a second, and a rect measures
// where it currently is — a press early in that motion would dock the form beside
// a panel still off to the left. Offset geometry is the seat the layout gave it,
// which is where it ends up.
//
// A caller with nothing to measure from — the caption's "add subscription" button,
// which closes the drawer on its way here — is answered with the panel that is on
// its way out, still in the DOM for this tick, or with undefined when there is no
// list at all. undefined means "open centred", which is what the caller does with
// it rather than opening a pane at 0,0.
export function paneAnchor(from?: HTMLElement | null): {left: number; top: number} | undefined {
    const panel = (from?.closest('.subdrop') ?? document.querySelector('.subdrop')) as HTMLElement | null;
    if (!panel || !panel.offsetWidth) return undefined;
    const left = Math.round(panel.offsetLeft + panel.offsetWidth) - 1;
    // Nowhere for a card of this size beside the rail: the centred modal is the
    // honest answer, and it is what undefined buys.
    if (window.innerWidth - left < 320) return undefined;
    const row = from?.closest('tr');
    const top = Math.round(row ? row.getBoundingClientRect().top : panel.offsetTop);
    return {left, top};
}
</script>

<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref, watch, watchEffect, type CSSProperties} from 'vue';
import {CloseFilled} from '../lib/icons';
import {type PanelExit} from '../lib/subdraw';

const props = withDefaults(defineProps<{
    title: string;
    wide?: boolean;
    narrow?: boolean;
    // extraClass lets a dialog claim its own width without inventing another
    // size prop: .wide is shared with the node editor, which must stay as it is.
    extraClass?: string;
    // bodyScroll = false hands scrolling to the dialog's own list component.
    // Required whenever a sticky element lives in the body: with the body
    // scrollable, a short list lets the scroll escape upwards and the sticky
    // header loses the container it was pinned to.
    bodyScroll?: boolean;
    // flip swaps the dialog in and out around the Y axis, like a door swinging.
    // Opt-in: it is motion not every dialog should have.
    flip?: boolean;
    // exit asks the dialog to close, from outside it. A trigger that lives in the
    // window rather than in the dialog — a rail button that toggles the panel it
    // opened, or the other panel taking the seat — has no way to reach the state its
    // own close button sets, and unmounting it outright is the one thing this
    // component exists to prevent: the exit animation would be skipped.
    // The value is which way it leaves: 'flip' turns it back out of its hinge. Only one
    // kind is left — a panel that is displaced rather than dismissed is not given an exit
    // at all (see PanelExit) — but the prop is still how an outside trigger asks.
    exit?: PanelExit | null;
    // anchor turns the dialog into the list's other half: a pane whose left border
    // sits on the given x and whose top border lines up with the given y, so the
    // form reads as belonging to the row it was opened from rather than as a
    // centred card that happened to be launched from one. Omitted, the dialog is
    // an ordinary centred modal.
    anchor?: {left: number; top: number};
    // sheet drops the dialog from the window's top edge instead of centring it: the
    // panel takes the window's whole height and covers what it lands on (see
    // .settings-sheet in App.css). A prop rather than a class handed in through
    // extraClass, because which way a dialog leaves is a decision the component has
    // to make — it is the one that runs the exit and times the unmount.
    sheet?: boolean;
    // seat places the panel in a box measured elsewhere — the node table's, which the
    // settings panel takes the place of while that turns away. Written as four custom
    // properties rather than as inline geometry so the stylesheet keeps the last word
    // on what a panel looks like, and so a caller with nothing to measure falls back to
    // the numbers already written there.
    seat?: {left: number; top: number; width: number; height: number} | null;
}>(), {bodyScroll: true});

// onClose -> 'close'; onClosing -> 'closing' (fires the moment the exit begins,
// before the animation has finished). `head` and `bar`, which were ReactNode
// props, are the 'head' and 'bar' slots; `children` is the default slot.
const emit = defineEmits<{
    close: [];
    closing: [kind: PanelExit];
}>();

// Closing cannot be driven by the parent's boolean alone: the instant it
// turns false this component unmounts and CSS never renders the last frame.
// Mark it closing, let the exit animation run, then call the real handler.
// The kind is kept rather than a bare flag because the two exits are different
// motions with different lengths: a door swinging shut is not a pane sliding
// back into the wall, and they cannot share one timer.
const closing = ref<PanelExit | 'pane' | 'sheet' | null>(null);

const requestClose = () => {
    // a second request while the exit is running means the user is impatient:
    // give it to him immediately rather than making him wait out the motion
    if (closing.value) {
        emit('close');
        return;
    }
    // The kind is worked out once and then used twice: for the state that drives the
    // animation, and for the report to the parent, which has to be told the exit has
    // begun rather than that it has finished. Naming it also lets the type narrow,
    // so the two can never be derived differently.
    const kind: PanelExit | 'pane' | 'sheet' | null = props.flip ? 'flip'
        : props.anchor ? 'pane'
            : props.sheet ? 'sheet'
                : null;
    if (!kind) {
        emit('close');
        return;
    }
    closing.value = kind;
    // 'pane' is the anchored form's own exit, and 'sheet' the settings panel's:
    // neither has a parent waiting to hear about it. The report is about the door
    // swing and the drop, the two exits something else has to act on.
    if (kind !== 'pane' && kind !== 'sheet') emit('closing', kind);
};

// An outside trigger runs the same path, so the panel leaves by the motion it
// arrived by. Keyed on the prop alone: it is a rising edge, and re-running it on
// every render would restart nothing but would keep the effect alive for nothing.
watch([() => props.exit, closing], () => {
    // `closing` is in the condition as well as the deps: the parent's report of
    // this very exit comes back as the `exit` prop, and re-running the request would
    // read as the reader being impatient, which cuts the animation off at once.
    if (props.exit && !closing.value) requestClose();
}, {immediate: true});

// The exit timer must not depend on onClose. Every caller passes an inline
// arrow, so it is a new function on every render of the app — and the app
// re-renders whenever a log line arrives or a poll comes back. With onClose in
// the dependency list the timer was cleared and re-armed on each of those,
// which means that while lines were streaming in faster than the animation
// lasts, the dialog finished its slide and then never unmounted: an invisible
// mask stayed over the whole window and the app looked frozen. Under Vue the
// emit is resolved at call time, so `closing` is the only thing that can start
// or stop the timer.
watch(closing, (c, _, onCleanup) => {
    if (!c) return;
    // must match the exit duration in App.css (.flip-out .44s, .pane-out .32s,
    // and the sheet's own .sheet-out, which is --door-dur like the door swing it
    // now shares): unmounting early would cut the motion off before the card is
    // back where it came from
    const timer = window.setTimeout(() => emit('close'),
        c === 'pane' ? 320 : 440);
    onCleanup(() => window.clearTimeout(timer));
});

// Escape is the sheet's dismissal. The panel is docked in the drawer's seat
// rather than laid over the window, so the click-outside its mask gives is not
// the whole story — most of what the mask covers is the table, and dismissing a
// stack of settings because the pointer landed on the list is a mis-click waiting
// to happen. This is the route beside the head's × and the bar's cancel. The
// handler reads the refs live, so it needs no re-binding between renders.
watch(() => props.sheet, (sheet, _, onCleanup) => {
    if (!sheet) return;
    const onKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') requestClose();
    };
    window.addEventListener('keydown', onKey);
    onCleanup(() => window.removeEventListener('keydown', onKey));
}, {immediate: true});

// Focus, for the whole of the dialog's life: it takes it when it opens, keeps
// Tab inside itself while it is up, and gives it back to whatever had it when it
// closes. Without this the keyboard walked straight out of the card and onto the
// table behind it — a reader tabbing through a form could reach a row they could
// not see, under the mask, and press Enter on it — and closing a dialog dropped
// the focus onto the body, so the next Tab started again from the top of the
// window.
const maskRef = ref<HTMLDivElement | null>(null);
let opener: HTMLElement | null = null;
onMounted(() => {
    opener = document.activeElement as HTMLElement | null;
    const el = maskRef.value;
    // The first thing inside that can take focus, or the card itself when there
    // is nothing: a dialog with no controls (a plain notice) still has to hold
    // the focus, or the next Tab walks out of it.
    const first = el?.querySelector<HTMLElement>(FOCUSABLE) ?? null;
    (first ?? el)?.focus();
});
onBeforeUnmount(() => {
    // Only when it is still in the document. The opener can be gone with the
    // panel that held it — a row menu that opened this card — and focusing a
    // detached node throws the focus at the body instead.
    if (opener?.isConnected) opener.focus();
});

// focusable is what the keyboard can reach inside a dialog: the tab order the
// browser would walk by itself, spelled out so the trap above can walk it too.
const FOCUSABLE = 'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])';

// Escape, and the tab loop, handled here rather than on the window: the settings
// panel behind this dialog listens on the window for its own Escape, and a press
// that reached it would dismiss two layers at once. A synthetic event's
// stopPropagation stops the native one before it leaves the app's root, so this
// is the whole of that fix.
const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
        e.stopPropagation();
        requestClose();
        return;
    }
    if (e.key !== 'Tab') return;
    const el = maskRef.value;
    if (!el) return;
    // Read live rather than kept in a list: a dialog's contents change while it
    // is open (a row added, a section expanded), and a stale list is a door the
    // keyboard can leave through.
    const items = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE))
        .filter(n => !(n as HTMLButtonElement).disabled && n.offsetParent !== null);
    if (items.length === 0) {
        e.preventDefault();
        return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === el)) {
        e.preventDefault();
        last.focus();
    } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
    }
};

const paneRef = ref<HTMLDivElement | null>(null);
// The measured box, as custom properties the stylesheet reads. An inline style
// rather than a layout effect writing them one by one: this runs on every render
// that carries a new measurement, which is exactly when the numbers change, and it
// cannot leave a stale one behind the way a mount-time effect can.
const seatStyle = computed<CSSProperties | undefined>(() => props.seat ? {
    '--seat-x': `${props.seat.left}px`,
    '--seat-y': `${props.seat.top}px`,
    '--seat-w': `${props.seat.width}px`,
    '--seat-h': `${props.seat.height}px`,
} as CSSProperties : undefined);
// The anchor is the row's own box, which says nothing about how tall the form
// will turn out to be. Writing the settled position into custom properties in
// a layout effect keeps the pane from ever being painted hanging off the
// bottom of the window: the first paint already uses the row's coordinates,
// and the correction lands before the browser shows any of it.
watchEffect(() => {
    const anchor = props.anchor;
    const el = paneRef.value;
    if (!anchor || !el) return;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    // a row low in a short window has no room below it, so the pane slides up
    // rather than being cut off; the alignment is a courtesy to the eye, not a
    // promise worth breaking the layout over
    const top = Math.max(8, Math.min(anchor.top, window.innerHeight - h - 8));
    // the pane runs to the window's right edge by construction, so the only
    // horizontal clamp it can need is on a window narrower than the anchor
    const left = Math.max(0, Math.min(anchor.left, window.innerWidth - w));
    el.style.setProperty('--fly-x', `${left}px`);
    el.style.setProperty('--fly-y', `${top}px`);
    // How far the pane has to travel on its way in: it starts with its left
    // edge on the window's right border, so the whole card is off-screen and
    // the slide is the length of the space it has to cross rather than a fixed
    // nudge. Measured here because it depends on the settled left, and reading
    // it after the fact is the same number the animation is about to use.
    el.style.setProperty('--fly-travel', `${Math.max(0, window.innerWidth - left)}px`);
}, {flush: 'post'});

// The size props are deliberately left on for an anchored pane: the pane is the
// same card, only relocated, so it must keep the width the dialog would have
// had in the middle of the screen.
const modalClass = computed(() =>
    `modal ${props.wide ? 'wide' : ''} ${props.narrow ? 'narrow' : ''} ${props.extraClass || ''} ${props.flip ? 'flip' : ''} ${closing.value === 'flip' ? 'flip-out' : ''} ${props.anchor ? 'anchored' : ''} ${closing.value === 'pane' ? 'pane-out' : ''} ${closing.value === 'sheet' ? 'sheet-out' : ''}`);
</script>

<template>
    <div :class="`modal-mask${anchor ? ' anchored' : ''}`" ref="maskRef"
         tabindex="-1" @keydown="onKeyDown" @click="requestClose">
        <div ref="paneRef"
             :style="seatStyle"
             role="dialog"
             aria-modal="true"
             :aria-label="title"
             :class="modalClass"
             @click.stop>
            <div class="modal-head">
                <h2>{{ title }}</h2>
                <div class="modal-head-actions">
                    <slot name="head"/>
                    <button class="btn tiny ghost" @click="requestClose">
                        <CloseFilled class="ic"/>
                    </button>
                </div>
            </div>
            <div :class="`modal-body ${bodyScroll ? '' : 'no-scroll'}`">
                <slot/>
            </div>
            <div v-if="$slots.bar" class="modal-bar"><slot name="bar"/></div>
        </div>
    </div>
</template>
