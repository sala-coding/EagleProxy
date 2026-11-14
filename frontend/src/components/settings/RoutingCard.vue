<script setup lang="ts">
import {computed} from 'vue';
import {t} from '../../i18n';
import {ROUTE_CARDS, SNIFF_MODES, STRATEGY_OPTIONS} from '../../lib/dns';
import {EarthFilled, ForbidCircleFilled, TransferHorizontalFilled, WifiFilled} from '../../lib/icons';
import SegmentedSwitch from '../SegmentedSwitch.vue';
import EitherSwitch from '../EitherSwitch.vue';
import RouteLists from '../RouteLists.vue';

// The routing card: the four cards, the three profile-wide fields, and the
// per-card bodies under them. The slide div and the card's own title stay with
// SettingsDialog, which owns the carousel and the profile itself.
//
// React -> Vue mapping：form 由 prop 传入（为 null 时画占位行，与原 JSX 的
// 三元一致）；editRoute/appendRoute 改为 'edit' 与 'append' 事件；四张卡的
// 按压合并为 'card' 事件（block 卡不切模式，见下）。routeWarnings 是纯派生，
// 随组件搬进来成为 computed。
const props = defineProps<{
    form: any;
    // Which card's settings are on screen. Deliberately not the egress mode: the block
    // card is a layer rather than a direction, so it has settings but switches nothing,
    // and keeping the two apart is what stops "open the block card" from meaning
    // "change how traffic flows".
    focus: string;
    egress: string;
    // Bumped on a language change (see langTick in SettingsDialog): receiving a
    // new value re-renders the card, so its t() words are re-spoken; read here
    // so routeWarnings' cache busts with it too.
    langTick?: number;
}>();

const emit = defineEmits<{
    edit: [k: string, v: string | number];
    append: [k: string, v: string];
    card: [m: string];
}>();

// What the lists as typed will actually do, said before the core is asked to
// run them. These are the mistakes a plain text box cannot catch on its own:
// a full-width comma looks exactly like a separator until a rule silently
// fails to match, and an item claimed by both the proxy and the direct list
// is resolved only by the order the builder happens to write the rules in
// (proxy ahead of direct — see addRuleRoute), which no amount of staring at
// the two textareas will reveal.
const routeWarnings = computed<string[]>(() => {
    void props.langTick;
    if (!props.form) return [];
    const out: string[] = [];
    const read = (k: string) =>
        String(props.form[k] ?? '').split('\n').map(s => s.trim()).filter(s => s !== '');
    const names: Record<string, string> = {
        direct_domain: t('routing.directDomain'),
        proxy_domain: t('routing.proxyDomain'),
        block_domain: t('routing.blockDomain'),
        direct_ip: t('routing.directIP'),
        proxy_ip: t('routing.proxyIP'),
        block_ip: t('routing.blockIP'),
    };
    const lists: Record<string, string[]> = {};
    for (const k of Object.keys(names)) lists[k] = read(k);
    for (const [k, lines] of Object.entries(lists)) {
        lines.forEach((line, i) => {
            const at = `${names[k]} #${i + 1} 「${line}」`;
            if (/[，。；：、（）【】""''？！]/.test(line)) out.push(`${at} ${t('warn.fullwidth')}`);
            else if (line.includes(',')) out.push(`${at} ${t('warn.comma')}`);
            else if (/\s/.test(line)) out.push(`${at} ${t('warn.space')}`);
        });
    }
    const claims: Record<string, string> = {};
    const claim = (k: string, kind: string) => {
        for (const line of lists[k]) {
            const key = line.toLowerCase();
            if (claims[key] && claims[key] !== kind) out.push(`「${line}」${t('warn.both')}`);
            else claims[key] = kind;
        }
    };
    claim('proxy_domain', 'proxy');
    claim('direct_domain', 'direct');
    claim('proxy_ip', 'proxy');
    claim('direct_ip', 'direct');
    return [...new Set(out)];
});

const routeCount = (keys: string[]) => computed(() =>
    keys.reduce((total, key) => total + String(props.form?.[key] ?? '').split('\n').filter(line => line.trim()).length, 0),
);

const directCount = routeCount(['direct_domain', 'direct_ip']);
const proxyCount = routeCount(['proxy_domain', 'proxy_ip']);
const blockCount = routeCount(['block_domain', 'block_ip']);

