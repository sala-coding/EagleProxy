<script lang="ts">
export type RotorItem =
    | {kind: 'grp'; key: string; label: string}
    | {kind: 'row'; key: string; name: string; addr: string; value: string; ms: number};
</script>

<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue';
import {t} from "../i18n";
import {latencyClass, latencyText, RESIZE_SETTLE_MS} from "../lib/metrics";
import {type DnsPreset, type Side} from "../lib/dns";

// One resolver list, as a box that turns.
//
// The list is cut into pages and each page is a face of the box; the box turns about its
// vertical axis to bring the next one round. It replaces a plain list because these lists
// are longer than the card is tall — a window with a scrollbar in it shows fourteen of
// eighty-three and says nothing about the other sixty-nine — while a box has sides, so the
// rest of the list is a gesture away rather than a scrollbar away.
//
// The pages are cut to the height the card gives it rather than to a fixed count of rows:
// the card is as tall as the window and the type scale is a setting, so a page of twenty
// rows would be half empty in one window and cut a row in half in another. The row and
// heading heights are measured off the rendered list and pages are filled until the next
// entry would not fit.
//
// Every row is a button, so the whole row is the target and the keyboard reaches it by
// tabbing — the rows off the current page are out of the tab order, being out of sight. The
// address column is a fixed width, which is what keeps a column of buttons lined up the way
// a table's cells would.
//
// EitherSwitch lives in its own SFC (./EitherSwitch.vue). onAssign/onPick are emits;
// `value` is read-only here (the table never writes it), so it stays a plain prop.
const props = defineProps<{
    value: string;
    presets: DnsPreset[];
    latency: Record<string, number>;
    // The interface size, passed in only so the measurement below can be re-taken when
    // it changes: the row height is derived from the type scale, and the scale can be
    // moved while this table is on screen (the looks card is a slide of the same
    // dialog, and the dialog does not unmount the table to show it).
    scale: number;
    // The fields a row of *this* list may be put into, which is what its menu offers. Not the
    // same for both lists: the foreign one's entries are answers for either side of the
    // fallback, while a domestic resolver is only ever the direct answer — the foreign field
    // is reached through the tunnel, and a resolver inside the country has nothing to do
    // there. `offers` is that rule, stated where the menu is built.
    offers: Side[];
}>();

const emit = defineEmits<{
    assign: [side: Side, value: string];
    pick: [v: string];
}>();

// The whole list, fastest first once a round has measured it — across the groups, not
// within them.
//
// What the reader is looking for is the quickest resolver there is, and a group is a
// property of a row rather than a way to order the table. Sorted group by group, the
// fastest entry in the list could sit on the fourth page because it happens to be filed
// under 默认 rather than 加密 — which is exactly what the sections are not for: they say
// what a resolver does to the answer, and the number beside it says how fast it does it.
//
// No headings: the source lists file their presets by transport and by policy only —
// a property the name column already carries in words ("DoH", "Family") — and the
// order here is an order by latency, so their runs are broken up anyway. A heading
// that announced itself wherever the run happened to break read as noise rather
// than as filing, and the tables now stand as one undivided list each.
//
// What did not answer goes after what did, and what was never asked goes after that: a
// resolver with no number has no place in a queue by number. Sorting is stable and the
// source list is filed by group, so the unmeasured tail keeps the catalogue's own order.
const ordered = computed<DnsPreset[]>(() => {
    const rank = (p: DnsPreset) => {
        const ms = props.latency[p.value];
        if (ms === undefined) return Number.MAX_SAFE_INTEGER;
        if (ms < 0) return Number.MAX_SAFE_INTEGER - 1;
        return ms;
    };
    // Sorted in a copy: the list itself is the catalogue, shared with everything else
    // that reads it.
    return [...props.presets].sort((a, b) => rank(a) - rank(b));
});

// Rows in page order — one kind only now, so a page is cut wherever the room runs out.
const items = computed<RotorItem[]>(() => ordered.value.map(p => ({
    kind: 'row', key: `r${p.value}`, name: p.name, value: p.value,
    // Left empty where the name is the address, which is how the encrypted ones
    // are filed; the cell stays so the column holds.
    addr: p.name === p.value ? '' : p.value,
    // 0 for a resolver that was never asked, -1 for one that did not answer: the
    // two states the node table already has a word and a colour for.
    ms: props.latency[p.value] ?? 0,
})));

