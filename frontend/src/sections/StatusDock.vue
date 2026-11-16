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
import {computed, ref, watch, onMounted, onBeforeUnmount, type Ref} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {t} from '../i18n';
import {countryName, purityLevel, SPEED_RUNNING_KEYS} from '../lib/metrics';
import {fmtRate} from '../lib/format';
import {STOPPED} from '../composables/shared-consts';
import {CheckCircleFilled, CopyFilled, LoadingFilled, StopFilled} from '../lib/icons';

const props = defineProps<{
    traffic: { up: number; down: number; upRate: number; downRate: number; count: number };
    testing: boolean;
    testingMode: number;
    testProgress: { done: number; total: number };
    running: number;
    exitIp: Record<string, string> | null;
    infoOpen: boolean;
    // The start-up TUN prewarm reached a live resident server: the bar's green tick.
    tunPrewarmOK: boolean;
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
    // Double-click on the bar's floor: the console is what unrolls from the
    // window's bottom edge, so the gesture belongs to this bar (see barDbl).
    logs: [];
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

// The report behind the purity band, as the backend handed it over in one JSON string
// (see riskDetail in app_exitip.go): per-source contributions, the votes behind the
// proxy/vpn reading, how much of it was live, and the registry the range belongs to.
type RiskDetail = {
    final?: number;
    contributors?: {source: string; risk: number; weight: number}[];
    votes?: {key: string; count: number; of: number}[];
    disagreement?: boolean;
    confidence?: string;
    bucket?: string;
    current?: number;
    stale?: number;
    assessedAt?: string;
    registry?: string;
    reasons?: string[];
};
const riskOpen = ref(false);
// The fold belongs to the address it was opened over: a new address is a new report,
// and an open one would show the next node's sources as if they had been read.
watch(() => props.exitIp?.ip, () => { riskOpen.value = false; });
// And to the opening: a panel put away comes back folded, the way it first opened,
// rather than resuming a detail the reader had finished with.
watch(() => props.infoOpen, open => { if (!open) riskOpen.value = false; });
const riskDetail = computed<RiskDetail | null>(() => {
    const raw = props.exitIp?.riskDetail;
    if (!raw) return null;
    try {
        return JSON.parse(raw) as RiskDetail;
    } catch {
        return null;
    }
});
const voteText = computed(() =>
    (riskDetail.value?.votes ?? []).map(v => `${v.key} ${v.count}/${v.of}`).join(' · '));
const evidenceText = computed(() =>
    [riskDetail.value?.confidence, riskDetail.value?.bucket].filter(Boolean).join(' · '));
const sourceText = computed(() => {
    const d = riskDetail.value;
    if (!d || d.current === undefined) return '';
    const total = (d.current ?? 0) + (d.stale ?? 0);
    let text = `${d.current} / ${total}`;
    if (d.assessedAt) {
        const at = new Date(d.assessedAt);
        if (!Number.isNaN(at.getTime())) text += ` · ${at.toLocaleString()}`;
    }
    return text;
});
const riskLevelOf = (v: number) => v < 30 ? 'low' : v < 60 ? 'mid' : 'high';

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
// Same two-step the log panel pays (see useLogs.copyLogs): the async clipboard is
// refused outright on the custom scheme, and the backend's binding is the fallback —
// not an error. The empty answer that used to be reported as "copied" was a promise
// thrown away by `void`.
async function writeClipboard(text: string) {
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

async function copyIp() {
    const text = props.exitIp?.ip ?? '';
    if (!text) return;
    await writeClipboard(text);
}

// The whole reading as one block, in the panel's own order and words: what a bug report
// or a support thread asks for, without retyping a dozen fields.
async function copyAll() {
    const e = props.exitIp;
    if (!e) return;
    const lines: string[] = [];
    const add = (label: string, value?: string) => {
        if (value) lines.push(`${label}: ${value}`);
    };
    add(t('ip.address'), e.ip);
    add(t('ip.reverse'), e.reverse);
    // The two halves the panel folds into one row are folded the same way here: an ASN
    // without its organisation, or a version without the anycast behind it, is half of
    // what the row said.
    add(t('ip.asn'), [e.asn, e.asOrg].filter(Boolean).join(' — '));
    add(t('ip.org'), e.org);
    add(t('ip.isp'), e.isp);
    if (e.cc) add(t('ip.country'), `${countryName(e.cc)} (${e.cc.toUpperCase()})`);
    add(t('ip.region'), e.region);
    add(t('ip.city'), e.city);
    add(t('ip.timezone'), e.timezone);
    if (e.lat && e.lon) add(t('ip.coords'), `${e.lat}, ${e.lon}`);
    add(t('ip.usage'), e.usage);
    add(t('ip.signals'), e.signals);
    add(t('ip.version'), [e.ipv, e.anycast].filter(Boolean).join(' · '));
    add(t('ip.assessment'), e.riskStatus);
    if (e.residential) add(t('ip.type'), e.residential === '1' ? t('ip.residential') : t('ip.datacenter'));
    if (e.purity) add(t('ip.purity'), `${e.purity}%`);
    // The report behind the band, which the panel shows folded open.
    if (voteText.value) {
        add(t('ip.consensus'), riskDetail.value?.disagreement ? `${voteText.value} · ${t('ip.disagreement')}` : voteText.value);
    }
    add(t('ip.evidence'), evidenceText.value);
    add(t('ip.sourcesState'), sourceText.value);
    add(t('ip.registry'), riskDetail.value?.registry);
    await writeClipboard(lines.join('\n'));
}

// The endpoint the guide hands out, split into the two fields a device's proxy form
// asks for. The address is the machine's own and can change under a new lease (see
// loadLan), so this reads the same ref the bar does rather than a copy taken when
// the dialog opened.
const lanServer = computed(() => {
    const i = lanIp.value.lastIndexOf(':');
    return i < 0 ? lanIp.value : lanIp.value.slice(0, i);
});
const lanPort = computed(() => {
    const i = lanIp.value.lastIndexOf(':');
    return i < 0 ? '' : lanIp.value.slice(i + 1);
});

// The guide behind a press on the endpoint. The bar has room for the address and
// nothing else, and an address alone is not the answer to "how do I use this on my
// phone": the steps are the answer, and they are what this panel holds.
//
// It lives in the same card the exit details own (see .exit-panel): the two answer
// the same question from the same corner of the window — what is at the other end of
// this connection — and a dialog with a mask would be a second shape for it.
const guideOpen = ref(false);
const guideRef = ref<HTMLDivElement | null>(null);

// The steps, as the rows the panel draws: a numeral, the platform it is for, and the
// presses themselves. The two names that are words rather than products come from the
// dictionary; "iPhone" and the rest do not need translating.
const guideSteps = computed(() => {
    void props.langTick;
    return [
        {n: '01', tag: t('lan.guide.tag.net'), text: t('lan.guide.s1')},
        {n: '02', tag: 'iPhone', text: t('lan.guide.s2')},
        {n: '03', tag: 'Android', text: t('lan.guide.s3')},
        {n: '04', tag: 'Windows', text: t('lan.guide.s4')},
        {n: '05', tag: t('lan.guide.tag.check'), text: t('lan.guide.s5')},
    ];
});

// Outside press and Escape put it away — the two gestures the exit panel answers to as
// well (App.vue owns that one's listener; this card owns itself, so it keeps its own).
// The press that opens it is exempted by its class: without that, the same press that
// closed the card would re-open it from the click that follows the mousedown.
watch(guideOpen, (open, _was, onCleanup) => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
        const target = e.target as HTMLElement | null;
        if (guideRef.value?.contains(target) || target?.closest('.lan-ip')) return;
        guideOpen.value = false;
    };
    const onKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') guideOpen.value = false;
    };
    window.addEventListener('mousedown', onDown, true);
    window.addEventListener('keydown', onKey, true);
    onCleanup(() => {
        window.removeEventListener('mousedown', onDown, true);
        window.removeEventListener('keydown', onKey, true);
    });
});

