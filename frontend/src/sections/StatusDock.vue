<script setup lang="ts">
// The bottom dock and the status bar, extracted from App.tsx (the
// bottom-dock JSX block): the logo/settings button, the live rates and the run
// pill, and the exit-details panel. The batch and outbound-mode lists it also
// held have moved up to the titlebar, which is where the whole-list controls
// live now (see App.vue's titlebar-egress).
//
// The bar's text slot is gone: what still spoke there — the run state on hover
// over the lamp, the words for a mode switch in progress — is a line of the
// console's stream now (see notify in App.vue). What remains here reads what
// is moving (rates, the test pill), which is not a sentence and has nowhere
// else to be said.
import {ref, watch, nextTick, onMounted, onBeforeUnmount, type Ref} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {t} from '../i18n';
import {countryName, purityLevel, SPEED_RUNNING_KEYS} from '../lib/metrics';
import {fmtRate} from '../lib/format';
import {STOPPED} from '../composables/shared-consts';
import {CopyFilled, LoadingFilled, StopFilled, CloseFilled} from '../lib/icons';

const props = defineProps<{
    traffic: { up: number; down: number; upRate: number; downRate: number; count: number };
    testing: boolean;
    testingMode: number;
    testProgress: { done: number; total: number };
    running: number;
    exitIp: Record<string, string> | null;
    infoOpen: boolean;
    // Refs owned by App.vue's composables and effects.
    statusbarRef: Ref<HTMLDivElement | null>;
    nodeIpRef: Ref<HTMLButtonElement | null>;
    infoRef: Ref<HTMLDivElement | null>;
    // Bumped on a language change so the words here are re-read (see App.vue).
    langTick: number;
    // The node search, moved down from the titlebar: the field the reader types
    // the query into and the two counters beside it. The state — query, open or
    // closed — lives in App; this dock draws the cluster and hands the presses
    // back (see the cluster in the template).
    searchOpen: boolean;
    query: string;
    searchShown: number;
    searchPool: number;
}>();

const emit = defineEmits<{
    toggleInfo: [];
    // The copy button reports whether the clipboard really took the address:
    // the async API is unavailable on the app's custom scheme (see useLogs'
    // copyLogs), so "done" is only known after the backend fallback answered.
    copied: [ok: boolean];
    stopTest: [];
    // Double-click on the bar's floor: the console is what unrolls from the
    // window's bottom edge, so the gesture belongs to this bar (see barDbl).
    logs: [];
    // The node search's presses (see the cluster in the template): opening it,
    // the query as it is typed, the keys (Enter hands a link over, Escape takes
    // it back), the blur that closes an empty field, and the clear button.
    openSearch: [];
    setQuery: [v: string];
    searchKey: [e: KeyboardEvent];
    searchBlur: [];
    clearQuery: [];
}>();

// Double-clicking the floor of the status bar unrolls the console. The controls on
// the bar keep their own double-clicks — a press on the exit address or the test pill
// is about that control, not about the panel behind it — and the search cluster is
// part of the bar's content too, so only the floor answers.
const barDbl = (e: MouseEvent) => {
    const el = e.target as HTMLElement | null;
    if (el?.closest('button, select, input, a, .status-search')) return;
    emit('logs');
};

// The flag beside the exit address: hidden when its file did not load (a code
// the set does not carry, or a copy that did not finish) and reset when the
// country changes, so one missing file does not blank every country after it.
const flagBroken = ref(false);
watch(() => props.exitIp?.cc, () => { flagBroken.value = false; });

// The search field, for the one thing App cannot reach from here: focusing it
// the moment the cluster opens (App owns the open state; this dock owns the
// input element).
const searchInput = ref<HTMLInputElement | null>(null);
watch(() => props.searchOpen, open => {
    if (open) nextTick(() => searchInput.value?.focus());
});

