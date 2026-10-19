// The reel — the heap of subscription tables the drawer puts in the table's own
// box — extracted from App.tsx (the stack block). The stack computed, the --i
// re-ranking written straight onto the cards, and the room measurement that goes
// onto the layer's custom properties.
import {computed, ref, watch, type Ref} from 'vue';
import {main} from '../../wailsjs/go/models';
import {STACK_ASPECT, STACK_DEPTH, STACK_H_FILL, STACK_N, STACK_P_SHARE, STACK_ROOM_FALLBACK, STACK_STEP, STACK_TILT} from '../lib/stack';

export function useReelStack(deps: {
    groups: Ref<main.GroupInfo[]>;
    gid: Ref<number>;
    subsDrop: Ref<boolean>;
    panelRef: Ref<HTMLDivElement | null>;
}) {
    // Which subscriptions the stack holds, and in what order: the reader's own on top and
    // the ones after it, wrapping round the end of the list, so the stack is four deep
    // whenever there are four subscriptions to fill it and as deep as the list is long
    // when there are fewer. The window follows the selection, which is what puts the
    // current subscription on top without anything having to move it up there.
    const stack = computed<main.GroupInfo[]>(() => {
        const groups = deps.groups.value;
        const n = groups.length;
        if (n === 0) return [] as main.GroupInfo[];
        const at = Math.max(0, groups.findIndex(x => x.id === deps.gid.value));
        const out = [] as main.GroupInfo[];
        for (let k = 0; k < Math.min(STACK_N, n); k++) out.push(groups[(at + k) % n]);
        return out;
    });

    // The room the stack stands in, read off the layer as it is laid out. The layer is
    // exactly the table's visible box (see .reel), so its width is what the three shares at
    // the top of this file are shares of; measured rather than worked out from the window,
    // because the arithmetic that decides it — the rail, the drawer's dragged width, the gap
    // it stands off — is already in the stylesheet, and a second copy here is a copy that can
    // drift from it. Observed rather than read once, because the drawer's edge can be dragged
    // wider while the stack is up. The observation goes straight onto the element's custom
    // properties (see the layout effect below) and never becomes state: these three numbers
    // are read by CSS and by nothing else, and a state copy of them re-rendered the whole
    // application on every frame of a drawer opening or a window resize.
    const reelRef = ref<HTMLDivElement | null>(null);
    const stackRef = ref<HTMLDivElement | null>(null);

    // The reader's pointed-at table is brought to the front by writing --i straight onto
    // the four cards — one write each, and no state at all. --i is everything a card's
    // place in the heap is made of: the step to the left, the push back in Z, the veil's
    // opacity. Re-ranking the heap is therefore four numbers on four elements, and a lift
    // is a transform transition (see .reel-card's .16s) rather than a render.
    //
    // It was a setState before — reelTop — and every lift re-rendered the whole
    // application: measured at 232ms per lift in dev, all of it inside React's scheduler
    // (performWorkUntilDeadline). That is what "the heap does not come forward smoothly"
    // was. Nothing about the re-ranking belongs to React: no card mounts or unmounts, and
    // no other part of the app asks which one is in front.
    //
    // It reads stackRef and stack from further up this file on purpose: this is only
    // ever called from a pointer handler or an effect, both of which run long after those
    // are initialised.
    const paintStack = (topId: number | null) => {
        const el = stackRef.value;
        if (!el) return;
        const ids = stack.value.map(g => g.id);
        // The stack's own order with the lifted table moved to the front. Every other
        // table keeps its place relative to the rest, so only the lifted one and the ones
        // it passed over swap ranks.
        const order = topId === null || !ids.includes(topId)
            ? ids
            : [topId, ...ids.filter(id => id !== topId)];
        order.forEach((id, i) => {
            const card = el.querySelector<HTMLElement>(`.reel-card[data-gid="${id}"]`);
            if (!card) return;
            card.style.setProperty('--i', String(i));
            card.classList.toggle('front', i === 0);
        });
    };
    // One lift per visit, and per place the pointer can lift from. Lifting a table moves
    // it out from under the pointer and puts another one there, so a plain hover handler
    // would have the two of them trade places over and over while the pointer stood still.
    // The lock is what makes it read as the pointer picking one table rather than the heap
    // chasing the pointer — and it remembers which of the two places the pointer is at, so
    // travelling from the drawer's card across to the heap counts as a new visit and is
    // answered rather than ignored.
    const reelTopLock: {current: 'drawer' | 'stack' | null} = {current: null};
    const liftReel = (id: main.GroupInfo['id'], from: 'drawer' | 'stack') => {
        if (reelTopLock.current === from) return;
        reelTopLock.current = from;
        paintStack(id);
    };
    const dropReel = (from: 'drawer' | 'stack') => {
        if (reelTopLock.current !== from) return;
        reelTopLock.current = null;
        paintStack(null);
    };
    // The drawer's own cards, which lift their table the moment the pointer arrives — the
    // reader asked for it there, and it is the only way to see a subscription that the heap
    // holds but has not brought to the front. It does not change which subscription is
    // current: that is still hoverSwitch's business, on its own delay, because looking at a
    // card and choosing it are two different things and only one of them is a change.
    //
    // A subscription the heap does not hold cannot be lifted, and nothing is done about it:
    // the heap is four deep out of however many there are, and bringing one in from outside
    // would mean changing what the heap is — which table it is standing in for, what the
    // current one's neighbours are. So the pointer lands on such a card and the heap is
    // simply not about it.
    const peekReel = (id: main.GroupInfo['id']) => {
        if (!stack.value.some(x => x.id === id)) return;
        liftReel(id, 'drawer');
    };
    // Which table the pointer is over, read off where the tables are rather than off what
    // the browser says is under it. The strips of border a table behind shows are the only
    // part of it that can be pointed at, and a table standing back in Z is not reliably
    // hit-tested where it is drawn — elementFromPoint returns the heap itself for most of
    // those strips, so the pointer would pass through what is plainly there. The eye, on
    // the other hand, stands on the room's right border (see the perspective-origin under
    // .reel), so that border is the one edge of a table the perspective does not move:
    // a table's right edge stands at the room's right edge less its own step times how
    // many tables are behind it. That makes the strips exact — a pointer is inside the
    // topmost table's face to the left of its edge, inside the next one's strip up to the
    // next edge, and so on to the rearmost — with no allowance and nothing to tune.
    const reelUnder = (clientX: number, right: number) => {
        const n = stack.value.length;
        if (!n) return null;
        let k = 0;
        while (k < n - 1 && right - (n - 1 - k) * STACK_STEP <= clientX) k++;
        return stack.value[k];
    };
    const trackReel = (e: MouseEvent) => {
        // Once something has been lifted the ranks are no longer the stack's own, and this
        // arithmetic reads them off the stack — so it is only asked while nothing has been
        // lifted, which is what the lock already says.
        if (reelTopLock.current === 'stack') return;
        const g = reelUnder(e.clientX, (e.currentTarget as HTMLElement).getBoundingClientRect().right);
        if (g && stack.value[0].id !== g.id) liftReel(g.id, 'stack');
    };

    // The order the cards are painted in is the stack's own and stays that way: a lift is
    // not a re-render any more, so there is no second order for the renderer to hold (see
    // paintStack). What a lift changes is each card's --i, and where a card covers another
    // follows from that by itself — they are absolutely positioned in a preserve-3d space,
    // so depth decides what is in front.
    //
    // A stack that changes — another subscription, one added — is painted from here again:
    // the numbers left over from the last lift describe a heap that no longer exists.
    watch(stack, () => {
        paintStack(null);
    }, {flush: 'post', immediate: true});

    // A table's box, which is two ceilings and a division, and which is deliberately not
    // computed here. The room the heap stands in is the layer as it is laid out, so its size
    // is observed (see below), and every observation used to arrive as a setState: while the
    // drawer slid, and again on every frame of a window resize, each one re-rendering the
    // whole application to change three numbers that only ever reach CSS custom properties.
    // The observer now writes them on the element itself, so a resize costs a style write and
    // nothing else. What the reader asked for is still what the arithmetic below says: the
    // table on top reaches from the room's right edge — which its hinge sits on — all the way
    // to the room's left border, less the steps the tables behind it stand out by; the height
    // is whatever the room has left over under the proportion. None of it is searched, and
    // that is the whole difference from the ring this replaced: the depth behind the stack is
    // a fixed STACK_DEPTH rather than a radius traded against a card's size, so the two
    // ceilings price each other instead of fighting.
    watch([deps.subsDrop, () => deps.groups.value.length], (_, __, onCleanup) => {
        const layer = reelRef.value;
        const stackEl = stackRef.value;
        if (!layer || !stackEl) return;
        const deep = Math.min(STACK_N, Math.max(1, deps.groups.value.length));
        const tilt = Math.cos(STACK_TILT * Math.PI / 180);
        const sin = Math.sin(STACK_TILT * Math.PI / 180);
        const paint = () => {
            const r = layer.getBoundingClientRect();
            // STACK_ROOM_FALLBACK is what the very first frame is sized against, before the
            // layer has reported its own box — that frame is at the start of the fade in.
            const roomW = r.width > 0 ? r.width : STACK_ROOM_FALLBACK.w;
            const roomH = r.height > 0 ? r.height : STACK_ROOM_FALLBACK.h;
            // Where the eye stands (see STACK_P_SHARE). It is wanted before the width,
            // because the perspective is part of how much of a table the reader gets to see.
            const p = roomW * STACK_P_SHARE;
            // The span to fill: the room's width less the steps, which is what the top
            // table's left border has to travel to arrive on the room's left border.
            const span = Math.max(140, roomW - STACK_STEP * (deep - 1));
            // Solving for the table width that puts exactly that span of table on the screen.
            // Dividing by the cosine alone is not enough in general: the hinge end is the near
            // end and the far end stands back by sin(tilt) of the width, so the perspective
            // draws the two ends at two different sizes and their average falls a little under
            // the cosine. Spreading that depth evenly across the face makes the average
            // 2P/(2P + W sin), and solving W cos · 2P/(2P + W sin) = span for W is the term
            // below. At no tilt the sine is nought and the whole term collapses to the span,
            // which is the plain statement that a table standing square to the room reaches
            // the border at exactly its own width. The denominator is floored because it
            // turns negative for a room so narrow that no table reaches, and the width
            // ceiling is there to catch that case anyway.
            const reach = span * 2 * p / Math.max(1, 2 * p * tilt - span * sin);
            // The height first, and from the room rather than from the width: the heap is
            // meant to stand just inside the frame the table's own box draws, with a small
            // even gap above and below, and only a height taken from the room says that at
            // every window size. The width is then whatever the room can give it, capped by
            // what the height allows at this proportion.
            // The height, from the drawer rather than from the room: the heap's top
            // table and the window beside it should read as one leaf standing at one
            // height, so the table is measured against the drawer's own box — its
            // layout height (offsetHeight), which the door's rotating entrance
            // cannot bend, where a projected rect would shrink with the swing and
            // never be asked about again. Before the drawer has reported, the old
            // proportion stands in.
            const panelH = deps.panelRef.value?.offsetHeight ?? 0;
            const h = panelH > 0 ? Math.max(100, panelH) : Math.max(100, roomH * STACK_H_FILL);
            const w = Math.max(140, Math.min(reach, h * STACK_ASPECT));
            stackEl.style.setProperty('--stack-w', w + 'px');
            stackEl.style.setProperty('--stack-h', h + 'px');
            stackEl.style.setProperty('--stack-p', p + 'px');
        };
        paint();
        const ro = new ResizeObserver(paint);
        ro.observe(layer);
        onCleanup(() => ro.disconnect());
    }, {flush: 'post', immediate: true});

    // The stack is up exactly when this is what the template mounts.
    const reelOn = computed(() => deps.subsDrop.value && deps.groups.value.length > 1);

    // The lift has to be let go of when the heap goes down. It is mounted afresh each time
    // the drawer opens, and a lift still held across that would come back as a table held
    // up for a pointer that is not over the heap — and, the lock with it, would not let the
    // reader lift anything at all.
    watch(deps.subsDrop, drop => {
        if (drop) return;
        reelTopLock.current = null;
    });

    return {stack, reelRef, stackRef, reelOn, paintStack, liftReel, dropReel, peekReel, trackReel};
}