const boxRef = ref<HTMLDivElement | null>(null);
const box = ref({w: 0, h: 0});
const metrics = ref({row: 0, head: 0});
// The page the reader asked for. What is drawn is this clamped to the pages that
// exist (see page, below) rather than a page corrected in an effect: an effect was a
// second render every time the count changed, and the count is cut to the card's
// height, so a window resize paid for one.
const pageWanted = ref(0);
const dragging = ref(false);
const pull = ref(0);
// Whether the last gesture turned the box rather than pressing a row: a drag that ends
// over a row still delivers a click to it, and picking a resolver nobody asked for is
// the one thing this must not do.
let drag: {x: number; moved: boolean} | null = null;
let turned = false;

// The box is the size of the space it is given — the card's height, less the caption and
// the pager — and that size is what the pages are cut to.
//
// Settled rather than followed: a window resize reports itself once per frame, and a
// setState for each of those re-renders the whole application to decide a page size
// nobody can read while the window is still moving. The first measurement is taken
// straight away — the box has to have a size before it is drawn — and the ones a
// resize brings wait for the move to stop.
let ro: ResizeObserver | null = null;
let settle = 0;
onMounted(() => {
    const el = boxRef.value;
    if (!el) return;
    const read = () => { box.value = {w: el.clientWidth, h: el.clientHeight}; };
    read();
    const measure = () => {
        clearTimeout(settle);
        settle = window.setTimeout(read, RESIZE_SETTLE_MS);
    };
    ro = new ResizeObserver(measure);
    ro.observe(el);
});
onBeforeUnmount(() => {
    ro?.disconnect();
    clearTimeout(settle);
});

// The row and heading heights come off the rendered list rather than from constants: the
// interface size scales the whole type scale, and a hard-coded 22px would cut every page
// wrong at any other setting.
//
// Measured again whenever the scale moves, rather than once for the size the card
// happened to open at. The rows grow with the type, the faces are cut from the height
// measured before they did, and the box is grown to that stale plan: the last row or
// two of every page ended up below the fold of a face that cannot scroll, and the
// pager only turns whole pages — so nothing could bring them back until the dialog was
// closed and reopened.
const measureRows = () => {
    const el = boxRef.value;
    if (!el) return;
    const rows = el.querySelectorAll<HTMLElement>('.dns-row');
    // The second row where there is one: every entry but the first wears the rule above
    // it, so the first row is a pixel shorter than the rest and measuring it would
    // leave the page a pixel short per row — enough, over twenty of them, to cut one.
    const row = rows[1] ?? rows[0];
    if (!row) return;
    const grp = el.querySelector<HTMLElement>('.dns-grp');
    // Written only when the numbers move: this runs after every render that the
    // scale change causes, and a state write for a pair that has not changed is the
    // re-render loop this measurement exists to avoid.
    const next = {row: row.offsetHeight, head: grp?.offsetHeight ?? 0};
    if (metrics.value.row !== next.row || metrics.value.head !== next.head) metrics.value = next;
};
watch([() => props.scale, boxRef], () => measureRows(), {flush: 'post'});

// The faces are cut by plan, not by probing the card. Asking the card for height put
// the arithmetic at the mercy of a min-height floor: a floor that returned a hundred
// and one pixels of box held three rows and a third of dead frame at every face's
// foot — which is what the reader read as empty entries at the table's bottom. The
// plan is owned by the list now: near seven rows to a face, the faces share the list
// evenly (the remainder rides the front faces, one row each), and the box is grown to
// the fullest face and no taller — see boxH below. The rows agree on one height
// for the arithmetic to hold (a fixed line-height on the cells, see .dns-row), so a
// cut row cannot hide behind a taller cell.
const pagesN = computed(() => items.value.length > 0 ? Math.max(1, Math.round(items.value.length / 7)) : 1);
const baseShare = computed(() => Math.floor(items.value.length / pagesN.value));
const extraShare = computed(() => items.value.length - baseShare.value * pagesN.value);
const per = computed(() => baseShare.value + (extraShare.value > 0 ? 1 : 0));
const pages = computed<RotorItem[][]>(() => {
    if (!metrics.value.row || items.value.length === 0) return [items.value];
    const out: RotorItem[][] = [];
    let i = 0;
    for (let p = 0; p < pagesN.value; p++) {
        const share = baseShare.value + (p < extraShare.value ? 1 : 0);
        out.push(items.value.slice(i, i + share));
        i += share;
    }
    return out;
});

