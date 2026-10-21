<script setup lang="ts">
// CrashBoundary keeps a render error from blanking the whole window: the app
// reports what went wrong instead of silently disappearing. It captures any
// error thrown while rendering the slot, shows the crash screen, and offers a
// retry that clears the captured error and re-renders.
import {onErrorCaptured, ref} from 'vue'
import {t} from './i18n'

const error = ref<Error | null>(null)

// The handler's name says what it does; the hook registration below is what
// makes the boundary real — a plain function of this name alone would never be
// called by anything.
onErrorCaptured((err: Error) => {
  console.error('render error:', err)
  error.value = err
  // Stop the error here; the crash screen is the report.
  return false
})
</script>

<template>
  <div v-if="error" class="crash">
    <h2>{{ t('crash.title') }}</h2>
    <pre>{{ String(error.stack || error) }}</pre>
    <button class="btn" @click="error = null">{{ t('crash.retry') }}</button>
  </div>
  <slot v-else/>
</template>