// The machine's address on the LAN, shown only while LAN sharing is on: then it is
// "ip:port", the two fields a phone's proxy settings ask for, and the one thing this
// bar's right corner says while nothing is moving. A prop, because the switch that
// turns it on lives on the titlebar and its state is App.vue's; the endpoint itself
// is re-read when the flag arrives.
const lanIp = ref('');
async function loadLan() {
    if (!(await API.LANShare().catch(() => false))) { lanIp.value = ''; return; }
    lanIp.value = await API.LANEndpoint().catch(() => '') || '';
}
// Re-read on a timer as well as on the switch: the address is the machine's, and
// a Wi-Fi hop or a DHCP lease renewal leaves the one on screen naming a network
// the phone is no longer on. Once a minute is enough for a number that changes
// this rarely, and the read is two cheap bindings.
let lanTimer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
    loadLan();
    lanTimer = setInterval(loadLan, 60000);
});
onBeforeUnmount(() => clearInterval(lanTimer));
defineExpose({loadLan});

// The exit panel's copy button: the clipboard write and the note ride one press,
// so both live here rather than inline in the template (the template scope has
// no navigator of its own).
async function copyIp() {
    const text = props.exitIp?.ip ?? '';
    if (!text) return;
    // Same two-step the log panel pays (see useLogs.copyLogs): the async
    // clipboard is refused outright on the custom scheme, and the backend's
    // clipboard binding is the fallback — not an error. The empty answer that
    // used to be reported as "copied" was a promise thrown away by `void`.
    try {
        await navigator.clipboard.writeText(text);
        emit('copied', true);
        return;
    } catch { /* fall through to the backend */ }
    try {
        await API.WriteClipboard(text);
        emit('copied', true);
    } catch {
        emit('copied', false);
    }
}

// The LAN endpoint's copy: the same two-step as the exit address's (see copyIp),
// with the endpoint as the text.
async function copyLan() {
    const text = lanIp.value;
    if (!text) return;
    try {
        await navigator.clipboard.writeText(text);
        emit('copied', true);
        return;
    } catch { /* fall through to the backend */ }
    try {
        await API.WriteClipboard(text);
        emit('copied', true);
    } catch {
        emit('copied', false);
    }
}

</script>