// The box is grown to the plan rather than to whatever floor the card keeps: the
// fullest face and two pixels of seam, and nothing over it — the buffer row this used
// to add against a rounding error in the row height was a permanent empty band at the
// foot of every page, which read as a blank entry in the table.
const boxH = computed<number | undefined>(() => metrics.value.row > 0 ? per.value * metrics.value.row + 2 : undefined);
const boxStyle = computed(() => boxH.value !== undefined ? {height: `${boxH.value}px`} : undefined);

// A shorter card or a longer list can leave the box standing on a page that is no
// longer there, and the correction is made here, where the page is read, rather than in
// an effect: the effect was a second render of the whole application for a number the
// reader never sees, and it ran on every resize, because the page count is cut to the
// card's height. pageWanted keeps what the reader asked for, so a window that grows
// back returns to the page they were on.
const count = computed(() => Math.max(pages.value.length, 1));
const page = computed(() => Math.min(pageWanted.value, count.value - 1));
// One page is not a box, and the arithmetic has to be told so. A one-sided polygon has
// no apothem, and tan(π/1) is not zero in floating point either — it is about 1.2e-16,
// so the division below returned a radius around 1e17 instead of failing. A face pushed
// out to meet a radius of 1e17, seen from 550px away, is a face projected from nowhere:
// the whole list slid and shrank inside its frame, which is what "the layout broke"
// looked like. Nothing here is a rounding error, and nothing caught it, because the
// number is finite and enormous rather than infinite or NaN.
//
// It was always reachable — the first frame, before the box has been measured, draws a
// single page — and it became reachable in practice the day the foreign resolver list
// was cut down to a size that fits one page on a wide card. With the pair below at nought
// there is no turn and no depth to project, which is exactly what one page is.
// The step the box is *drawn* at: a quarter of a turn, whatever the list cuts to.
// The geometry used to share one number with the pagination, and on a long list that
// number is small — the foreign table's four pages turn ninety degrees a step while
// the domestic table's six turn sixty in a box half again as deep, so the two tables
// standing side by side did not turn the same way at all: at sixty degrees the
// neighbouring face still leans into view at the frame's edge and the reader sees two
// pages at once, while at ninety the neighbours stand edge-on and the card reads as
// one face with the next waiting outside it. The floor is now the foreign table's own
// quarter turn, which is what the two are asked to match on; lists of four pages or
// fewer keep the spacing they had (a two-page list still flips, as it should).
const viewStep = computed(() => count.value > 1 ? Math.max(360 / count.value, 90) : 0);
// The apothem of a regular box at that drawn spacing, so the shared edge still lands
// exactly on the frame (half a face-width from the axis) no matter how steeply the
// sides are spaced.
const radius = computed(() => count.value > 1 && box.value.w > 0
    ? (box.value.w / 2) / Math.tan((viewStep.value / 2) * Math.PI / 180)
    : 0);
// A quarter turn a step only closes a ring when the faces come in fours: five faces
// at ninety degrees put the fifth exactly where the first stands, and it would paint
// its rows over the page on screen. So past four pages the box is drawn as a window
// rather than a ring — the page on screen, one each side for the turn to swing
// through, and two more that can only ever be edge-on or behind — and the drag stops
// at the ends of the list, because past an end there is no face to turn into.
const ringed = computed(() => count.value <= 4);
const shown = (i: number) => ringed.value || Math.abs(i - page.value) <= 2;
const dragLo = computed(() => ringed.value ? -Infinity : -Math.min(90, (count.value - 1 - page.value) * 90));
const dragHi = computed(() => ringed.value ? Infinity : Math.min(90, page.value * 90));
const angle = computed(() => -page.value * viewStep.value
    + Math.max(dragLo.value, Math.min(dragHi.value, (pull.value / Math.max(box.value.w, 1)) * viewStep.value)));
// p arrives as the page that was asked for rather than the one drawn (see page), so it
// is clamped the same way here: a turn steps from the page on screen, never from one
// the card is too short to show.
const turn = (d: number) => {
    pageWanted.value = Math.max(0, Math.min(count.value - 1, Math.min(pageWanted.value, count.value - 1) + d));
};

