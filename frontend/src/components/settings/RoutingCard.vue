<script setup lang="ts">
import {computed} from 'vue';
import {t} from '../../i18n';
import {ROUTE_CARDS, SNIFF_MODES, STRATEGY_OPTIONS} from '../../lib/dns';
import {CheckCircleFilled} from '../../lib/icons';
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

const onEdit = (k: string, v: string) => emit('edit', k, v);
const onAppend = (k: string, v: string) => emit('append', k, v);
</script>

<template>
    <div class="rt-cards">
        <button v-for="m in ROUTE_CARDS" :key="m" type="button"
                :class="`rt-card${focus === m ? ' active' : ''}${egress === m ? ' current' : ''}`"
                @click="emit('card', m)">
            <span class="rt-card-name">
                <CheckCircleFilled v-if="egress === m" class="ic"/><i v-else class="state-dot"/>{{ t(`egress.${m}`) }}
            </span>
            <span class="rt-card-note">{{ t(`routing.card.${m}`) }}</span>
        </button>
        <!-- The fourth card is not a fourth mode. Its lists are read ahead of
            whatever the mode decides, so they refuse their matches under the
            proxy card as readily as under the rule card — which is why it has
            settings but switches nothing. -->
        <button type="button"
                :class="`rt-card rt-block${focus === 'block' ? ' active' : ''}`"
                @click="emit('card', 'block')">
            <span class="rt-card-name">
                <i class="state-dot"/>{{ t('routing.card.blockName') }}
            </span>
            <span class="rt-card-note">{{ t('routing.card.block') }}</span>
        </button>
    </div>

    <div v-if="!form" class="hint">加载中…</div>
    <div v-else class="rt-body">
        <!-- The three fields the whole profile carries, above the fields the
            focused mode does. Sniffing is what turns a connection into a
            domain; the two strategies are how that domain — and the
            addresses this app has to resolve for itself — become IPs. Read
            in that order, they are the three questions in front of
            everything else on this card.

            They stand outside the per-mode bodies because the profile
            carries them and the builder writes them whatever the mode is
            (see noteSniffing and the default_domain_resolver in buildRoute),
            while 兜底出站 further down is what only rule mode reads. -->
        <div class="rt-wide">
            <div class="rt-cell">
                <label :title="t('routing.sniffHint')">{{ t('routing.sniffing') }}</label>
                <SegmentedSwitch :label="t('routing.sniffing')"
                                 :options="SNIFF_MODES"
                                 :model-value="String(form.sniffing_mode ?? 1)"
                                 @update:model-value="v => emit('edit', 'sniffing_mode', Number(v))"/>
            </div>
            <div class="rt-cell">
                <label :title="t('routing.domainStrategyHint')">
                    {{ t('routing.domainStrategy') }}
                </label>
                <SegmentedSwitch :label="t('routing.domainStrategy')"
                                 :options="STRATEGY_OPTIONS"
                                 :model-value="form.domain_strategy ?? ''"
                                 @update:model-value="v => emit('edit', 'domain_strategy', v)"/>
            </div>
            <div class="rt-cell">
                <label :title="t('routing.outboundStrategyHint')">
                    {{ t('routing.outboundDomainStrategy') }}
                </label>
                <SegmentedSwitch :label="t('routing.outboundDomainStrategy')"
                                 :options="STRATEGY_OPTIONS"
                                 :model-value="form.outbound_domain_strategy ?? ''"
                                 @update:model-value="v => emit('edit', 'outbound_domain_strategy', v)"/>
            </div>
        </div>
        <template v-if="focus === 'block'">
            <div class="hint">{{ t('routing.blockHint') }}</div>
            <RouteLists :form="form" :keys="['block_domain', 'block_ip']"
                        @edit="onEdit" @preset="onAppend"/>
        </template>
        <template v-else-if="focus === 'rule'">
            <!-- One field where there were four: the three DNS cells
                have a card of their own now (see below), and the
                connection fallback is what is left of this row. Sized
                like the cell it was rather than stretched across the
                sheet — a two-word answer does not need the width of a
                window to say itself in. -->
            <div class="rt-solo">
                <div class="rt-cell">
                    <label>{{ t('routing.defOutbound') }}</label>
                    <EitherSwitch :label="t('routing.defOutbound')"
                                  :left="t('egress.direct')" :right="t('egress.global')"
                                  left-value="bypass" right-value="proxy"
                                  :model-value="form.def_outbound || 'proxy'"
                                  @update:model-value="v => emit('edit', 'def_outbound', v)"/>
                </div>
            </div>
            <RouteLists :form="form"
                        :keys="['direct_domain', 'direct_ip', 'proxy_domain', 'proxy_ip']"
                        @edit="onEdit" @preset="onAppend"/>
            <!-- Whitelist mode makes the direct lists redundant as
                connection rules — anything they match is already
                going where the fallback sends it — but not as DNS
                rules, which the builder still writes from them
                regardless of the mode. Said here rather than by
                disabling the boxes: disabled would read as
                "inactive", and half of the boxes still is. -->
            <div v-if="form.def_outbound === 'bypass'" class="rt-note">{{ t('routing.directFallback') }}</div>
            <!-- The order the builder writes, said where the lists
                are typed rather than left in the source. -->
            <div class="rt-note">{{ t('routing.priorityHint') }}</div>
            <details class="rt-adv">
                <summary>{{ t('routing.advanced') }}</summary>
                <RouteLists :form="form" :keys="['custom']" mono
                            @edit="onEdit" @preset="onAppend"/>
                <div class="rt-note">{{ t('routing.customHint') }}</div>
            </details>
        </template>
        <template v-else>
            <div class="hint">{{ t(`routing.modeHint.${focus}`) }}</div>
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
