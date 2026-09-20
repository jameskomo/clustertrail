<script setup lang="ts">
// A tiny bar plus the number. The bar is relative to `max` when known
// (node capacity), otherwise to a fixed scale so pods are comparable.
import { computed } from 'vue'
const props = defineProps<{ value?: number; max?: number; format: (v: number | undefined) => string }>()
const pct = computed(() => {
  if (props.value === undefined) return 0
  const max = props.max ?? 0
  return max > 0 ? Math.min(100, (props.value / max) * 100) : 0
})
const tone = computed(() => (pct.value > 90 ? 'bad' : pct.value > 70 ? 'warn' : 'ok'))
</script>

<template>
  <span class="usage" :class="{ na: value === undefined }">
    <span class="bar"><span class="fill" :class="tone" :style="{ width: pct + '%' }"></span></span>
    <span class="val mono">{{ value === undefined ? '—' : format(value) }}</span>
  </span>
</template>

<style scoped>
.usage { display: inline-flex; align-items: center; gap: 8px; width: 100%; }
.bar { flex: 1; height: 4px; border-radius: 2px; background: var(--raised); overflow: hidden; min-width: 28px; }
.fill { display: block; height: 100%; border-radius: 2px; transition: width 400ms var(--ease); }
.fill.ok { background: var(--ok); }
.fill.warn { background: var(--warn); }
.fill.bad { background: var(--bad); }
.val { min-width: 44px; text-align: right; color: var(--text-1); }
.na .val { color: var(--text-3); }
</style>
