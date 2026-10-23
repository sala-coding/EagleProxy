<script setup lang="ts">
import {computed, ref} from 'vue';
import {t} from '../../i18n';
import {DIRECT_DNS_PRESETS, DNS_OBJECT_TEMPLATE, DNS_SERVER_TYPES, type DnsServerRow, dnsRowToServer, dnsServerToRow, dnsTypeResolvesItself, dnsTypeUsesPath, pickFastestDNS, REMOTE_DNS_PRESETS, roundChoice, SMART_PICK_MINUTES, type PickWin, type Side} from '../../lib/dns';
import EitherSwitch from '../EitherSwitch.vue';
import ResolverTable from '../ResolverTable.vue';
import {AddFilled, Delete2Filled} from '../../lib/icons';

// The DNS card's body: the two resolvers with their catalogues, the custom-DNS
// panel over them, and the switches frame. The slide div and the card's own
// title stay with SettingsDialog, which owns the profile.
//
// React -> Vue mapping：editRoute/editRouteFlag 改为 'edit'/'editFlag' 事件；
// notify 改为 'notify' 事件；onDnsLatency 因是函数式更新（往 App 已有的 map
// 里合并）而保留为函数 prop，此处改名 publishDnsLatency 以避开 Vue 对 onXxx
// 属性的监听器语义。smartPick 与 dnsTesting 只被本卡使用，随卡搬入。
const props = defineProps<{
    form: any;
    // What the last round measured (the app owns the map; see SettingsDialog).
    latency: Record<string, number>;
    // Whether a node is up right now. The DNS card reads it to say why the foreign half of
    // its table is empty: that half is measured through the tunnel (see App.TestDNS), so
    // until something is running there is nothing for it to be measured through.
    nodeOn: boolean;
    // The interface size, passed through to the two tables so a measurement
    // re-taken at a new type scale re-pages them.
    scale: number;
    // The way to publish a round made from in here (was onDnsLatency): a setter
    // rather than a plain callback — a round publishes into the map that is already
    // there, and the two tables' buttons can be pressed one after the other without the
    // second round's reading of it overwriting the first's numbers.
    publishDnsLatency: (set: (prev: Record<string, number>) => Record<string, number>) => void;
    // Bumped on a language change (see langTick in SettingsDialog): receiving a
    // new value re-renders the card, so its t() words are re-spoken.
    langTick?: number;
}>();

const emit = defineEmits<{
    edit: [k: string, v: string | number];
    editFlag: [k: string, v: boolean];
    notify: [t: string, k?: 'ok' | 'err'];
}>();

// The smart pick, while it runs. A test asks every candidate on both sides — three
// times each — so it takes a second or two rather than being instant, and the card has
// to say so while it does. The ref is the guard rather than the state: two switches
// pressed in the same tick would both read a state that has not been set yet, and two
// tests at once would each write the fields the other was about to.
// Which list is being measured right now, and the ref that guards it. Per side rather
// than one flag, because each table carries a button of its own and the two rounds are
// not the same one: the foreign one can only be made through a running node. The ref is
// the guard rather than the state — two presses in the same tick would both read a state
// that has not been set yet.
const dnsTesting = ref<Record<Side, boolean>>({remote: false, direct: false});
const dnsTestingRef: Record<Side, boolean> = {remote: false, direct: false};

// The DNS object is the one field whose failure is silent: the builder drops
// a value it cannot parse and runs on the assembled defaults, so a stray
// comma reads as "my DNS settings stopped working". Parse it as it is typed
// and say so here, and keep a formatter beside it, because an object this
// size pasted in one line is unreadable.
const dnsObjectProblem = computed<{kind: 'shape' | 'missing' | ''; text: string}>(() => {
    const v = String(props.form?.dns_object ?? '').trim();
    if (!props.form?.use_dns_object || v === '') return {kind: '', text: ''};
    try {
        const obj = JSON.parse(v);
        if (!obj || typeof obj !== 'object' || Array.isArray(obj)) {
            return {kind: 'shape', text: t('routing.dnsObjNotObject')};
        }
        // The one thing about this section that can be checked without reading sing-box's
        // schema: the route's own fallback resolver points at a server tagged dns-direct
        // (see buildRoute in the Go side), and that reference is not part of what this box
        // replaces. A section without that tag is one the core refuses to start on, and it
        // refuses with a line about an unknown server rather than about the box.
        const tags = Array.isArray(obj.servers) ? obj.servers.map((s: any) => s?.tag) : [];
        if (!tags.includes('dns-direct')) return {kind: 'missing', text: t('dns.objNoDirect')};
        return {kind: '', text: ''};
    } catch (e: any) {
        return {kind: 'shape', text: `${t('routing.dnsObjBad')} (${String(e?.message ?? e)})`};
    }
});

