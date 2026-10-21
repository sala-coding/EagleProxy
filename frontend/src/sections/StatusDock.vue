<script setup lang="ts">
// The bottom dock and the status bar, extracted from App.tsx (the
// bottom-dock JSX block): the logo/settings button, the text slot with the
// live rates and the run pill, the exit-details panel, the node search, the
// batch and egress menus, the node chip and the run-state lamp.
import {type Ref} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {t} from '../i18n';
import {countryName, purityLevel, SPEED_RUNNING_KEYS} from '../lib/metrics';
import {fmtRate} from '../lib/format';
import {EGRESS_MODES} from '../lib/layout';
import {STOPPED} from '../composables/shared-consts';
import {CopyFilled, LoadingFilled, ServerFilled, StopFilled, CloseFilled} from '../lib/icons';
import {type StatusNote} from '../composables/useStatusNote';

const props = defineProps<{
    traffic: { up: number; down: number; upRate: number; downRate: number; count: number };
    statusNote: StatusNote | null;
    testing: boolean;
    testingMode: number;
    testProgress: { done: number; total: number };
    egress: string;
    egressOpen: boolean;
    batchOpen: boolean;
    query: string;
    subBusy: boolean;
    shownCount: number;
    poolCount: number;
    running: number;
    isRunning: boolean;
    exitIp: Record<string, string> | null;
    flagMissing: boolean;
    infoOpen: boolean;
    proxyEngaged: boolean;
    settingsOpen: boolean;
    // Refs owned by App.vue's composables and effects.
    statusbarRef: Ref<HTMLDivElement | null>;
    egressRef: Ref<HTMLSpanElement | null>;
    batchRef: Ref<HTMLSpanElement | null>;
    nodeIpRef: Ref<HTMLButtonElement | null>;
    infoRef: Ref<HTMLDivElement | null>;
    // Bumped on a language change so the words here are re-read (see App.vue).
    langTick: number;
}>();

const emit = defineEmits<{
    openSettings: [];
    setQuery: [v: string];
    submitSub: [];
    toggleBatch: [];
    runBatch: [kind: 'dedupe' | 'failed' | 'resolve'];
    toggleEgress: [];
    setEgress: [mode: string];
    toggleInfo: [];
    // The copy button reports whether the clipboard really took the address:
    // the async API is unavailable on the app's custom scheme (see useLogs'
    // copyLogs), so "done" is only known after the backend fallback answered.
    copied: [ok: boolean];
    flagMissing: [];
    stopTest: [];
    stateEnter: [];
    stateLeave: [];
}>();

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