const onDown = (e: PointerEvent) => {
    if (e.button !== 0 || count.value < 2) return;
    drag = {x: e.clientX, moved: false};
    turned = false;
    dragging.value = true;
    (e.currentTarget as HTMLDivElement).setPointerCapture(e.pointerId);
};
const onMove = (e: PointerEvent) => {
    // Guarded on the ref rather than on `dragging`: both handlers run in the same
    // gesture, and a state set a moment ago has not come back round as a render yet.
    if (!drag) return;
    const dx = e.clientX - drag.x;
    if (Math.abs(dx) > 4) drag.moved = true;
    pull.value = dx;
};
const onUp = (e: PointerEvent) => {
    const gesture = drag;
    if (!gesture) return;
    drag = null;
    dragging.value = false;
    pull.value = 0;
    turned = gesture.moved;
    // Cleared in a task of its own: the click this gesture may still deliver comes
    // straight after this event, and anything later is a fresh intention.
    if (turned) window.setTimeout(() => { turned = false; }, 0);
    // A nudge is not a turn: the box only moves on with a gesture that meant to.
    const dx = e.clientX - gesture.x;
    if (Math.abs(dx) > 40) turn(dx < 0 ? 1 : -1);
};
const pick = (v: string) => {
    if (turned) return;
    emit('pick', v);
};

// The row's menu, at the row and in the frame's own coordinates (see .ctxmenu.dns-ctx for
// why not the window's), and gone as soon as the reader does anything else with the card.
const menu = ref<{x: number; y: number; value: string} | null>(null);
const wrapRef = ref<HTMLDivElement | null>(null);
const menuRef = ref<HTMLDivElement | null>(null);
// The menu's own height, near enough to decide whether it fits below the row it was
// opened on: an item and the panel's padding, counted rather than written down, because
// a list offers one field or two.
const menuH = computed(() => 8 + props.offers.length * 26);

watch(menu, (m, _, onCleanup) => {
    if (!m) return;
    // A press inside the menu is not a press elsewhere: without that check the mousedown
    // would take the menu away before the click on its item could land.
    const onDown = (e: MouseEvent) => {
        if (menuRef.value?.contains(e.target as Node)) return;
        menu.value = null;
    };
    const onKey = (e: KeyboardEvent) => {
        if (e.key !== 'Escape') return;
        // The menu's Escape, and only the menu's. The settings panel behind it
        // listens on the window for its own dismissal, and it registered first —
        // so this one asks for the capture phase, which runs before either, and
        // stops the press there. One Escape closes the menu, the next closes the
        // panel, which is what a reader expects of two layers.
        e.stopPropagation();
        menu.value = null;
    };
    const onScroll = () => { menu.value = null; };
    // `scroll` does not bubble out of the sheet's own scroller, so it is caught on the way
    // down — a menu left behind by a scroll is a menu pointing at the wrong row. All three
    // are named so that this cleanup takes off the ones it put on.
    document.addEventListener('mousedown', onDown);
    document.addEventListener('scroll', onScroll, true);
    window.addEventListener('keydown', onKey, true);
    onCleanup(() => {
        document.removeEventListener('mousedown', onDown);
        document.removeEventListener('scroll', onScroll, true);
        window.removeEventListener('keydown', onKey, true);
    });
});

// The row's own right click. While the menu is up the box is not turned by it: the drag
// handlers only take the left button.
const openMenu = (e: MouseEvent, v: string) => {
    e.preventDefault();
    e.stopPropagation();
    const wrapBox = wrapRef.value?.getBoundingClientRect();
    if (!wrapBox) return;
    menu.value = {x: e.clientX - wrapBox.left, y: e.clientY - wrapBox.top, value: v};
};

// The frame's own right click: the browser's menu is not the one this card
// offers, and the row menus stop their own before this sees them.
const onWrapContext = (e: MouseEvent) => {
    e.preventDefault();
    menu.value = null;
};

// How far this face is round the box, 0 when it is square on to the
// reader. What is being read is unshaded; a side on its way in or
// out is not, and that difference is most of what says the box has
// sides rather than being a panel that slides across.
const shadeOf = (i: number): string => {
    const round = ((i * viewStep.value + angle.value) % 360 + 360) % 360;
    const facing = round > 180 ? 360 - round : round;
    return (Math.min(facing / 90, 1) * 0.5).toFixed(3);
};

const choose = (side: Side) => {
    const m = menu.value;
    if (!m) return;
    emit('assign', side, m.value);
    menu.value = null;
};

const menuStyle = computed(() => {
    if (!menu.value) return undefined;
    // Kept in the frame: past its right edge the menu would leave the
    // card, and past the bottom the sheet's own scrolling would cut it —
    // so it opens upwards instead. Both numbers come from the menu's own
    // size to within a few pixels: an item and the panel's padding.
    const w = wrapRef.value?.clientWidth ?? 0;
    const h = wrapRef.value?.clientHeight ?? 0;
    return {
        left: `${Math.max(0, Math.min(menu.value.x, w - 158))}px`,
        top: menu.value.y + menuH.value > h
            ? `${Math.max(0, menu.value.y - menuH.value + 4)}px`
            : `${menu.value.y}px`,
    };
});
</script>