// The text the warn line shows. Split from the problem above because the add
// button's seat is decided by the KIND: a section the JSON parser cannot read
// has nothing a new row can fix, while the missing dns-direct tag is exactly
// the thing a new row fixes — see addDnsServer.
const dnsObjectError = computed(() => dnsObjectProblem.value.text);
const dnsObjectShapeError = computed(() => dnsObjectProblem.value.kind === 'shape');

// Whether the foreign half of the table has been measured at all. It is the half that is
// measured through the tunnel, so an empty column there is a state to explain rather than
// a failure to leave looking like one — and the explanation is different depending on
// whether there is anything to measure through.
const remoteUnmeasured = computed(
    () => REMOTE_DNS_PRESETS.every(p => !(props.latency[p.value] > 0)));

// Whether the app is set to choose the resolvers at all, or only to keep an eye on the
// ones in the fields being good enough. Either way the two tables get their buttons —
// measuring is what both switches are built on.
const smartOn = computed(() => !!(props.form?.dns_smart_select || props.form?.dns_smart_switch));

// What a row's menu writes: either field, whichever list the row was clicked in. The
// mapping is the card's own — 国外 DNS is what a proxied name is resolved through and
// 国内 DNS what the lists send out unproxied — and both tables get the same one, because
// the two fields are two places a resolver can go rather than two catalogues.
const assignDns = (side: Side, v: string) =>
    emit('edit', side === 'remote' ? 'remote_dns' : 'direct_dns', v);

// The box's servers as the visual editor holds them — empty when the box is empty or is
// not JSON, which the line above is already saying.
const dnsServers = computed<DnsServerRow[]>(() => {
    const v = String(props.form?.dns_object ?? '').trim();
    if (v === '') return [];
    try {
        const obj = JSON.parse(v);
        return Array.isArray(obj?.servers) ? obj.servers.map(dnsServerToRow) : [];
    } catch {
        return [];
    }
});

// Writing them back. The rest of the document is taken exactly as it is and only `servers`
// is replaced, so a hand-written rule, a key this side has never heard of, or a field a row
// does not offer is not something editing another row can destroy.
const setDnsServers = (rows: DnsServerRow[]) => {
    const v = String(props.form?.dns_object ?? '').trim();
    let doc: any;
    try {
        doc = v === '' ? {} : JSON.parse(v);
    } catch {
        return;   // not JSON: nothing here is editable, and the line above says why
    }
    if (doc === null || typeof doc !== 'object' || Array.isArray(doc)) return;
    doc.servers = rows.map(dnsRowToServer);
    emit('edit', 'dns_object', JSON.stringify(doc, null, 2));
};

const patchDnsServer = (index: number, patch: Partial<DnsServerRow>) =>
    setDnsServers(dnsServers.value.map((row, i) => (i === index ? {...row, ...patch} : row)));

// A new row starts as the simplest server there is — plain UDP with nothing filled in —
// because a row is also a statement about the type, and the type decides which of its
// fields exist at all.
//
// When the box's own warning is the missing dns-direct tag, the row starts as the
// row that warning is about: the tag the route's fallback resolver points at. The
// one press that is the obvious answer to the warning is then the one that answers
// it; the address is still the reader's to fill in.
const addDnsServer = () => setDnsServers([...dnsServers.value, {
    raw: {},
    tag: dnsObjectProblem.value.kind === 'missing' ? 'dns-direct' : '',
    type: 'udp', addr: '', path: '', detour: '', resolver: '',
}]);

const removeDnsServer = (index: number) =>
    setDnsServers(dnsServers.value.filter((_, i) => i !== index));

const formatDnsObject = () => {
    const v = String(props.form?.dns_object ?? '').trim();
    if (v === '') return;
    try {
        emit('edit', 'dns_object', JSON.stringify(JSON.parse(v), null, 2));
    } catch { /* the error line beside the button already says why */ }
};

// The two buttons beside that box are a pair over its two states: 模板 applies to an empty
// one, 格式化 to one with something in it, and neither can be pressed while the other
// could. Which is also what keeps the template from overwriting a section somebody has
// already written — there is no undo here, and a mistake in this box is a core that does
// not start.
const fillDnsTemplate = () => emit('edit', 'dns_object', DNS_OBJECT_TEMPLATE);

