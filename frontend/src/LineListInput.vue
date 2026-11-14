<script setup lang="ts">
import {ref, watch} from 'vue';
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
// The rows are held as local state rather than read back off the string every time: a
// trailing blank line is dropped when a value is first read (a file that ended in a
// newline should not open with an empty row), but that same rule would eat the blank the
// reader just added with 新增 and the button would look dead. So the array is the truth
// while editing, and the string is only re-parsed when something outside changed it.
//
// value/onChange 是受控状态，映射为 v-model（defineModel）。
const value = defineModel<string>({required: true});

const toRows = (s: string): string[] => {
    const lines = (s ?? '').split('\n');
    while (lines.length > 1 && lines[lines.length - 1].trim() === '') lines.pop();
    return lines.length ? lines : [''];
};

const rows = ref<string[]>(toRows(value.value));

// Follow an outside change (a profile switch, a reload) without echoing our own write
// back through toRows — which would strip the blank row the reader just added.
watch(value, (v) => {
    if (v !== rows.value.join('\n')) rows.value = toRows(v);
});

const write = () => { value.value = rows.value.join('\n'); };

const editRow = (i: number, e: Event) => {
    rows.value[i] = (e.target as HTMLInputElement).value;
    write();
};

const addRow = () => {
    rows.value.push('');
    write();
};

const delRow = (i: number) => {
    if (rows.value.length === 1) rows.value = [''];
    else rows.value.splice(i, 1);
    write();
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
                    @click="delRow(i)">
                <CloseFilled class="ic"/>
            </button>
        </div>
        <!-- The row that is not there yet. A button rather than a blank last row, so the
             count of identifiers on screen is the count in the file. -->
        <button type="button" class="ltable-add" @click="addRow">
            <AddFilled class="ic"/>{{ t('btn.add') }}
        </button>
    </div>
</template>
