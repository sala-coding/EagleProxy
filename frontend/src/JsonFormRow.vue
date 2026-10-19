<script setup lang="ts">
import {ref} from 'vue';
import {arrayToText, fieldHint, fieldLabel, HOTKEY_KEY, isPlainObject, LINE_LIST_KEYS, LONG_KEYS, textToArray, type Json} from './lib/jsonform';
import HotkeyInput from './HotkeyInput.vue';
import LineListInput from './LineListInput.vue';
import JsonForm from './JsonForm.vue';

// One row of a JsonForm, extracted so the grouped and ungrouped layouts can share the
// same markup (the React original built it with the objectRow / lineListRow / row
// helpers). `set` becomes the 'set' emit; onHotkeyCapture is forwarded as
// 'hotkey-capture'; acceptHotkey rides down to HotkeyInput as a callable prop because
// its answer is awaited.
const props = defineProps<{
    k: string;
    v: Json;
    depth: number;
    // The keys that keep a row of their own whatever cols says (see JsonForm).
    fullRow?: string[];
    acceptHotkey?: (key: string, combo: string) => Promise<boolean>;
}>();

const emit = defineEmits<{
    set: [k: string, v: Json];
    hotkeyCapture: [active: boolean];
}>();

// Number inputs hold their typing in flight here (see the number row below).
const numDraft = ref<string | null>(null);

const onNumInput = (e: Event) => {
    const txt = (e.target as HTMLInputElement).value;
    numDraft.value = txt;
    if (txt === '') return;
    const n = Number(txt);
    // A field that arrived as a whole number is a whole number
    // on the Go side (int, not float64): writing 1.5 into one
    // makes the settings file refuse to unmarshal, and from
    // that moment on every save in the panel fails while the
    // fields go on showing values that were never written.
    // Round to what the field can hold — the draft above keeps
    // the "1." being typed, so nothing is lost on the way.
    if (Number.isFinite(n)) emit('set', props.k, Number.isInteger(props.v as number) ? Math.round(n) : n);
};

const onNumBlur = () => {
    numDraft.value = null;
};
</script>

<template>
    <!-- A nested object is its own row: a boxed block rather than a control with a name in
         the label column beside it. Its name rides the block's own border (see .jf-nested)
         the way every frame in the settings sheet names itself, because the two fields the
         object holds are territory the reader walks into — a frame saying so on its own edge
         reads as a room with a door name, not as one label pointing at two values. -->
    <div class="jf-field jf-object" v-if="isPlainObject(v)">
        <div class="jf-nested" :data-title="fieldLabel(k)">
            <JsonForm :model-value="v as Record<string, Json>"
                      @update:model-value="nv => emit('set', k, nv)"
                      :depth="depth + 1"
                      @hotkey-capture="emit('hotkeyCapture', $event)"
                      :accept-hotkey="acceptHotkey"/>
        </div>
        <div class="jf-hint" v-if="fieldHint(k)">{{ fieldHint(k) }}</div>
    </div>
    <!-- A line-list row is boxed whole, the same treatment the nested object takes: a
         list is long enough to fill the row, its rows rule from edge to edge of the table
         inside it, and a name at the far end of a 150px label column reads as captioning
         nothing. The legend rides the box's border instead (see .jf-nested). -->
    <div class="jf-field jf-object" v-else-if="LINE_LIST_KEYS.has(k)">
        <div class="jf-nested" :data-title="fieldLabel(k)">
            <LineListInput :model-value="(v as string) ?? ''"
                           @update:model-value="nv => emit('set', k, nv)"/>
        </div>
        <div class="jf-hint" v-if="fieldHint(k)">{{ fieldHint(k) }}</div>
    </div>
    <!-- One row: its name in the label column, its control beside it (see .set-group
         .jf-field). The raw key stays in the label's tooltip: the label is the interface's
         word for the key, the key is the file's, and the mapping stays checkable (see
         fieldLabel).
         The switch's control carries no 'true'/'false' beside it: it is drawn as a capsule
         with a knob (see .modal .jf-check input[type="checkbox"]), which says on or off by
         itself, and those words said it a second time — in the config file's language rather
         than the interface's, and on every boolean row in every card, since this is where
         they are all drawn. -->
    <div v-else :class="`jf-field${LONG_KEYS.has(k) ? ' jf-long' : fullRow?.includes(k) ? ' jf-full' : ''}`">
        <!-- The label carries both names when they differ: the interface's word for the
             key, and the key itself, which is what the file and the logs call it. The
             label column is 88px and a label that does not fit is cut rather than
             wrapped (see .set-group .jf-field > label), so for the long ones the
             tooltip is the only place the whole label can be read — and the only place
             the key can be read at all. For a key this interface has no word for, the
             two are the same word and one of them is enough. -->
        <label :title="fieldLabel(k) === k ? k : `${fieldLabel(k)} · ${k}`">{{ fieldLabel(k) }}</label>
        <input v-if="Array.isArray(v)"
               :value="arrayToText(v)"
               @input="emit('set', k, textToArray(($event.target as HTMLInputElement).value, v as Json[]))"/>
        <label class="jf-check" v-else-if="typeof v === 'boolean'">
            <input type="checkbox" :checked="v"
                   @change="emit('set', k, ($event.target as HTMLInputElement).checked)"/>
        </label>
        <!-- The draft keeps what is still being typed: "-" and "1." are on their
             way to a number but Number() rejects both, and writing that NaN in
             would strand the field (and a null in the saved JSON). Failure to
             parse holds the last good value until blur settles on the draft. -->
        <input type="number" v-else-if="typeof v === 'number'"
               :value="numDraft ?? String(v)"
               @input="onNumInput"
               @blur="onNumBlur"/>
        <textarea rows="3" v-else-if="LONG_KEYS.has(k)"
                  :value="(v as string) ?? ''"
                  @input="emit('set', k, ($event.target as HTMLTextAreaElement).value)"/>
        <HotkeyInput v-else-if="HOTKEY_KEY.test(k)"
                     :model-value="(v as string) ?? ''"
                     @update:model-value="nv => emit('set', k, nv)"
                     @capture="emit('hotkeyCapture', $event)"
                     :hotkey-key="k"
                     :accept-hotkey="acceptHotkey"/>
        <input v-else
               :value="(v as string) ?? ''"
               @input="emit('set', k, ($event.target as HTMLInputElement).value)"/>
        <div class="jf-hint" v-if="fieldHint(k)">{{ fieldHint(k) }}</div>
    </div>
</template>
