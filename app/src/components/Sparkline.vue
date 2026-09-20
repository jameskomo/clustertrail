<script setup lang="ts">
// A tiny inline trend for signal panels: no axes, no chrome, just the shape.
// Scales to a fixed viewBox so no measuring is needed.
import { computed } from 'vue'

const props = withDefaults(defineProps<{ points: { t: string; v: number }[]; height?: number }>(), { height: 26 })
const H = computed(() => props.height)

const path = computed(() => {
  const pts = props.points
  if (pts.length < 2) return { line: '', area: '' }
  const peak = Math.max(...pts.map((p) => p.v), 1e-9)
  const x = (i: number) => (i / (pts.length - 1)) * 100
  const y = (v: number) => H.value - 2 - (v / peak) * (H.value - 4)
  const line = pts.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.v).toFixed(1)}`).join(' ')
  const area = `${line} L100,${H.value} L0,${H.value} Z`
  return { line, area }
})
</script>

<template>
  <svg class="spark" :viewBox="`0 0 100 ${H}`" preserveAspectRatio="none" :style="{ height: H + 'px' }" aria-hidden="true">
    <path v-if="path.area" :d="path.area" class="sa" />
    <path v-if="path.line" :d="path.line" class="sl" />
  </svg>
</template>

<style scoped>
.spark { display: block; width: 100%; }
.sl { fill: none; stroke: var(--chart); stroke-width: 1.5; stroke-linejoin: round; stroke-linecap: round; vector-effect: non-scaling-stroke; }
.sa { fill: var(--chart); opacity: 0.12; }
</style>
