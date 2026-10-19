<script lang="ts">
// --- country flags in names ---
// Names from airports often open with a flag emoji — two Regional Indicator
// code points. Windows has no flag glyphs for them, so WebView2 draws a boxed
// letter pair ("JP") or an empty box, which is what made the list look broken.
// The pair is read as an ISO code, stripped from the name, and drawn as a real
// little flag (CSS for the common countries, a code badge for the rest).

export const FLAG_LEADING_RE = /^([\u{1F1E6}-\u{1F1FF}])([\u{1F1E6}-\u{1F1FF}])/u;

// flagISOOf returns the two-letter code a leading flag emoji stands for, or
// null when the name does not open with one.
export function flagISOOf(name: string): string | null {
    const m = FLAG_LEADING_RE.exec(name);
    if (!m) return null;
    return String.fromCharCode(m[1].codePointAt(0)! - 0x1F1E6 + 65)
        + String.fromCharCode(m[2].codePointAt(0)! - 0x1F1E6 + 65);
}

// nameWithoutFlag drops the leading flag emoji and whatever separator comes
// after it, so the text does not open with a stray dash or space. An empty
// result means the name was the flag alone, and the caller keeps the original.
export function nameWithoutFlag(name: string): string {
    return name.replace(FLAG_LEADING_RE, '').replace(/^[\s\-—|·・]+/, '');
}

// Flag lives in its own SFC now (./Flag.vue); re-exported here so callers of
// the old NodeName.tsx keep one import site.
export {default as Flag} from './Flag.vue';
</script>

<script setup lang="ts">
import {computed} from 'vue';
import Flag from './Flag.vue';

const props = defineProps<{name: string}>();

const iso = computed(() => flagISOOf(props.name));
const rest = computed(() => iso.value ? nameWithoutFlag(props.name) : '');
</script>

<template>
    <!-- NodeName renders a name that may open with a flag emoji: the flag becomes a
         small drawn flag, the rest of the name follows as plain text. -->
    <template v-if="!iso || !rest">{{ name }}</template>
    <template v-else><Flag :iso="iso"/>{{ rest }}</template>
</template>
