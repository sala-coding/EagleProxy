<script setup lang="ts">
import {t} from "../i18n";
import {COUNTRY_PRESETS, ROUTE_LIST_PRESETS, ROUTE_LISTS} from "../lib/dns";
import {countryName} from "../lib/metrics";
import Flag from "./Flag.vue";

// One routing profile's lists as boxes to type into, with the items that come up over and
// over offered underneath. One component rather than the same markup repeated: every list
// behaves identically — typed into, appended to, saved with the rest of the card — and the
// label and the presets are looked up by key, so a list cannot drift from its catalogue.
const props = defineProps<{
    form: any;
    keys: string[];
    mono?: boolean;
}>();

// onEdit -> 'edit'; onPreset -> 'preset'.
const emit = defineEmits<{
    edit: [k: string, v: string];
    preset: [k: string, v: string];
}>();

const entryLabel = (key: string): string => {
    const entry = ROUTE_LISTS.find(l => l.key === key);
    return entry ? t(entry.label) : t('routing.custom');
};

const countryVals = (key: string, c: {domain?: string[]; ip?: string[]}): string[] =>
    key === 'direct_domain' ? (c.domain ?? []) : (c.ip ?? []);
</script>

<template>
    <div class="rt-lists">
        <div class="rt-list" v-for="key in keys" :key="key">
            <label>{{ entryLabel(key) }}</label>
            <textarea :class="mono ? 'rawjson' : undefined"
                      :spellcheck="false"
                      :placeholder="t('routing.listHint')"
                      :value="form[key] ?? ''"
                      @input="emit('edit', key, ($event.target as HTMLTextAreaElement).value)"/>
            <!-- The presets, right under the box they go into. They add rather
                 than replace, and each one is the exact string the config needs:
                 a rule item typed from memory is a rule that never matches. The
                 direct lists lead with the flags: one flag one country's direct
                 rules, the domain table into the domain list, the network range
                 into the IP list, and only a press rather than a retyped rule. -->
            <div class="rt-presets"
                 v-if="(ROUTE_LIST_PRESETS[key]?.length ?? 0) > 0 || key === 'direct_domain' || key === 'direct_ip'">
                <template v-if="key === 'direct_domain' || key === 'direct_ip'">
                    <template v-for="c in COUNTRY_PRESETS" :key="c.code">
                        <button v-if="countryVals(key, c).length > 0"
                                type="button" class="rt-preset rt-flag"
                                :title="t('routing.presetTip', countryName(c.code.toUpperCase()), countryVals(key, c).join(', '))"
                                @click="countryVals(key, c).forEach(v => emit('preset', key, v))">
                            <Flag :iso="c.code.toUpperCase()"/>
                        </button>
                    </template>
                </template>
                <button v-for="v in (ROUTE_LIST_PRESETS[key] ?? [])" :key="v"
                        type="button" class="rt-preset"
                        :title="t('routing.presetHint')"
                        @click="emit('preset', key, v)">
                    {{ v }}
                </button>
            </div>
        </div>
    </div>
</template>
