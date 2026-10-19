<script setup lang="ts">
// Flag draws one country flag from the flag-icons package (real SVGs, every
// ISO 3166-1 code), sized by .flag in App.css. A flag emoji's letter pair is
// always a valid ISO code, so there is no fallback path.
defineProps<{iso: string}>();
</script>

<template>
    <i :class="`flag fi fi-${iso.toLowerCase()}`" aria-hidden="true"/>
</template>
