<script setup lang="ts">
import {computed} from 'vue';

// One DNS address cell: a text input for anything typed by hand, and a caret
// beside it that opens the presets as a menu to click. The native datalist gave
// both but showed neither well — the suggestions arrived filtered by what was
// already typed, behind an arrow the field did not show. The menu opens
// downward (the field sits mid-card, not on a bar) and closes on a click
// anywhere outside it.
// A choice between two answers, as two buttons with one of them in force.
//
// Neither answer is "off" — 代理 and 直连 are two directions, not a state and its absence —
// so both are on screen and both are pressed for what they are: two things, one of which is
// in force, and pressing either takes it. It was a pill that slid between them, which said
// the same thing with the extra implication that one end was the switch being on and the
// other its absence, whatever the two ends were called.
//
// Used for the two fallbacks, which are the only two-answer choices in this dialog that are
// not modes in their own right: the four modes have cards to be picked from, and everything
// else here is a value to be typed.
//
// value/onChange 是受控状态，映射为 v-model（defineModel）。
const props = defineProps<{
    leftValue: string;
    rightValue: string;
    left: string;
    right: string;
    label: string;
}>();

const value = defineModel<string>({required: true});

const answers = computed<[string, string][]>(() =>
    [[props.leftValue, props.left], [props.rightValue, props.right]]);
</script>

<template>
    <!-- A radio group, because that is what it is: one of these, always one. -->
    <div class="either" role="radiogroup" :aria-label="label">
        <button v-for="[v, word] in answers" :key="v" type="button" role="radio"
                :aria-checked="value === v"
                :class="`either-btn${value === v ? ' on' : ''}`"
                @click="value = v">
            {{ word }}
        </button>
    </div>
</template>
