<script setup lang="ts">
import {t} from "../i18n";

// A choice among a few named answers, drawn as one capsule cut into segments: the chosen
// segment is filled with the accent, the others stay on the card's surface.
//
// Not the sliding pill the two-answer switches use (see EitherSwitch): two answers are the
// two ends of one thing and a knob travelling between them says which end it is at, while
// three answers have no ends — the middle one is not "more" or "less" than either — and a
// knob has no middle to stand at.
//
// value/onChange 是受控状态，映射为 v-model（defineModel）。
defineProps<{
    options: {value: string; label: string}[];
    label: string;
}>();

const value = defineModel<string>({required: true});
</script>

<template>
    <!-- A radio group, because that is what it is: one of these, always one. -->
    <div class="seg" role="radiogroup" :aria-label="label">
        <button v-for="o in options" :key="o.value" type="button" role="radio"
                :aria-checked="value === o.value"
                :class="`seg-item${value === o.value ? ' on' : ''}`"
                @click="value = o.value">
            {{ t(o.label) }}
        </button>
    </div>
</template>
