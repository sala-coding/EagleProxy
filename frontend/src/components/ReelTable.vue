<script setup lang="ts">
import {computed, ref, watch, type CSSProperties} from 'vue';
import * as API from "../../wailsjs/go/main/App";
import {main} from "../../wailsjs/go/models";
import {t} from "../i18n";
import {hueFor} from "../lib/metrics";
import ReelRow from "./ReelRow.vue";

// ReelTable is one table in the heap, and it owns its own nodes.
//
// The owning is the whole point. The heap's four faces were once fetched together into one
// record in the component that holds the whole application, which meant that the arrival of
// those faces re-rendered everything on the screen inside the drawer's opening — the door's
// .44s and a second and a half of script were the same event. A face that holds its own rows
// re-renders itself and nothing else, so the four can arrive whenever they like.
//
// Arriving whenever they like is arranged rather than left to chance: no face asks before the
// drawer has finished opening (`ready`, see reelReady), and each waits its own turn after that,
// so the faces fill from the front of the heap back and the work is spread across four frames
// instead of arriving as one. A face with nothing to show yet draws standing-in rows rather
// than an empty box or the "no nodes" line, because the line is a statement about the
// subscription and the standing-in rows are a statement about the loading.

const props = defineProps<{
    g: main.GroupInfo;
    layer: number;
    total: number;
    ready: boolean;
    seed?: main.ProfileInfo[];
    // The profiles epoch (see App.tsx): bumped on every groups:changed. A card
    // that is not the current group gets no seed, and its rows — once fetched —
    // never refreshed, so a subscription rewritten while the drawer stood open
    // left the card on its stale list (an empty one, for a fresh import) until
    // the drawer was closed and opened again. A bump tells the card to ask
    // again. The current group's card is fed fresh rows by seed, so it takes
    // the constant 0 and never re-asks on its own.
    epoch?: number;
}>();

// null is not the same as empty: null is a face that has not asked yet, [] is a
// subscription that has none.
const rows = ref<main.ProfileInfo[] | null>(props.seed ?? null);

// A seed that arrives after the first render (e.g. a reopened drawer) must not be
// ignored — keep the cached rows from a previous mount from sticking.
watch(() => props.seed, s => {
    rows.value = s ?? null;
}, {immediate: true});

watch([() => props.epoch, () => props.ready], ([epoch, ready]) => {
    if (!ready || epoch === undefined || epoch === 0) return;
    rows.value = null;
}, {immediate: true});

watch([() => props.ready, rows, () => props.layer, () => props.g.id], (_, __, onCleanup) => {
    if (!props.ready || rows.value !== null) return;
    let alive = true;
    const timer = window.setTimeout(() => {
        API.ListProfiles(props.g.id)
            .then(r => {
                if (alive) rows.value = r;
            })
            .catch(() => {
                if (alive) rows.value = [];
            });
    }, props.layer * 70);
    onCleanup(() => {
        alive = false;
        window.clearTimeout(timer);
    });
}, {immediate: true});

// data-layer is where this card starts in the heap, and the stylesheet turns it into
// --i (see .reel-card[data-layer] in App.css). React writes no --i of its own any
// more: a later re-ranking is written straight onto the elements by paintStack, and
// a re-render that re-wrote --i from this prop would push the lift back down — the
// heap would jump home on the next unrelated render.
const cardStyle = computed(() => ({
    '--n': String(props.total),
    // The card's own subscription colour, the same value the drawer's card and
    // the main table's type column carry (see hueFor): a card is one
    // subscription, so its protocol column wears that subscription's colour.
    '--tab-hue': hueFor(props.g.id),
} as CSSProperties));
</script>

<template>
    <div :class="'reel-card' + (layer === 0 ? ' front' : '')"
         :data-gid="g.id"
         :data-layer="layer"
         :style="cardStyle">
        <div class="reel-head">
            <span class="reel-name">{{ g.name }}</span>
            <span class="reel-count">{{ g.profileCount }}</span>
        </div>
        <div class="reel-list">
            <template v-if="rows === null">
                <div class="reel-row skel" v-for="k in 4" :key="k">
                    <span/><span/><span/><span/><span/>
                    <span/><span/><span/><span/><span/>
                </div>
            </template>
            <template v-else>
                <ReelRow v-for="p in rows" :key="p.id" :p="p"/>
            </template>
            <div v-if="rows !== null && rows.length === 0" class="reel-row dim">{{ t('table.empty') }}</div>
        </div>
    </div>
</template>