<template>
    <!-- Named for the reader who cannot see which field sits above it. -->
    <div class="dns-table-wrap" role="group" :aria-label="t('routing.presets')"
         ref="wrapRef"
         @contextmenu="onWrapContext">
        <div class="dns-table-head">
            <span>{{ t('col.name') }}</span>
            <!-- The column the table is ordered by, and the reason the order is what it
                 is: a list sorted by a number nobody can see is a list in a strange
                 order. -->
            <span>{{ t('col.latency') }}</span>
            <span>{{ t('col.address') }}</span>
        </div>
        <div ref="boxRef"
             :class="`dns-rotor${dragging ? ' dragging' : ''}${count < 2 ? ' static' : ''}`"
             :style="boxStyle"
             @pointerdown="onDown" @pointermove="onMove"
             @pointerup="onUp" @pointercancel="onUp">
            <div class="dns-rotor-track"
                 :style="{
                     transform: `translateZ(${-radius}px) rotateY(${angle}deg)`,
                     transition: dragging ? 'none' : undefined,
                 }">
                <template v-for="(rows, i) in pages" :key="i">
                    <!-- Past four pages the box is a window, not a ring (see shown):
                         the faces at ±4 steps stand exactly on other faces, and one of
                         the pair would cover the page being read. -->
                    <div class="dns-face" v-if="shown(i)" :aria-hidden="i !== page"
                         :style="{
                             transform: `rotateY(${i * viewStep}deg) translateZ(${radius}px)`,
                             '--dns-shade': shadeOf(i),
                         }">
                        <template v-for="it in rows" :key="it.key">
                            <!-- What the resolver does to the answer: the choice
                                 behind the choice. -->
                            <div class="dns-grp" v-if="it.kind === 'grp'">{{ it.label }}</div>
                            <button type="button" v-else
                                    :tabindex="i === page ? 0 : -1"
                                    :class="`dns-row${value === it.value ? ' on' : ''}`"
                                    :aria-pressed="value === it.value"
                                    @contextmenu="openMenu($event, it.value)"
                                    @click="pick(it.value)">
                                <!-- Either column can run out of room — a card is as
                                     narrow as the window — and the whole of both is
                                     worth being able to read, each under its own
                                     tooltip: the name because it was only just
                                     shortened, the address because it is what
                                     picking the row writes. -->
                                <span class="dns-row-name" :title="it.name">{{ it.name }}</span>
                                <!-- The number this table is ordered by, in the same
                                     words and colours the node table's latency column
                                     uses (see latencyText / latencyClass) — a 30ms
                                     here is a 30ms there. -->
                                <span :class="`dns-row-ms ${latencyClass(it.ms)}`">
                                    {{ latencyText(it.ms) }}
                                </span>
                                <span class="dns-row-addr" :title="it.addr">{{ it.addr }}</span>
                            </button>
                        </template>
                    </div>
                </template>
            </div>
        </div>
        <!-- The sides are for a reader who knows the box turns; the pager is for the one
             who does not, and it is also the way to turn it without a pointer. -->
        <div class="dns-pager" v-if="pages.length > 1">
            <button type="button" class="dns-page-btn" :disabled="page === 0"
                    :aria-label="t('btn.prev')" @click="turn(-1)">
                <i class="nav-caret flip"/>
            </button>
            <span class="dns-page-count">{{ page + 1 }} / {{ pages.length }}</span>
            <button type="button" class="dns-page-btn"
                    :disabled="page === pages.length - 1"
                    :aria-label="t('btn.next')" @click="turn(1)">
                <i class="nav-caret"/>
            </button>
        </div>
        <!-- The row's menu, in the frame's coordinates rather than the window's. One item
             per field this list may be put into (see offers): the click writes the field
             the table is bound to, and this says — in the words of the fallback switch
             above — which side of it a resolver from this list can answer for. -->
        <div class="ctxmenu dns-ctx" v-if="menu" ref="menuRef"
             :style="menuStyle"
             @contextmenu="e => e.preventDefault()">
            <button v-for="side in offers" :key="side" class="ctx-item"
                    @click="choose(side)">
                {{ t(side === 'remote' ? 'dns.setRemote' : 'dns.setDirect') }}
            </button>
        </div>
    </div>
</template>