<template>
    <!-- Bottom dock: the status bar's home. It used to hold a floating action
        bar as well, but every control on that bar had one home that suited
        it better — the node menu, which is where the node is — and a bar
        that has to be summoned to reach controls that belong to a row was
        costing the list its bottom edge twice over. -->
    <div class="bottom-dock">
        <div class="statusbar" :ref="statusbarRef" @dblclick="barDbl">
            <!-- The node search, moved down from the titlebar: the bar's left end
                was empty floor, and the field takes it when it opens. The state —
                query, open or closed — lives in App; the presses come back here
                as events, and the focus follows the open (see searchInput). -->
            <span :class="`status-search${searchOpen ? ' open' : ''}`">
                <button v-if="!searchOpen" class="tb-btn search-open" type="button"
                        :title="t('search.placeholder')"
                        :aria-label="t('search.placeholder')"
                        @click="emit('openSearch')">
                    <!-- A bare magnifier, drawn here rather than glyphed: a library
                        glyph would carry a baseline into a box whose only job is to
                        sit on the bar's centre line. -->
                    <svg class="search-glyph" viewBox="0 0 16 16" aria-hidden="true">
                        <circle cx="7" cy="7" r="4.6" fill="none" stroke="currentColor" stroke-width="1.4"/>
                        <path d="M10.6 10.6L14 14" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/>
                    </svg>
                </button>
                <template v-else>
                    <span class="search-field">
                        <svg class="search-ic" viewBox="0 0 16 16" aria-hidden="true">
                            <circle cx="7" cy="7" r="4.6" fill="none" stroke="currentColor" stroke-width="1.4"/>
                            <path d="M10.6 10.6L14 14" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/>
                        </svg>
                        <input ref="searchInput"
                               class="node-search"
                               :value="query"
                               :placeholder="t('search.placeholder')"
                               :aria-label="t('search.placeholder')"
                               @input="emit('setQuery', ($event.target as HTMLInputElement).value)"
                               @keydown="emit('searchKey', $event)"
                               @blur="emit('searchBlur')"/>
                    </span>
                    <template v-if="query.trim() !== ''">
                        <span class="status-search-count" :title="t('search.scope')">
                            {{ searchShown }}/{{ searchPool }}
                        </span>
                        <!-- The press is kept off the button so the field keeps the
                            caret: clearing is followed by the next word, and a field
                            that drops focus as it is emptied makes the reader click
                            back into it for every correction. -->
                        <button class="status-search-x"
                                :title="t('search.clear')"
                                :aria-label="t('search.clear')"
                                @mousedown.prevent
                                @click="emit('clearQuery')">
                            <CloseFilled class="ic"/>
                        </button>
                    </template>
                </template>
            </span>
            <!-- The bar's left end after the search: the rates that used to hold
                it have moved into the right-hand group, on the exit address's
                left (see .status-right), and the settings opener that shared
                this corner moved up to the navigation rail's head (see NavRail)
                before that. Nothing is positioned against the rail's line any
                more, so nothing here needs the absolute slot the rates lived
                in — the bar's only children are the search cluster, the exit
                panel and the right group, and the floor reads as the floor. -->
            <!-- The exit details, opened by the address itself: what the tunnel's other
                end is registered as, in the rows the public APIs can answer — where it
                is, who owns the network, how clean the address is. It opens upwards
                off the bar like the two lists beside it, and closes on a press
                outside or on Escape. Rows appear only when the field behind them
                exists: an API that did not answer leaves no gap to read. -->
            <div v-if="infoOpen && exitIp" class="exit-panel" :ref="infoRef" role="dialog" :aria-label="t('ip.info')">
                <div class="exit-row">
                    <span class="exit-k">{{ t('ip.address') }}</span>
                    <span class="exit-v mono">
                        {{ exitIp.ip }}
                        <button class="exit-copy" :title="t('btn.copy')"
                                :aria-label="t('btn.copy')"
                                @click="copyIp">
                            <CopyFilled class="ic"/>
                        </button>
                    </span>
                </div>
                <div v-if="exitIp.asn" class="exit-row">
                    <span class="exit-k">{{ t('ip.asn') }}</span>
                    <span class="exit-v">{{ exitIp.asn }}{{ exitIp.asOrg ? ` — ${exitIp.asOrg}` : '' }}</span>
                </div>
                <div v-if="exitIp.asDomain" class="exit-row"><span class="exit-k">{{ t('ip.asDomain') }}</span><span class="exit-v">{{ exitIp.asDomain }}</span></div>
                <div v-if="exitIp.isp" class="exit-row"><span class="exit-k">{{ t('ip.isp') }}</span><span class="exit-v">{{ exitIp.isp }}</span></div>
                <div v-if="exitIp.cc" class="exit-row">
                    <span class="exit-k">{{ t('ip.country') }}</span>
                    <span class="exit-v">
                        <img class="node-flag" :src="`/fa/flag/${exitIp.cc}.svg`" alt=""> {{ countryName(exitIp.cc) }} ({{ exitIp.cc.toUpperCase() }})
                    </span>
                </div>
                <div v-if="exitIp.region" class="exit-row"><span class="exit-k">{{ t('ip.region') }}</span><span class="exit-v">{{ exitIp.region }}</span></div>
                <div v-if="exitIp.city" class="exit-row"><span class="exit-k">{{ t('ip.city') }}</span><span class="exit-v">{{ exitIp.city }}</span></div>
                <div v-if="exitIp.timezone" class="exit-row"><span class="exit-k">{{ t('ip.timezone') }}</span><span class="exit-v">{{ exitIp.timezone }}</span></div>
                <div v-if="exitIp.lat && exitIp.lon" class="exit-row"><span class="exit-k">{{ t('ip.coords') }}</span><span class="exit-v">{{ exitIp.lat }}, {{ exitIp.lon }}</span></div>
                <div v-if="exitIp.postal" class="exit-row"><span class="exit-k">{{ t('ip.postal') }}</span><span class="exit-v">{{ exitIp.postal }}</span></div>
                <div v-if="exitIp.residential === '1' || exitIp.broadcast === '1'" class="exit-row">
                    <span class="exit-k">{{ t('ip.type') }}</span>
                    <span class="exit-v">
                        {{ exitIp.residential === '1' ? t('ip.residential') : t('ip.datacenter') }}
                        {{ ' · ' }}
                        {{ exitIp.broadcast === '1' ? t('ip.broadcast') : t('ip.native') }}
                    </span>
                </div>
                <template v-if="exitIp.purity">
                    <div class="exit-row">
                        <span class="exit-k">{{ t('ip.purity') }}</span>
                        <span class="exit-v purity-val"
                              :data-level="purityLevel(exitIp.purity)">
                            {{ exitIp.purity }}% · {{ t(`ip.purity.${purityLevel(exitIp.purity)}`) }}
                        </span>
                    </div>
                    <div class="purity-band" :style="{'--pos': `${exitIp.purity}%`}"/>
                </template>
            </div>
            <!-- The bar's centre slot: the test in progress. A run over a long
                group is minutes, and the words answer a question about the
                whole list rather than about any entry on the bar — so they
                take the middle, the way the search field takes the title
                bar's. Absolutely placed, so the right-hand group keeps its
                layout whatever the words' width. -->
            <div v-if="testing" class="status-center">
                <span class="pill pill-live">
                    <LoadingFilled class="ic spin"/>
                    {{ t(SPEED_RUNNING_KEYS[testingMode] ?? SPEED_RUNNING_KEYS[0]) }}
                    {{ testProgress.total > 0 ? ` ${testProgress.done}/${testProgress.total}` : '' }}
                    <!-- The one thing in the readout that can be pressed: a run
                        over a long group is minutes, and the numbers already
                        there are usually enough to decide by. -->
                    <button class="pill-stop"
                            :title="t('metrics.stop')"
                            :aria-label="t('metrics.stop')"
                            @click="emit('stopTest')">
                        <StopFilled class="ic"/>
                    </button>
                </span>
            </div>
            <!-- Subscriptions are not repeated here. The rail's grid button drops a
                box holding every one of them, and the same names twice within a
                hand's width of each other is one list too many — so what is left
                is what nothing else shows, in reading order: the live rates on
                the exit address's left, the running node's exit address, and
                the LAN endpoint last so it sits against the window edge. -->
            <div class="status-right">
                <!-- The live rates, on the exit address's left: a readout of what
                    is moving, up and down — the two directions the tunnel
                    carries. Permanent now: an idle readout ("↓ 0 B/s ↑ 0 B/s")
                    is itself the state of the tunnel, and a block that appears
                    only under load made the bar's right group reshuffle — the
                    address sliding left as the rates arrived and back as they
                    went. The hairline between it and the address is the right
                    group's own divider (see .status-right). -->
                <div :class="`traffic${traffic.upRate > 0 || traffic.downRate > 0 ? '' : ' idle'}`">
                    <span class="rate down" :title="t('status.rateHint')">↓ {{ fmtRate(traffic.downRate) }}</span>
                    <span class="rate up" :title="t('status.rateHint')">↑ {{ fmtRate(traffic.upRate) }}</span>
                </div>
                <!-- The address this machine is actually talking to. A node named by a hostname is a name
                    the reader cannot check against a firewall log, so the resolved
                    address is what the bar shows; the profile's own name and address are
                    in the tooltip, which is where the two can be compared. Nothing at
                    all while no node is up — an empty corner says that better than a
                    dash would. -->
                <button v-if="running !== STOPPED && exitIp?.ip" class="node-ip" :ref="nodeIpRef"
                        :title="t('ip.open')"
                        :aria-expanded="infoOpen"
                        aria-haspopup="dialog"
                        @click="emit('toggleInfo')">
                    <!-- The address the world sees, not the node's own: pressing
                        it opens the exit details. The flag at its right is that
                        address's country — the same artwork the panel's country
                        row shows, so the bar and the panel say one thing. -->
                    {{ exitIp.ip }}
                    <img v-if="exitIp.cc && !flagBroken" class="node-flag"
                         :src="`/fa/flag/${exitIp.cc}.svg`"
                         alt="" aria-hidden="true"
                         @error="flagBroken = true"/>
                </button>
                <!-- The LAN endpoint, while sharing is on: what a phone on the same
                    Wi-Fi dials. Last on the bar, against the window edge — the exit
                    address follows the node, this one is the machine's own, and the
                    reading order runs from the tunnel's other end back home. A press
                    copies it — the same two-step the exit panel's copy button pays
                    (see copyIp) — so the address can be typed on the phone rather
                    than retold by eye. The hairline between it and the exit address
                    is the right group's own divider (see .status-right). -->
                <button v-if="lanIp" class="lan-ip mono" type="button"
                        :title="t('status.lanHint')"
                        @click="copyLan">
                    {{ lanIp }}
                </button>
            </div>
        </div>
    </div>
</template>
