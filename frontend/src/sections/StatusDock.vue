<script setup lang="ts">
// The bottom dock and the status bar, extracted from App.tsx (the
// bottom-dock JSX block): the logo/settings button, the live rates and the run
// pill, and the exit-details panel. The batch and outbound-mode lists it also
// held have moved up to the titlebar, which is where the whole-list controls
// live now (see App.vue's titlebar-batch and titlebar-egress).
//
// The bar's text slot is gone: what still spoke there — the run state on hover
// over the lamp, the words for a mode switch in progress — is a line of the
// console's stream now (see notify in App.vue). What remains here reads what
// is moving (rates, the test pill), which is not a sentence and has nowhere
// else to be said.
import {ref, watch, type Ref} from 'vue';
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
}>();

const emit = defineEmits<{
    toggleInfo: [];
    // The copy button reports whether the clipboard really took the address:
    // the async API is unavailable on the app's custom scheme (see useLogs'
    // copyLogs), so "done" is only known after the backend fallback answered.
    copied: [ok: boolean];
    stopTest: [];
}>();

// The flag beside the exit address: hidden when its file did not load (a code
// the set does not carry, or a copy that did not finish) and reset when the
// country changes, so one missing file does not blank every country after it.
const flagBroken = ref(false);
watch(() => props.exitIp?.cc, () => { flagBroken.value = false; });

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

</script>

<template>
    <!-- Bottom dock: the status bar's home. It used to hold a floating action
        bar as well, but every control on that bar had one home that suited
        it better — the node menu, which is where the node is — and a bar
        that has to be summoned to reach controls that belong to a row was
        costing the list its bottom edge twice over. -->
    <div class="bottom-dock">
        <div class="statusbar" :ref="statusbarRef">
            <!-- The bar's left slot, against the rail's line: the live rates.
                The transient WORDS that used to speak here (the run state on
                hover, the app's notices) are lines of the console now. The slot
                is positioned and its contents are not, which is what keeps the
                controls on the right: an in-flow child of the bar would be
                carried there, half a window away from the rail's line. The
                settings opener that used to share this corner has moved up to
                the navigation rail's head (see NavRail). -->
            <div class="status-left">
                <!-- The live rates at the bar's far left: a readout of what is
                    moving. It only exists while something is being sent or
                    received. -->
                <div v-if="traffic.upRate > 0 || traffic.downRate > 0" class="traffic">
                    <span class="rate down" :title="t('status.rateHint')">↓ {{ fmtRate(traffic.downRate) }}</span>
                    <span class="rate up" :title="t('status.rateHint')">↑ {{ fmtRate(traffic.upRate) }}</span>
                </div>
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
                is what nothing else shows, in reading order: the log and
                connections entries, the running node, the run state last so it
                sits against the window edge. -->
            <div class="status-right">
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
            </div>
        </div>
    </div>
</template>