const onEdit = (k: string, v: string) => emit('edit', k, v);
const onAppend = (k: string, v: string) => emit('append', k, v);
</script>

<template>
    <!-- Outbound mode as one control rail: the three directions are a
        segmented switch wearing the mode's colour, the blocking layer a chip
        beside it, and what the choice means is the line under them. The card
        used to open with a hero — a big title, a live badge saying the same
        word twice, and three stacked cards — which spent the whole first
        screen on picking before anything could be edited. -->
    <section class="rt-rail" :aria-label="t('egress.title')">
        <div class="rt-rail-head">
            <span class="rt-rail-label">{{ t('egress.title') }}</span>
            <!-- Blocking is not a fourth direction: it is a layer over all
                three, so it sits out of the switch as a chip that opens its
                lists without changing where traffic goes. -->
            <button type="button" :class="`rt-block-chip${focus === 'block' ? ' on' : ''}`"
                    :aria-pressed="focus === 'block'" :title="t('routing.blockHint')"
                    @click="emit('card', 'block')">
                <ForbidCircleFilled class="ic"/>{{ t('routing.card.blockName') }}
            </button>
        </div>
        <div class="rt-modes" role="radiogroup" :aria-label="t('egress.title')">
            <button v-for="m in ROUTE_CARDS" :key="m" type="button" role="radio"
                    :class="`rt-mode rt-mode-${m}${egress === m ? ' current' : ''}${focus === m ? ' active' : ''}`"
                    :aria-checked="egress === m"
                    @click="emit('card', m)">
                <TransferHorizontalFilled v-if="m === 'rule'" class="ic"/>
                <WifiFilled v-else-if="m === 'global'" class="ic"/>
                <EarthFilled v-else class="ic"/>
                <span>{{ t(`egress.${m}`) }}</span>
            </button>
        </div>
        <p class="rt-rail-note">{{ t(`routing.modeHint.${egress}`) }}</p>
    </section>

    <div v-if="!form" class="hint">加载中…</div>
    <div v-else class="rt-body">
        <!-- The profile-wide answers in one frame of label→control rows:
            sniffing turns a connection into a domain, the two strategies
            turn domains into IPs, the two DNS sides' strategies do the same
            on each resolution path, and — in rule mode only — the fallback
            catches what no list claimed. The builder writes all of them
            whatever the mode is (see noteSniffing and addRuleDNS on the Go
            side); the five-option strategies are a select's work, not five
            segments stretched across a narrow card. -->
        <section class="rt-fields">
            <div class="rt-row">
                <label :title="t('routing.sniffHint')">{{ t('routing.sniffing') }}</label>
                <SegmentedSwitch :label="t('routing.sniffing')"
                                 :options="SNIFF_MODES"
                                 :model-value="String(form.sniffing_mode ?? 1)"
                                 @update:model-value="v => emit('edit', 'sniffing_mode', Number(v))"/>
            </div>
            <div class="rt-row">
                <label :title="t('routing.domainStrategyHint')">{{ t('routing.domainStrategy') }}</label>
                <select class="rt-input" :value="form.domain_strategy ?? ''"
                        :aria-label="t('routing.domainStrategy')"
                        @change="emit('edit', 'domain_strategy', ($event.target as HTMLSelectElement).value)">
                    <option v-for="o in STRATEGY_OPTIONS" :key="o.value" :value="o.value">{{ t(o.label) }}</option>
                </select>
            </div>
            <div class="rt-row">
                <label :title="t('routing.outboundStrategyHint')">{{ t('routing.outboundDomainStrategy') }}</label>
                <select class="rt-input" :value="form.outbound_domain_strategy ?? ''"
                        :aria-label="t('routing.outboundDomainStrategy')"
                        @change="emit('edit', 'outbound_domain_strategy', ($event.target as HTMLSelectElement).value)">
                    <option v-for="o in STRATEGY_OPTIONS" :key="o.value" :value="o.value">{{ t(o.label) }}</option>
                </select>
            </div>
            <div class="rt-row">
                <label :title="t('cfg.remote_dns_strategy')">{{ t('cfg.remote_dns_strategy') }}</label>
                <select class="rt-input" :value="form.remote_dns_strategy ?? ''"
                        :aria-label="t('cfg.remote_dns_strategy')"
                        @change="emit('edit', 'remote_dns_strategy', ($event.target as HTMLSelectElement).value)">
                    <option v-for="o in STRATEGY_OPTIONS" :key="o.value" :value="o.value">{{ t(o.label) }}</option>
                </select>
            </div>
            <div class="rt-row">
                <label :title="t('cfg.direct_dns_strategy')">{{ t('cfg.direct_dns_strategy') }}</label>
                <select class="rt-input" :value="form.direct_dns_strategy ?? ''"
                        :aria-label="t('cfg.direct_dns_strategy')"
                        @change="emit('edit', 'direct_dns_strategy', ($event.target as HTMLSelectElement).value)">
                    <option v-for="o in STRATEGY_OPTIONS" :key="o.value" :value="o.value">{{ t(o.label) }}</option>
                </select>
            </div>
            <div v-if="focus === 'rule'" class="rt-row">
                <label>{{ t('routing.defOutbound') }}</label>
                <EitherSwitch :label="t('routing.defOutbound')"
                              :left="t('egress.direct')" :right="t('egress.global')"
                              left-value="bypass" right-value="proxy"
                              :model-value="form.def_outbound || 'proxy'"
                              @update:model-value="v => emit('edit', 'def_outbound', v)"/>
            </div>
        </section>
        <template v-if="focus === 'block'">
            <!-- The one thing the blocking lists are not obvious about: they refuse
                whatever the chosen direction is. Said here, over the lists it names,
                rather than up in the rail that is about the directions themselves. -->
            <p class="rt-note rt-block-note">{{ t('routing.blockHint') }}</p>
            <section class="rt-rule-panel rt-rule-block">
                <header class="rt-rule-head">
                    <span class="rt-rule-destination"><ForbidCircleFilled class="ic"/><h3>{{ t('routing.card.blockName') }}</h3></span>
                    <span class="rt-rule-count">{{ blockCount }}</span>
                </header>
                <RouteLists :form="form" :keys="['block_domain', 'block_ip']"
                            @edit="onEdit" @preset="onAppend"/>
            </section>
        </template>
        <template v-else-if="focus === 'rule'">
            <div class="rt-route-matrix">
                <section class="rt-rule-panel rt-rule-direct">
                    <header class="rt-rule-head">
                        <span class="rt-rule-destination"><EarthFilled class="ic"/><h3>{{ t('egress.direct') }}</h3></span>
                        <span class="rt-rule-count">{{ directCount }}</span>
                    </header>
                    <RouteLists :form="form" :keys="['direct_domain', 'direct_ip']"
                                @edit="onEdit" @preset="onAppend"/>
                </section>
                <section class="rt-rule-panel rt-rule-proxy">
                    <header class="rt-rule-head">
                        <span class="rt-rule-destination"><WifiFilled class="ic"/><h3>{{ t('egress.global') }}</h3></span>
                        <span class="rt-rule-count">{{ proxyCount }}</span>
                    </header>
                    <RouteLists :form="form" :keys="['proxy_domain', 'proxy_ip']"
                                @edit="onEdit" @preset="onAppend"/>
                </section>
            </div>
            <div v-if="form.def_outbound === 'bypass'" class="rt-note">{{ t('routing.directFallback') }}</div>
            <div class="rt-priority">{{ t('routing.priorityHint') }}</div>
            <details class="rt-adv">
                <summary>{{ t('routing.advanced') }}</summary>
                <RouteLists :form="form" :keys="['custom']" mono
                            @edit="onEdit" @preset="onAppend"/>
                <div class="rt-note">{{ t('routing.customHint') }}</div>
            </details>
        </template>

        <!-- The checks live outside the per-card bodies: the six lists
            are one pool of text and a mistake in the block list is as
            silent as one in the rule lists, so the warnings follow the
            text wherever it is being edited. -->
        <div v-if="routeWarnings.length > 0" class="rt-warn">
            <div v-for="(w, i) in routeWarnings" :key="i">{{ w }}</div>
        </div>

        <!-- No save button: this card writes itself, and did so before the
            button existed — the debounced effect above puts every edit on
            disk once the typing stops. What the button added was a toast
            saying so, which is a report about the app rather than something
            the reader asked for; the warnings above it are the part of the
            foot worth its space. -->
    </div>
</template>
