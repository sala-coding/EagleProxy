<script setup lang="ts">
import {computed} from 'vue';
import {t} from "./i18n";
import {fieldLabel} from "./lib/jsonform";
import {AddFilled, CloseFilled} from './lib/icons';

// A value that is a list of lines, edited as a table of rows.
//
// Two things the textarea this replaces could not say. Where one identifier ended and the
// next began was a newline in a block of text, and the row number — which is what the
// order means, because the fetcher tries these in order until one is accepted (see
// netx.UserAgents) — was something to be counted. So the number is a column, and a line
// separates the rows.
//
// A blank line is not an identifier and is not drawn as one: trailing blanks are dropped
// from the display and come back only as an added row, which also means a file that ended
// in a newline loses it on the first edit. Nothing reads that newline — the fetcher drops
// blank lines too — but it is written, so it is said here rather than left to be found.
//
// value/onChange 是受控状态，映射为 v-model（defineModel）。
const value = defineModel<string>({required: true});

const rows = computed(() => {
    const lines = (value.value ?? '').split('\n');
    while (lines.length > 1 && lines[lines.length - 1].trim() === '') lines.pop();
    return lines;
});

const write = (next: string[]) => { value.value = next.join('\n'); };

const editRow = (i: number, e: Event) => {
    const next = rows.value.slice();
    next[i] = (e.target as HTMLInputElement).value;
    write(next);
};
</script>

<template>
    <div class="ltable">
        <div class="ltable-row" v-for="(line, i) in rows" :key="i">
            <span class="ltable-n">{{ i + 1 }}</span>
            <input :value="line"
                   :aria-label="`${fieldLabel('user_agent2')} ${i + 1}`"
                   @input="editRow(i, $event)"/>
            <button type="button" class="ltable-del"
                    :title="t('btn.delete')" :aria-label="t('btn.delete')"
                    @click="write(rows.length === 1 ? [''] : rows.filter((_, j) => j !== i))">
                <CloseFilled class="ic"/>
            </button>
        </div>
        <!-- The row that is not there yet. A button rather than a blank last row, so the
             count of identifiers on screen is the count in the file. -->
        <button type="button" class="ltable-add" @click="write([...rows, ''])">
            <AddFilled class="ic"/>{{ t('btn.add') }}
        </button>
    </div>
</template>