// The search box's two jobs are told apart by what was pasted, not by a state
// the reader had to switch first. Only a link takes the Enter.
const onSearchKey = (e: KeyboardEvent, query: string, subBusy: boolean) => {
    if (e.key === 'Escape') {
        emit('setQuery', '');
        return;
    }
    if (e.key !== 'Enter' || subBusy) return;
    if (!/^https?:\/\//i.test(query.trim())) return;
    e.preventDefault();
    emit('submitSub');
};
</script>

<template>
    <!-- Bottom dock: the status bar's home. It used to hold a floating action
        bar as well, but every control on that bar had one home that suited
        it better — the node menu, which is where the node is — and a bar
        that has to be summoned to reach controls that belong to a row was
        costing the list its bottom edge twice over. -->
    <div class="bottom-dock">
        <div class="statusbar" :ref="statusbarRef">
            <!-- The app's own mark, in the box the rail's foot and the bar's left
                end make together: the strip's width by the bar's height, which is
                the one corner in the window that belongs to neither of them and
                is drawn by both. It grows to the edges of that box under the
                pointer and lights up — see .status-logo:hover, which is also the
                only reason it takes pointer events. It is the settings panel's
                own button now: the mark is the face of the app, and pressing it
                pushes the settings panel up from the bar's left end — the same
                engine, its own cup holder. The artwork inside it is the app's
                eagle mark (fa/icon/eagles.jpg), cropped to its square by
                .status-logo img. -->
            <button class="status-logo" type="button"
                    :title="t('btn.settings')"
                    :aria-label="t('btn.settings')"
                    :aria-pressed="settingsOpen"
                    @click="emit('openSettings')">
                <!-- The app's eagle mark rather than the language flag: the
                    source file is a portrait artwork with the logo square
                    centred inside black margins, and object-fit: cover (see
                    .status-logo img.logo-eagle) crops straight to the square —
                    sharp at any pixel ratio, identical in every language. -->
                <img class="logo-eagle" src="/fa/icon/eagles.jpg" alt="">
            </button>
            <!-- The bar's one text slot, hung off the rail's vertical line: every transient
                word — run state on hover, "node connected" when a start lands —
                grows out of that line and fades where it stands after three
                seconds. The stylesheet anchors it to the line itself rather than
                to the bar's left edge (see .status-note), so the two cannot come
                apart; the controls keep the right edge. -->
            <!-- The bar's left slot: everything that speaks while something is
                happening — a notify toast, and the test in progress — in one place
                rather than at two ends of the bar, because they answer the same
                question and both grow out of the rail's own line. Standing side by
                side is then possible: the run and the word for it are two things
                said at once, not one written over the other.
                The slot is positioned and its contents are not, which is what keeps
                the controls on the right: an in-flow child of the bar would be
                carried to the right edge by the bar's own justify-content. -->
            <div class="status-left">
                <!-- The live rates lead the slot: they sit against the rail's own
                    line, the first thing after the divider, and the transient
                    words (a hover hint, a test in progress) follow them. The
                    block only exists while something is moving — with the node
                    idle the slot is the note's alone, as it always was. -->
                <div v-if="traffic.upRate > 0 || traffic.downRate > 0" class="traffic">
                    <span class="rate down" :title="t('status.rateHint')">↓ {{ fmtRate(traffic.downRate) }}</span>
                    <span class="rate up" :title="t('status.rateHint')">↑ {{ fmtRate(traffic.upRate) }}</span>
                </div>
                <span v-if="statusNote && statusNote.mode === 'bar'" :key="statusNote.seq" role="status"
                      :class="`status-note${statusNote.leaving ? ' leaving' : ''}${statusNote.kind === 'err' ? ' err' : ''}`">
                    {{ statusNote.text }}
                </span>
                <span v-if="testing" class="pill pill-live">
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
            <!-- Subscriptions are not repeated here. The rail's grid button drops a
                box holding every one of them, and the same names twice within a
                hand's width of each other is one list too many — so what is left
                is what nothing else shows, in reading order: the log and
                connections entries, the running node, the run state last so it
                sits against the window edge. -->
            <!-- The node search, centred on the window. A search is a way of looking
                at the list rather than a property of it, and the bar is the one
                place in this window that is about the whole list at once — which is
                where it belongs, and why it is not in the table's own strip. It is
                absolutely placed so that the bar's two ends keep the layout they
                have; this answers to the window's centre, not to them. No glyph
                beside it: the placeholder already says what it is, and the bar is
                not the place to say it twice.
                And the box takes subscriptions: a link pasted in and Enter-ed
                becomes one — the record made, the pull run, and the drawer standing
                open with the result on it (see addSubFromSearch). Nothing else
                Enter-ed here need do anything; the placeholder says both jobs in
                the same breath ("搜索节点+订阅"), the reader's one hint. -->
            <span class="status-search">
                <input :class="`node-search${subBusy ? ' busy' : ''}`"
                       :value="query"
                       :placeholder="t('search.placeholder')"
                       :aria-label="t('search.placeholder')"
                       @input="emit('setQuery', ($event.target as HTMLInputElement).value)"
                       @keydown="onSearchKey($event, query, subBusy)"/>
                <template v-if="query.trim() !== ''">
                    <!-- The denominator is what the search read, which is every
                        subscription while the box is in use — the count follows
                        the same list the rows come from. -->
                    <span class="status-search-count" :title="t('search.scope')">
                        {{ shownCount }}/{{ poolCount }}
                    </span>
                    <button class="status-search-x"
                            :title="t('search.clear')"
                            :aria-label="t('search.clear')"
                            @click="emit('setQuery', '')">
                        <CloseFilled class="ic"/>
                    </button>
                </template>
            </span>
            <div class="status-right">
                <!-- The three sweeps over the group. They sit on the bar rather than in
                    a strip above the table because they are about the list as a whole,
                    which is what the bar is for; two of the three destroy nodes, so
                    they are behind a list rather than three buttons in the open. The
                    control is the same shape as the outbound mode beside it — a word
                    and an upward triangle, with the choices above — because both are
                    "one of a few things" on a bar that stands on the window's floor. -->
                <span class="batch-wrap" :ref="batchRef">
                    <button class="egress-trigger"
                            aria-haspopup="menu"
                            :aria-expanded="batchOpen"
                            :title="t('batch.title')"
                            :aria-label="t('batch.title')"
                            @click="emit('toggleBatch')">
                        {{ t('batch.title') }}
                    </button>
                    <div v-if="batchOpen" class="egress-menu" role="menu"
                         :aria-label="t('batch.title')">
                        <button role="menuitem" @click="emit('runBatch', 'dedupe')">
                            {{ t('batch.dedupe') }}
                        </button>
                        <button role="menuitem" @click="emit('runBatch', 'failed')">
                            {{ t('batch.removeFailed') }}
                        </button>
                        <button role="menuitem" @click="emit('runBatch', 'resolve')">
                            {{ t('batch.resolve') }}
                        </button>
                    </div>
                </span>
                <!-- How traffic leaves — the routing profile decides, everything goes
                    out unproxied, or everything goes through the proxy. It stands
                    first because it is not an entry into something the way the two
                    beside it are: it is the mode the rest of this half of the bar
                    reports on, so it reads as the heading of the group rather than
                    as a fourth item wedged between the ones that open windows. The
                    list opens upwards — the bar is the window's floor — and the
                    colour carries the mode. -->
                <span class="egress" :data-mode="egress" :ref="egressRef">
                    <button class="egress-trigger"
                            aria-haspopup="listbox"
                            :aria-expanded="egressOpen"
                            :title="t('egress.title')"
                            :aria-label="t('egress.title')"
                            @click="emit('toggleEgress')">
                        {{ t(`egress.${egress}`) }}
                    </button>
                    <div v-if="egressOpen" class="egress-menu" role="listbox"
                         :aria-label="t('egress.title')">
                        <button v-for="m in EGRESS_MODES" :key="m" role="option"
                                :aria-selected="egress === m"
                                :class="egress === m ? 'on' : ''"
                                @click="emit('setEgress', m)">
                            {{ t(`egress.${m}`) }}
                        </button>
                    </div>
                </span>
                <!-- The address this machine is actually talking to, beside the mode that
                    decides where the traffic goes. A node named by a hostname is a name
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
                        it opens the exit details, and the flag in the chip beside
                        it is that address's country. Nothing here names the node
                        — this corner of the bar is about the traffic, and the
                        node is on its row in the table. -->
                    {{ exitIp.ip }}
                </button>
                <span v-if="isRunning" class="node-chip"
                      :title="countryName(exitIp?.cc ?? '') || undefined">
                    <!-- The exit address's country, where the generic server glyph
                        used to be: the chip says which node is up, and where the
                        traffic comes out is the more useful half of that. The
                        artwork is one file per country under fa/flag (see
                        LICENSE-flag-icons.txt beside them), so the code the
                        backend returns is turned into a path here and nothing
                        more. Falls back to the rack when there is no code, and
                        again when the file for that code is not there: a broken
                        image would be the one answer worse than the rack. -->
                    <img v-if="exitIp?.cc && !flagMissing" class="node-flag"
                         :src="`/fa/flag/${exitIp.cc}.svg`"
                         alt="" aria-hidden="true"
                         @error="$emit('flagMissing')"/>
                    <ServerFilled v-else class="ic"/>
                </span>
                <!-- The lamp: a steady green dot while the core is merely up, and a green
                    bar turning on the spot once a proxy mode is engaged (see
                    proxyEngaged) — the same state the rail's two switches carry, in
                    the one place that stays on screen whatever else is open.
                    The engaged state changes the glyph rather than animating the dot:
                    a circle has no orientation, so a circle turning looks like nothing
                    at all. The bar is drawn in CSS (see .state-spinner) rather than
                    glyphed, because Font Awesome has no bare slash — and a slash
                    character would bring a font's own proportions along to a thing
                    whose only property is its angle. -->
                <span class="state-wrap"
                      @mouseenter="emit('stateEnter')"
                      @mouseleave="emit('stateLeave')">
                    <span :class="`state-chip ${isRunning ? 'on' : 'off'}${proxyEngaged ? ' spinning' : ''}`">
                        <i v-if="proxyEngaged" class="state-spinner"/>
                        <i v-else class="state-dot"/>
                    </span>
                </span>
            </div>
        </div>
    </div>
</template>