// Sharing switched off under the open guide: the endpoint it is about is gone, so the
// guide goes with it. The panel is already hidden by its own v-if, but the flag stayed
// true — and the next time sharing came on the guide opened itself, as if the reader
// had asked for it again.
watch(lanIp, v => {
    if (!v) guideOpen.value = false;
});

// The LAN endpoint's copy: the same two-step as the exit address's (see copyIp),
// with the endpoint as the text. The guide's own button is the caller now — the bar's
// press opens the steps instead, which is what that address is asked about.
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
                <!-- 头部：这条地址本身。旗、地址、复制，一行读完；ASN 与归属放在下面一行，
                     不再各占一行表格 —— 它们说的是同一个东西：这条地址属于谁。 -->
                <div class="exit-hero">
                    <img v-if="exitIp.cc && !flagBroken" class="node-flag"
                         :src="`/fa/flag/${exitIp.cc}.svg`" alt="" aria-hidden="true"
                         @error="flagBroken = true"/>
                    <div class="exit-hero-main">
                        <span class="exit-ip">{{ exitIp.ip }}</span>
                        <span v-if="exitIp.asn || exitIp.asOrg" class="exit-hero-sub">
                            <span v-if="exitIp.asn">{{ exitIp.asn }}</span>
                            <i v-if="exitIp.asn && exitIp.asOrg" class="exit-hero-dot">·</i>
                            <span v-if="exitIp.asOrg">{{ exitIp.asOrg }}</span>
                        </span>
                    </div>
                    <button class="exit-copy" :title="t('btn.copy')"
                            :aria-label="t('btn.copy')"
                            @click="copyIp">
                        <CopyFilled class="ic"/>
                    </button>
                </div>
                <!-- 数据行：左标签、右值，行间一条发丝线，指针扫过时整行亮一点。 -->
                <div class="exit-table">
                    <div v-if="exitIp.reverse" class="exit-row"><span class="exit-k">{{ t('ip.reverse') }}</span><span class="exit-v mono">{{ exitIp.reverse }}</span></div>
                    <div v-if="exitIp.isp" class="exit-row"><span class="exit-k">{{ t('ip.isp') }}</span><span class="exit-v">{{ exitIp.isp }}</span></div>
                    <div v-if="exitIp.org" class="exit-row"><span class="exit-k">{{ t('ip.org') }}</span><span class="exit-v">{{ exitIp.org }}</span></div>
                    <div v-if="exitIp.cc" class="exit-row">
                        <span class="exit-k">{{ t('ip.country') }}</span>
                        <span class="exit-v">{{ countryName(exitIp.cc) }} <em class="exit-code">{{ exitIp.cc.toUpperCase() }}</em></span>
                    </div>
                    <div v-if="exitIp.region" class="exit-row"><span class="exit-k">{{ t('ip.region') }}</span><span class="exit-v">{{ exitIp.region }}</span></div>
                    <div v-if="exitIp.city" class="exit-row"><span class="exit-k">{{ t('ip.city') }}</span><span class="exit-v">{{ exitIp.city }}</span></div>
                    <div v-if="exitIp.timezone" class="exit-row"><span class="exit-k">{{ t('ip.timezone') }}</span><span class="exit-v mono">{{ exitIp.timezone }}</span></div>
                    <div v-if="exitIp.lat && exitIp.lon" class="exit-row"><span class="exit-k">{{ t('ip.coords') }}</span><span class="exit-v mono">{{ exitIp.lat }}, {{ exitIp.lon }}</span></div>
                    <div v-if="exitIp.usage" class="exit-row"><span class="exit-k">{{ t('ip.usage') }}</span><span class="exit-v">{{ exitIp.usage }}</span></div>
                    <div v-if="exitIp.signals" class="exit-row"><span class="exit-k">{{ t('ip.signals') }}</span><span class="exit-v">{{ exitIp.signals }}</span></div>
                    <div v-if="exitIp.ipv" class="exit-row"><span class="exit-k">{{ t('ip.version') }}</span><span class="exit-v">{{ exitIp.ipv }}{{ exitIp.anycast ? ` · ${exitIp.anycast}` : '' }}</span></div>
                    <div v-if="exitIp.riskStatus" class="exit-row"><span class="exit-k">{{ t('ip.assessment') }}</span><span class="exit-v">{{ exitIp.riskStatus }}</span></div>
                    <div v-if="exitIp.residential" class="exit-row">
                        <span class="exit-k">{{ t('ip.type') }}</span>
                        <span class="exit-v">{{ exitIp.residential === '1' ? t('ip.residential') : t('ip.datacenter') }}</span>
                    </div>
                </div>
                <!-- 纯净度：整块横贯，数字在右、色带在底，读的是同一个分。 -->
                <div v-if="exitIp.purity" class="exit-purity">
                    <span class="exit-k">{{ t('ip.purity') }}</span>
                    <span class="exit-purity-val" :data-level="purityLevel(exitIp.purity)">
                        <b>{{ exitIp.purity }}%</b>
                        <span class="exit-purity-tag">{{ t(`ip.purity.${purityLevel(exitIp.purity)}`) }}</span>
                    </span>
                    <div class="purity-band" :style="{'--pos': `${exitIp.purity}%`}"/>
                </div>
                <!-- The report behind that score, folded away: the panel is a glance,
                    and the per-source detail is for the reader who doubts the number. -->
                <div v-if="riskDetail" class="exit-more">
                    <button type="button" class="exit-more-toggle"
                            :aria-expanded="riskOpen"
                            @click="riskOpen = !riskOpen">
                        {{ riskOpen ? t('ip.less') : t('ip.more') }}
                    </button>
                    <div v-if="riskOpen" class="exit-detail">
                        <!-- Each source as an ordinary label/value row, so its name sits in
                            the panel's own label column with every other label, and the bar
                            and the number are the row's value. -->
                        <template v-if="riskDetail.contributors?.length">
                            <div v-for="c in riskDetail.contributors" :key="c.source" class="exit-row">
                                <span class="exit-k">{{ c.source }}</span>
                                <span class="exit-v risk-src">
                                    <span class="risk-src-bar">
                                        <i :data-level="riskLevelOf(c.risk)"
                                           :style="{width: `${Math.max(2, Math.min(100, c.risk))}%`}"/>
                                    </span>
                                    <span class="risk-src-val">{{ Math.round(c.risk) }}</span>
                                </span>
                            </div>
                        </template>
                        <div v-if="voteText" class="exit-row">
                            <span class="exit-k">{{ t('ip.consensus') }}</span>
                            <span class="exit-v">{{ voteText }}{{ riskDetail.disagreement ? ` · ${t('ip.disagreement')}` : '' }}</span>
                        </div>
                        <div v-if="evidenceText" class="exit-row">
                            <span class="exit-k">{{ t('ip.evidence') }}</span>
                            <span class="exit-v">{{ evidenceText }}</span>
                        </div>
                        <div v-if="sourceText" class="exit-row">
                            <span class="exit-k">{{ t('ip.sourcesState') }}</span>
                            <span class="exit-v">{{ sourceText }}</span>
                        </div>
                        <div v-if="riskDetail.registry" class="exit-row">
                            <span class="exit-k">{{ t('ip.registry') }}</span>
                            <span class="exit-v">{{ riskDetail.registry }}</span>
                        </div>
                    </div>
                </div>
                <!-- The whole reading in one press, at the panel's bottom-left: the
                    address row's own copy takes the address alone. -->
                <button type="button" class="exit-copy-all"
                        :title="t('ip.copyAll')" :aria-label="t('ip.copyAll')"
                        @click="copyAll">
                    <CopyFilled class="ic"/>
                </button>
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
                    opens the steps for using it (see the guide below): the address is
                    only half of what a reader needs, and the bar has room for the half.
                    The hairline between it and the exit address is the right group's
                    own divider (see .status-right). -->
                <button v-if="lanIp" class="lan-ip mono" type="button"
                        :title="t('status.lanHint')"
                        :aria-expanded="guideOpen"
                        aria-haspopup="dialog"
                        @click="guideOpen = !guideOpen">
                    {{ lanIp }}
                </button>
                <!-- The start-up TUN prewarm's receipt, at the bar's far edge: a green
                    tick says the resident server is up, so the first TUN switch-on of the
                    session opens a device instead of waiting out a cold task start. -->
                <span v-if="tunPrewarmOK" class="tun-prewarm-ok"
                      :title="t('status.tunPrewarm')" role="img" aria-label="TUN">
                    <CheckCircleFilled class="ic"/>
                </span>
            </div>
        </div>

        <!-- The LAN guide, in the same card the exit details use (see .exit-panel):
            a spec sheet whose subject is the endpoint, then one row per press the
            other device needs — numeral, platform, sentence. Not a dialog: nothing
            here is modal, and the reader is holding a phone rather than tabbing
            through a form. -->
        <div v-if="guideOpen && lanIp" class="exit-panel lan-panel" ref="guideRef"
             role="dialog" :aria-label="t('lan.guide.title')">
            <div class="exit-hero">
                <!-- Where the exit panel carries the country's flag: what kind of thing
                     this is. The same slot, so the two cards open the same way. -->
                <span class="lan-hero-chip">LAN</span>
                <div class="exit-hero-main">
                    <span class="exit-ip">{{ lanServer }}<i class="lan-hero-port">:{{ lanPort }}</i></span>
                    <span class="exit-hero-sub">{{ t('lan.guide.sub') }}</span>
                </div>
                <button class="exit-copy" :title="t('lan.guide.copy')"
                        :aria-label="t('lan.guide.copy')"
                        @click="copyLan">
                    <CopyFilled class="ic"/>
                </button>
            </div>
            <div class="lan-steps">
                <div v-for="s in guideSteps" :key="s.n" class="lan-step">
                    <span class="lan-step-n">{{ s.n }}</span>
                    <span class="lan-step-tag">{{ s.tag }}</span>
                    <span class="lan-step-text">{{ s.text }}</span>
                </div>
            </div>
            <p class="lan-note">{{ t('lan.guide.note') }}</p>
        </div>
    </div>
</template>
