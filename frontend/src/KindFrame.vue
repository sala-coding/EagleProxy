<script setup lang="ts">
// One of those frames: a bordered block whose legend says what kind of row it holds, and
// whose rows keep their own names in the label column beside them (see .kind-box — the
// same frame as the looks box, read the other way round). The caller's own rows of that
// kind stand ahead of the drawn ones (see `extraRows` in JsonForm), which is where a
// hand-written row of a kind belongs: beside its own kind, not outside the frame the
// kind stands in.
defineProps<{kind: 'text' | 'switch'; label: string}>();
</script>

<template>
    <div :class="`kind-box kind-${kind}`">
        <span class="look-box-title">{{ label }}</span>
        <slot/>
    </div>
</template>