// A round made now rather than on the timer (see App). `sides` is what the caller is
// asking about: each table's button asks for its own list, and a switch asks for both —
// which is the whole card, and what the timer does on its own schedule.
//
// Only the winners are written, so a side that answered nothing leaves its field as it
// was — the reader's own choice, not an empty string.
const smartPick = async (sides: Side[], justOn?: 'select' | 'switch') => {
    // One round per side at a time: a second press while that list is being measured
    // would start a second round over the same resolvers.
    if (sides.some(s => dnsTestingRef[s])) return;
    const mark = (busy: boolean) => {
        sides.forEach(s => { dnsTestingRef[s] = busy; });
        const next = {...dnsTesting.value};
        sides.forEach(s => { next[s] = busy; });
        dnsTesting.value = next;
    };
    mark(true);
    try {
        const round = await pickFastestDNS(
            {remote: props.form.remote_dns, direct: props.form.direct_dns}, sides);
        // Published before the fields are written: the ranking is the round's result
        // whether or not either resolver moved. Merged rather than replaced — a round over
        // one list knows nothing about the other's numbers and must not clear them — and
        // merged through the setter, so a round finishing while another is in flight does
        // not put back the map it was handed.
        props.publishDnsLatency(prev => ({...prev, ...round.latency}));
        // The same rule the timer follows (see roundChoice), so that pressing the button
        // does what waiting for the interval would have done — including doing nothing,
        // when 切换 is on and the resolver in the field is still quick enough.
        //
        // The switch that has just been turned on is passed in rather than read from the
        // form: setState has not come back round yet, and a switch read as off would make
        // the press that turned it on do nothing at all.
        const flags = justOn === 'select' ? {...props.form, dns_smart_select: true}
            : justOn === 'switch' ? {...props.form, dns_smart_switch: true}
                : props.form;
        const won: PickWin[] = [];
        for (const side of sides) {
            const choice = roundChoice(side, flags, round);
            if (!choice) continue;
            emit('edit', side === 'remote' ? 'remote_dns' : 'direct_dns', choice.value);
            won.push(choice);
        }
        const measured = sides
            .map(s => (s === 'remote' ? round.remote : round.direct))
            .find(w => w !== null);
        emit('notify', won.length > 0
                       ? t('dns.smartPicked') + ' ' + won.map(p => `${p.name} ${p.ms}ms`).join(' · ')
                       : measured ? t('dns.smartKept') : t('dns.smartNone'),
                   won.length > 0 || measured ? 'ok' : 'err');
        // Why the foreign side came back with nothing, when it is not simply a slow
        // resolver: the usual reason is that there is no node to test it through.
        if (round.note) emit('notify', round.note, 'err');
    } finally {
        mark(false);
    }
};

// Turning either switch on is a round over both lists. 优选 has to make its choice;
// 切换 has to look at where each field stands before it can decide to leave it alone —
// and either way, a switch that only recorded an intention would leave the reader to go
// and find the buttons that do it.
const onSmartToggle = (key: 'select' | 'switch', on: boolean) => {
    emit('editFlag', key === 'select' ? 'dns_smart_select' : 'dns_smart_switch', on);
    if (on) void smartPick(['remote', 'direct'], key);
};
</script>

