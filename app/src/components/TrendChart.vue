<script setup lang="ts">
// One series over time: a 2px line, a 10% wash, two hairline gridlines, and a
// crosshair tooltip. One measure per chart, never two axes. Text uses text
// tokens; only the mark carries the series colour.
import { computed, onMounted, onUnmounted, ref, useId } from 'vue'

const props = defineProps<{ points: { t: string; v: number }[]; title: string; format: (v: number) => string; max?: number; height?: number; context?: string }>()

const wrap = ref<HTMLElement | null>(null)
const W = ref(600)
let ro: ResizeObserver | null = null
onMounted(() => { ro = new ResizeObserver((e) => { const w = e[0]?.contentRect.width; if (w) W.value = Math.max(120, w) }); ro.observe(wrap.value!) })
onUnmounted(() => ro?.disconnect())
const H = computed(() => props.height ?? 120)
const PAD = { l: 8, r: 8, t: 18, b: 22 }

const domain = computed(() => {
  const vs = props.points.map((p) => p.v)
  const peak = Math.max(props.max ?? 0, ...vs, 1)
  // A flat series would otherwise paint the whole frame solid; give it headroom.
  return { lo: 0, hi: peak * (props.max ? 1.02 : 1.9) }
})
const x = (i: number) => PAD.l + (props.points.length < 2 ? 0 : (i / (props.points.length - 1)) * (W.value - PAD.l - PAD.r))
const y = (v: number) => PAD.t + (1 - (v - domain.value.lo) / (domain.value.hi - domain.value.lo)) * (H.value - PAD.t - PAD.b)

const line = computed(() => props.points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.v).toFixed(1)}`).join(' '))
const area = computed(() => props.points.length < 2 ? '' : `${line.value} L${x(props.points.length - 1).toFixed(1)},${y(0)} L${x(0).toFixed(1)},${y(0)} Z`)
const last = computed(() => props.points.at(-1))
const peak = computed(() => props.points.reduce((m, p) => (p.v > m.v ? p : m), props.points[0] ?? { t: '', v: 0 }))
const peakLabel = computed(() => `peak ${props.format(peak.value.v)}`)
const peakW = computed(() => peakLabel.value.length * 6.6 + 14)
const peakX = computed(() => Math.min(W.value - PAD.r - peakW.value / 2, Math.max(PAD.l + peakW.value / 2, x(props.points.indexOf(peak.value)))))

const hover = ref<number | null>(null)
const gid = useId()
const svg = ref<SVGSVGElement | null>(null)
const move = (e: PointerEvent) => {
  if (!svg.value || props.points.length < 2) return
  const r = svg.value.getBoundingClientRect()
  const px = e.clientX - r.left
  hover.value = Math.max(0, Math.min(props.points.length - 1, Math.round(((px - PAD.l) / (W.value - PAD.l - PAD.r)) * (props.points.length - 1))))
}
const hp = computed(() => (hover.value === null ? null : props.points[hover.value]))
const fmtTime = (t: string) => new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
const span = computed(() => {
  if (props.points.length < 2) return ''
  const ms = Date.parse(props.points.at(-1)!.t) - Date.parse(props.points[0]!.t)
  const m = Math.round(ms / 60000)
  return m < 1 ? 'under a minute' : `last ${m} min`
})
</script>

<template>
  <figure ref="wrap" class="trend">
    <figcaption>
      <span class="title">{{ title }}</span>
      <span class="now">{{ hp ? format(hp.v) : last ? format(last.v) : '—' }}</span>
      <span v-if="context" class="dim ctx">{{ context }}</span>
      <span class="dim when">{{ hp ? fmtTime(hp.t) : span }}</span>
    </figcaption>
    <svg v-if="points.length >= 2" ref="svg" :width="W" :height="H" :viewBox="`0 0 ${W} ${H}`" @pointermove="move" @pointerleave="hover = null" role="img" :aria-label="`${title}, ${span}`">
      <line class="grid" :x1="PAD.l" :x2="W - PAD.r" :y1="y(domain.hi / 2)" :y2="y(domain.hi / 2)" />
      <line class="grid" :x1="PAD.l" :x2="W - PAD.r" :y1="y(0)" :y2="y(0)" />
      <text class="axis" :x="W - PAD.r" :y="y(domain.hi / 2) - 4" text-anchor="end">{{ format(domain.hi / 2) }}</text>
      <template v-if="points.length >= 2">
        <defs>
          <linearGradient :id="gid" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" class="g-top" />
            <stop offset="100%" class="g-bot" />
          </linearGradient>
        </defs>
        <path class="area" :d="area" :fill="`url(#${gid})`" />
        <path class="line" :d="line" />
        <circle v-if="last && hover !== points.length - 1" class="lastdot" :cx="x(points.length - 1)" :cy="y(last.v)" r="3" />
        <circle v-if="hp" class="dot" :cx="x(hover!)" :cy="y(hp.v)" r="4" />
        <line v-if="hp" class="hair" :x1="x(hover!)" :x2="x(hover!)" :y1="PAD.t - 6" :y2="H - PAD.b" />
        <g v-if="peak.v > 0 && !hp" class="peak" :transform="`translate(${peakX.toFixed(1)},${(y(peak.v) - 8).toFixed(1)})`">
          <rect :x="-peakW / 2" y="-12" :width="peakW" height="17" rx="3" />
          <text text-anchor="middle" y="0">{{ peakLabel }}</text>
        </g>
      </template>

      <text class="axis" :x="PAD.l" :y="H - 6">{{ points[0] ? fmtTime(points[0].t) : '' }}</text>
      <text class="axis" :x="W - PAD.r" :y="H - 6" text-anchor="end">{{ last ? fmtTime(last.t) : '' }}</text>
    </svg>
    <p v-else class="waiting" :style="{ height: H + 'px' }">{{ points.length === 1 ? 'One sample so far. The line appears at the next reading.' : 'No readings yet. metrics-server is polled every 15 seconds.' }}</p>
  </figure>
</template>

<style scoped>
.trend { margin: 0; display: flex; flex-direction: column; gap: 4px; min-width: 0; }
figcaption { display: flex; align-items: baseline; gap: 10px; font-size: 13.5px; }
.title { color: var(--text-2); }
.now { font-family: var(--mono); font-weight: 500; color: var(--text-0); }
.when { margin-left: auto; font-size: 12px; }
svg { display: block; overflow: visible; touch-action: none; max-width: 100%; }
.ctx { font-size: 12px; }
.waiting { display: flex; align-items: center; margin: 0; color: var(--text-3); font-size: 13px; }
.grid { stroke: var(--line); stroke-width: 1; vector-effect: non-scaling-stroke; }
.line { fill: none; stroke: var(--chart); stroke-width: 1.75; stroke-linejoin: round; stroke-linecap: round; vector-effect: non-scaling-stroke; }
.g-top { stop-color: var(--chart); stop-opacity: 0.3; }
.g-bot { stop-color: var(--chart); stop-opacity: 0; }
.dot { fill: var(--chart); stroke: var(--panel); stroke-width: 2; vector-effect: non-scaling-stroke; }
.lastdot { fill: var(--chart); }
.hair { stroke: var(--text-3); stroke-width: 1; vector-effect: non-scaling-stroke; }
.peak rect { fill: var(--raised); stroke: var(--line-2); stroke-width: 1; vector-effect: non-scaling-stroke; }
.peak text { fill: var(--text-1); font-size: 11px; font-family: var(--mono); }
.axis { fill: var(--text-2); font-size: 12px; font-family: var(--sans); }
.axis { fill: var(--text-3); }
</style>
