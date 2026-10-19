<script setup lang="ts">
import {t} from '../../i18n';
import {rsCategoryOf} from '../../lib/metrics';
import {AddFilled} from '../../lib/icons';

// The rule sets card's body: one bar per category, each with its own list and
// its own add button, and the card-level actions under them. The slide div
// stays with SettingsDialog (this card carries no set-group-title of its own,
// exactly as in the React original).
//
// React -> Vue mapping：所有动作（toggle/add/download/remove/downloadAll/
// openDir/addCat）都是事件，后端调用留在 SettingsDialog；rsCatsOpen 由父级
// 持有（它还兼管 localStorage）。
defineProps<{
    // Rule sets are the one part of routing that fails without saying so (see
    // refreshRuleSets in SettingsDialog, which reads this list back).
    ruleSets: {tag: string; url: string; needed: boolean; local: boolean; size: number; modified: string; country: string}[];
    // The card cut into bars by category — 中国 (CN), one bar each (see rsCats
    // in SettingsDialog, which derives this from the list).
    rsCats: {key: string; label: string; code: string}[];
    // The rule-set folds' open hands, remembered too (see rsCatsOpen in
    // SettingsDialog).
    rsCatsOpen: Record<string, boolean>;
    rsBusy: boolean;
    // Which of them have a newer copy at their source than the one on disk, as the last
    // check said.
    rsUpdates: Record<string, boolean>;
    // Bumped on a language change (see langTick in SettingsDialog): receiving a
    // new value re-renders the card, so its t() words are re-spoken.
    langTick?: number;
}>();

const emit = defineEmits<{
    toggle: [key: string];
    add: [code: string];
    download: [tag?: string];
    remove: [tag: string];
    downloadAll: [];
    openDir: [];
    addCat: [];
}>();
</script>

<template>
    <!-- One bar per category rather than one fold for the card: the bars are
        where the countries these rules bite in are said, and each bar's add
        button is the category's own — rules picked from it are filed under
        that country. A bar's list opens only over the bar itself, so a reader
        after one country's rules does not read another's. The card-level
        actions (the downloads for the active profile and the directory) ride
        under the list, where they always were. -->
    <template v-for="cat in rsCats" :key="cat.key">
        <div class="rs-nav">
            <!-- The fold itself, drawn as the bar's left item rather
                than as a summary on its own line: the name is still
                the one click that opens and closes this category's
                list, and the caret beside it still turns with the
                fold. The nameless bar's label is the card's own. -->
            <button type="button" class="rs-nav-toggle"
                    :aria-expanded="!!rsCatsOpen[cat.key]"
                    @click="emit('toggle', cat.key)">
                <!-- The nameless bar is the card itself while the card
                    is empty, and "other" once something on it carries
                    a country — the card's own name twice would read
                    as two cards. -->
                <template v-if="cat.key === ''">{{ ruleSets.length === 0 ? t('settings.group.rulesets') : t('ruleset.general') }}</template>
                <template v-else>{{ `${cat.label} · ${t('settings.group.rulesets')}` }}</template>
                <span :class="`rs-caret${rsCatsOpen[cat.key] ? ' open' : ''}`" aria-hidden="true"/>
            </button>
            <!-- The right end of the bar: the category's own add.
                The country the files are filed under is the name this
                read off the bar, prefilled rather than asked. -->
            <button type="button" class="btn tiny"
                    @click="emit('add', cat.code)">
                {{ t('ruleset.add') }}
            </button>
        </div>
        <div v-if="rsCatsOpen[cat.key]" class="rs-list">
            <template v-for="rs in ruleSets" :key="rs.tag">
                <div v-if="rsCategoryOf(rs).key === cat.key" class="rs-row">
                    <span class="rs-tag">{{ rs.tag }}</span>
                    <span :class="`rs-state ${rs.local ? (rs.needed ? 'local' : 'orphan') : 'missing'}`">
                        {{ rs.local
                            ? `${rs.needed ? t('ruleset.local') : t('ruleset.orphan')} · ${(rs.size / 1024).toFixed(1)} KB`
                            : t('ruleset.remote') }}
                    </span>
                    <template v-if="rs.local">
                        <button v-if="rsUpdates[rs.tag]" class="btn tiny"
                                :title="t('ruleset.updateHint')"
                                :disabled="rsBusy"
                                @click="emit('download', rs.tag)">
                            {{ t('ruleset.update') }}
                        </button>
                        <button class="btn tiny ghost"
                                @click="emit('remove', rs.tag)">
                            {{ t('ruleset.remove') }}
                        </button>
                    </template>
                    <template v-else>
                        <button class="btn tiny" :disabled="rsBusy || !rs.needed"
                                @click="emit('download', rs.tag)">
                            {{ t('ruleset.download') }}
                        </button>
                    </template>
                </div>
            </template>
        </div>
    </template>
    <div v-if="ruleSets.length === 0 && rsCatsOpen['']" class="rs-empty">{{ t('ruleset.empty') }}</div>
    <div class="input-row">
        <button class="btn" :disabled="rsBusy" @click="emit('downloadAll')">
            {{ t('ruleset.downloadAll') }}
        </button>
        <button class="btn ghost" @click="emit('openDir')">
            {{ t('ruleset.openDir') }}
        </button>
        <!-- The card's own add, at the row's far end: a new category, not one
            of the bars' — its country is not known until the reader names it
            in the dialog the picked files end at, so the field opens empty
            and the answer is typed rather than carried in from a bar. -->
        <button type="button" class="btn tiny ghost icon-only rs-addcat"
                :title="t('ruleset.addCat')"
                :aria-label="t('ruleset.addCat')"
                @click="emit('addCat')">
            <AddFilled class="ic"/>
        </button>
    </div>
</template>