<template>
    <div class="rt-body">
        <!-- The card's rows stand in the two kind frames every card of the
            panel gathers its rows into — what the reader fills in in one,
            the switches in the other — and the rows of this card are the
            two answers a name gets, the catalogues they are chosen from,
            and the switches that decide how it is resolved.
            The answers and their catalogues go in the fields' frame whole:
            a resolver with the list it is chosen from is one column rather
            than a row of fields over a row of lists — which list a row
            writes to is then the column it is in, and needs no heading to
            say so — and the custom DNS panel stands over those two columns
            and stays with them, since what it replaces is the pair of
            answers (see .dns-panel). 兜底解析 and 间隔 stand at the frame's
            foot, under them: the two were cells of the card's three-column
            board, and what they are is the policy over the pair of answers
            above — worth having in view while the object that replaces both
            answers is written, which is what the panel's own comment is
            about. -->
        <div class="kind-box kind-text">
            <span class="look-box-title">{{ t('kind.dns.text') }}</span>
            <div class="rt-dns">
                <!-- The two resolvers: the one a proxied name is resolved
                    through, and the one for everything the lists send out
                    unproxied. A cell each because they are read as a pair —
                    a resolver set in one cell and not the other is half a
                    setting. Side by side on a card with the width for it,
                    one above the other on a dock that has not (see .rt-dns
                    and .settings-dock .rt-dns). -->
                <div class="dns-col">
                    <div class="rt-cell">
                        <!-- The list's name and the one thing there is to do to
                            it, on one line (see .rt-cell-head): 立即测速 is
                            about the list rather than about any one row, and
                            the caption under it is the ruler its rows are
                            measured against — a button standing in there
                            would sit over the column it names (see
                            .dns-table-head). -->
                        <span class="rt-cell-head">
                            <label :title="t('cfg.remote_dns')">{{ t('routing.remoteDNS') }}</label>
                            <!-- Offered only while there is something to measure
                                through: this list is reached over the running
                                node, so without one the button could only ever
                                report that it has no path. The note under the
                                table is what says so instead, and it is already
                                there whenever this half has no numbers. -->
                            <button v-if="smartOn && nodeOn" class="btn tiny" :disabled="dnsTesting.remote"
                                    @click="smartPick(['remote'])">
                                {{ t(dnsTesting.remote ? 'dns.smartTesting' : 'dns.smartTest') }}
                            </button>
                        </span>
                        <input class="rt-input" :value="form.remote_dns ?? ''"
                               :spellcheck="false"
                               placeholder="https://dns.google/dns-query"
                               @input="emit('edit', 'remote_dns', ($event.target as HTMLInputElement).value)"/>
                    </div>
                    <ResolverTable :value="form.remote_dns ?? ''"
                                   :presets="REMOTE_DNS_PRESETS"
                                   :latency="latency"
                                   :offers="['remote', 'direct']"
                                   :scale="scale"
                                   @assign="assignDns"
                                   @pick="v => emit('edit', 'remote_dns', v)"/>
                    <!-- Why this half of the card has no numbers in it. The
                        domestic list is measured straight out and fills in with
                        or without a node; this one is measured through the
                        tunnel (see App.TestDNS), so until something is running
                        there is nothing for it to be measured through — and
                        what a node does give it is a property of that node as
                        much as of the resolver, which is why no fallback to a
                        direct measurement is offered here. -->
                    <div v-if="remoteUnmeasured" class="rt-note">
                        {{ t(nodeOn ? 'dns.remoteNotYet' : 'dns.remoteNeedsNode') }}
                    </div>
                </div>
                <div class="dns-col">
                    <div class="rt-cell">
                        <span class="rt-cell-head">
                            <label :title="t('cfg.direct_dns')">{{ t('routing.directDNS') }}</label>
                            <!-- This list's own 立即测速, over its own path:
                                straight out, which is why this button works
                                with or without a node running. -->
                            <button v-if="smartOn" class="btn tiny" :disabled="dnsTesting.direct"
                                    @click="smartPick(['direct'])">
                                {{ t(dnsTesting.direct ? 'dns.smartTesting' : 'dns.smartTest') }}
                            </button>
                        </span>
                        <input class="rt-input" :value="form.direct_dns ?? ''"
                               :spellcheck="false"
                               placeholder="https://doh.pub/dns-query"
                               @input="emit('edit', 'direct_dns', ($event.target as HTMLInputElement).value)"/>
                    </div>
                    <ResolverTable :value="form.direct_dns ?? ''"
                                   :presets="DIRECT_DNS_PRESETS"
                                   :latency="latency"
                                   :offers="['direct']"
                                   :scale="scale"
                                   @assign="assignDns"
                                   @pick="v => emit('edit', 'direct_dns', v)"/>
                    <!-- Only one field: a resolver inside the country
                        has nothing to do as the foreign answer,
                        which is reached through the tunnel. -->
                </div>
                <!-- The custom DNS section, as a panel over the two tables rather
                    than a block under them.

                    It covers their two columns and nothing else: the grid area of
                    an absolutely-positioned grid child is its containing block,
                    so the panel begins at the first table's own left edge, and
                    the column on the left stays where it can be read — 兜底解析
                    is what says which field each of the two answers is for, and
                    that is the one thing worth having in view while writing the
                    object that replaces both of them.

                    Off, it is slid out past the card's right edge and the grid
                    does not let it be seen on the way; the transition is what
                    makes it arrive rather than appear. -->
                <div :class="`dns-panel${form.use_dns_object ? ' open' : ''}`">
                    <div class="rt-row">
                        <button class="btn tiny" @click="fillDnsTemplate"
                                :disabled="!!String(form.dns_object ?? '').trim()"
                                :title="t('dns.templateHint')">
                            {{ t('dns.template') }}
                        </button>
                        <button class="btn tiny" @click="formatDnsObject"
                                :disabled="!String(form.dns_object ?? '').trim()">
                            {{ t('btn.format') }}
                        </button>
                        <span v-if="dnsObjectError" class="rt-warn">{{ dnsObjectError }}</span>
                    </div>
                    <!-- The servers as rows, above the text rather than beside
                        it: this is the part of the section that can be read
                        without knowing sing-box's vocabulary, and the box
                        below is what remains for the rest of it. -->
                    <div class="dns-obj">
                        <div class="dns-obj-head">
                            <span>{{ t('dns.objServers') }}</span>
                            <button class="btn tiny" @click="addDnsServer"
                                    :disabled="dnsObjectShapeError">
                                <AddFilled class="ic"/>{{ t('dns.objAdd') }}
                            </button>
                        </div>
                        <div v-for="(row, i) in dnsServers" :key="i" class="dns-obj-row">
                            <input class="dns-obj-in obj-tag" :value="row.tag"
                                   placeholder="tag" :title="t('dns.objTag')"
                                   :spellcheck="false"
                                   @input="patchDnsServer(i, {tag: ($event.target as HTMLInputElement).value})"/>
                            <select class="dns-obj-in obj-type" :value="row.type"
                                    :title="t('dns.objType')"
                                    @change="patchDnsServer(i, {type: ($event.target as HTMLSelectElement).value})">
                                <option v-for="v in DNS_SERVER_TYPES" :key="v" :value="v">{{ v }}</option>
                            </select>
                            <!-- The three that take an address at all: a
                                local or fakeip server answers by itself,
                                and offering it an empty address field
                                would be asking for something to fill in. -->
                            <template v-if="!dnsTypeResolvesItself(row.type)">
                                <input class="dns-obj-in obj-addr" :value="row.addr"
                                       placeholder="8.8.8.8:53" :title="t('dns.objAddr')"
                                       :spellcheck="false"
                                       @input="patchDnsServer(i, {addr: ($event.target as HTMLInputElement).value})"/>
                                <input v-if="dnsTypeUsesPath(row.type)" class="dns-obj-in obj-path" :value="row.path"
                                       placeholder="/dns-query" :title="t('dns.objPath')"
                                       :spellcheck="false"
                                       @input="patchDnsServer(i, {path: ($event.target as HTMLInputElement).value})"/>
                                <input class="dns-obj-in obj-detour" :value="row.detour"
                                       placeholder="proxy" :title="t('dns.objDetour')"
                                       :spellcheck="false"
                                       @input="patchDnsServer(i, {detour: ($event.target as HTMLInputElement).value})"/>
                                <input class="dns-obj-in obj-resolver" :value="row.resolver"
                                       placeholder="dns-local" :title="t('dns.objResolver')"
                                       :spellcheck="false"
                                       @input="patchDnsServer(i, {resolver: ($event.target as HTMLInputElement).value})"/>
                            </template>
                            <button class="dns-obj-del" :title="t('btn.delete')"
                                    :aria-label="t('btn.delete')"
                                    @click="removeDnsServer(i)">
                                <Delete2Filled class="ic"/>
                            </button>
                        </div>
                    </div>
                    <textarea class="rawjson"
                              rows="6"
                              :spellcheck="false"
                              placeholder='{"servers": [], "rules": []}'
                              :value="form.dns_object ?? ''"
                              @input="emit('edit', 'dns_object', ($event.target as HTMLTextAreaElement).value)"/>
                </div>
            </div>
            <!-- 兜底解析 and 间隔, at the foot of the frame: the two answers and
                the two catalogues they are chosen from are what the card is
                about, and these two are the policy over the pair of them —
                which answer a name falls back to, and how often the lists
                are re-measured. As the frame's last rows they read as that
                (and the frame's own slack, on a card with room to spare,
                goes to the catalogues above them: see
                .rt-body > .kind-box.kind-text > .rt-dns).

                Side by side rather than one above the other: both are a
                name and a single value, neither of which needs the row to
                itself the way the catalogues do, so a second line each is
                two lines spent on nothing. The frame's own slack is not
                spent on them, so the grid above keeps its height. -->
            <div class="rt-tail">
                <div class="jf-field">
                    <label>{{ t('routing.dnsFinalOut') }}</label>
                    <EitherSwitch :label="t('routing.dnsFinalOut')"
                                  :left="t('egress.direct')" :right="t('egress.global')"
                                  left-value="bypass" right-value="proxy"
                                  :model-value="form.dns_final_out || 'proxy'"
                                  @update:model-value="v => emit('edit', 'dns_final_out', v)"/>
                </div>
                <!-- The interval 智能优选 runs on: a number the reader sets, and
                    this is where the card's numbers stand. How often, in
                    minutes, and it governs both policies: what is worth watching
                    for — a link that got slower, a resolver that moved — keeps
                    no schedule this side could guess at for the reader.
                    立即测速 is not here: each list has one on its own title line
                    (see .rt-cell-head), measuring that list over its own path. -->
                <div v-if="smartOn" class="jf-field">
                    <label :title="t('dns.smartHint')">{{ t('dns.smartEvery') }}</label>
                    <div class="rt-every">
                        <input type="number" min="1" max="1440" step="1"
                               :value="form.dns_smart_minutes || SMART_PICK_MINUTES"
                               @input="emit('edit', 'dns_smart_minutes', Number(($event.target as HTMLInputElement).value) || 0)"/>
                        <span>{{ t('dns.smartUnit') }}</span>
                    </div>
                </div>
            </div>
        </div>
        <!-- The switches of this card, in the switches' frame — the other half
            of the pair the fields' frame above is (see .kind-box). They used
            to be a stack in the board's first column, one control to a line
            and all of them starting at the same edge: three rows of unequal
            length — a pair of capsules with a switch riding at their end, two
            switches, then an interval with a switch after it — put every pill
            at a different x and read as three unrelated settings rather than
            as the list of ways a name gets resolved. As rows of a frame they
            are even by construction: one name in the label column, one switch
            beside it. The interval 智能优选 runs on is in the fields' frame
            instead, with the card's other numbers, since a number is a value
            the reader sets. -->
        <div class="kind-box kind-switch">
            <span class="look-box-title">{{ t('kind.dns.switch') }}</span>
            <!-- 解析域名: off, everything is resolved locally and the domain
                lists can only match against what the local resolver was
                willing to answer. The note stands under the row it is about,
                where the switch's own off state is what is being read. -->
            <div class="jf-field">
                <label :title="t('routing.dnsRoutingHint')">{{ t('routing.dnsRouting') }}</label>
                <label class="jf-check">
                    <input type="checkbox" :checked="!!form.dns_routing"
                           @change="emit('editFlag', 'dns_routing', ($event.target as HTMLInputElement).checked)"/>
                </label>
            </div>
            <div class="jf-field">
                <label :title="t('dns.smartHint')">{{ t('dns.smart') }}</label>
                <label class="jf-check">
                    <input type="checkbox" :checked="!!form.dns_smart_select"
                           @change="onSmartToggle('select', ($event.target as HTMLInputElement).checked)"/>
                </label>
            </div>
            <!-- 智能切换, right under 智能优选: the other policy for the same
                rounds, and the two are read as a pair. -->
            <div class="jf-field">
                <label :title="t('dns.switchHint')">{{ t('dns.switch') }}</label>
                <label class="jf-check">
                    <input type="checkbox" :checked="!!form.dns_smart_switch"
                           @change="onSmartToggle('switch', ($event.target as HTMLInputElement).checked)"/>
                </label>
            </div>
            <!-- And the last resort, at the foot of the list: what it turns
                on is the panel over the two catalogues, which is where the two
                answers above stop being the answers. -->
            <div class="jf-field">
                <label :title="t('routing.dnsObjectHint')">{{ t('routing.useDNSObject') }}</label>
                <label class="jf-check">
                    <input type="checkbox" :checked="!!form.use_dns_object"
                           @change="emit('editFlag', 'use_dns_object', ($event.target as HTMLInputElement).checked)"/>
                </label>
            </div>
        </div>
        <!-- No button: this card writes itself (see the debounced effect in
            this component). Nothing stands here to say so any more — the
            note that used to name it took as much of the card's foot as
            the setting above it needed, and the reader who changes a
            resolver and sees nothing happen is served better by the window
            changing than by a line of text about it. -->
    </div>
</template>
