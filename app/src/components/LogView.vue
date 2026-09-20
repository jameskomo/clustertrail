<script setup lang="ts">
// Follows a container log. Auto-scroll sticks to the bottom until the person
// scrolls up; a "jump to live" pill brings them back. Timestamps from the
// API are parsed off each line and shown dimmed.
import { computed, nextTick, ref, watch } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import AppIcon from './AppIcon.vue'
import { useLogs } from '../composables/useLogs'

const props = defineProps<{ cluster: string; namespace: string; pod: string; containers: string[] }>()
const container = ref(props.containers[0] ?? '')
const previous = ref(false)
const filter = ref('')
const wrap = ref(false)
watch(() => props.containers, (c) => { if (!c.includes(container.value)) container.value = c[0] ?? '' })

const { lines, eof, error, paused } = useLogs(computed(() => props.cluster), computed(() => props.namespace), computed(() => props.pod), container, previous)

type Line = { ts: string; text: string; level: string; hay: string }

// Each raw line is split and classified once and kept. A chatty container
// delivers ten frames a second, and re-running two regexes over five
// thousand retained lines on every one of them is most of a core.
const seen = new Map<string, Line>()
const parse = (l: string): Line => {
  let p = seen.get(l)
  if (!p) {
    const sp = l.indexOf(' ')
    const ts = sp > 0 && l[10] === 'T' ? l.slice(11, 23) : ''
    const text = ts ? l.slice(sp + 1) : l
    const level = /\b(error|err|fatal|panic|exception)\b/i.test(text) ? 'err' : /\b(warn|warning)\b/i.test(text) ? 'warn' : ''
    p = { ts, text, level, hay: text.toLowerCase() }
    if (seen.size > 20000) seen.clear()
    seen.set(l, p)
  }
  return p
}

const parsed = computed(() => {
  const q = filter.value.toLowerCase()
  const out: Line[] = []
  for (const l of lines.value) {
    const p = parse(l)
    if (q && !p.hay.includes(q)) continue
    out.push(p)
  }
  return out
})

const box = ref<HTMLElement | null>(null)
const stuck = ref(true)

// Virtualised, like the resource table. Five thousand retained lines were
// five thousand pairs of DOM nodes, re-patched on every frame; wrapped lines
// vary in height, so each one is measured rather than assumed.
const virt = useVirtualizer(computed(() => ({
  count: parsed.value.length,
  getScrollElement: () => box.value,
  estimateSize: () => 20,
  overscan: 24,
})))

const onScroll = () => {
  const el = box.value!
  stuck.value = el.scrollHeight - el.scrollTop - el.clientHeight < 24
}
const toBottom = () => {
  const el = box.value
  if (el) el.scrollTop = el.scrollHeight
}
watch(parsed, async () => { if (stuck.value) { await nextTick(); toBottom() } })
watch(wrap, async () => {
  // Read stuck before re-measuring: re-laying out every line fires scroll
  // events, which clear it, so asking afterwards always says "not at the
  // bottom" and the view jumps away from the newest line.
  const wasAtBottom = stuck.value
  virt.value.measure()
  if (!wasAtBottom) return
  // Lines are measured as they mount, so the total height keeps growing for
  // a few frames. Re-pin until it settles.
  for (let i = 0; i < 12; i++) {
    await nextTick()
    toBottom()
    await new Promise((r) => requestAnimationFrame(r))
  }
  stuck.value = true
})

const jump = () => { stuck.value = true; toBottom() }
</script>

<template>
  <div class="logs">
    <div class="bar">
      <span v-if="containers.length > 1" class="selwrap">
        <select v-model="container" class="input mono sel">
          <option v-for="c in containers" :key="c" :value="c">{{ c }}</option>
        </select>
        <AppIcon name="chevronDown" :size="12" class="chev" />
      </span>
      <span v-else class="mono muted">{{ container }}</span>
      <span class="srch">
        <AppIcon name="search" :size="13" class="sic" />
        <input v-model="filter" class="input flt" placeholder="Filter lines" />
      </span>
      <button class="chip" :class="{ on: previous }" @click="previous = !previous">Previous</button>
      <button class="chip" :class="{ on: wrap }" @click="wrap = !wrap">Wrap</button>
      <button class="btn sm" @click="paused = !paused">{{ paused ? 'Resume' : 'Pause' }}</button>
    </div>
    <div ref="box" class="body mono" :class="{ wrap }" @scroll="onScroll">
      <div v-if="error" class="line err">{{ error }}</div>
      <div class="virt" :style="{ height: virt.getTotalSize() + 'px' }">
        <div
          v-for="v in virt.getVirtualItems()"
          :key="v.index"
          :ref="(el) => virt.measureElement(el as Element)"
          :data-index="v.index"
          class="line"
          :class="parsed[v.index]!.level"
          :style="{ transform: `translateY(${v.start}px)` }"
        >
          <span class="ts">{{ parsed[v.index]!.ts }}</span><span class="txt">{{ parsed[v.index]!.text }}</span>
        </div>
      </div>
      <div v-if="!error && parsed.length === 0" class="line dim">{{ eof ? 'This container wrote nothing.' : 'Waiting for output…' }}</div>
      <div v-if="eof && parsed.length" class="line dim">End of log.</div>
    </div>
    <button v-if="!stuck" class="btn sm jump" @click="jump"><AppIcon name="chevronDown" :size="12" /> Jump to live</button>
  </div>
</template>

<style scoped>
.logs { position: relative; display: flex; flex-direction: column; height: 100%; min-height: 0; }
.bar { display: flex; align-items: center; gap: 8px; padding: 0 0 8px; }
.selwrap { position: relative; display: inline-flex; align-items: center; flex: none; }
.sel { width: 150px; font-size: 12px; appearance: none; padding-right: 24px; }
.chev { position: absolute; right: 7px; color: var(--text-3); pointer-events: none; }
.srch { position: relative; display: inline-flex; align-items: center; flex: 1; min-width: 80px; }
.sic { position: absolute; left: 8px; color: var(--text-3); pointer-events: none; }
.flt { width: 100%; padding-left: 27px; }
.body { flex: 1; overflow: auto; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); padding: 8px 0; font-size: 12.5px; line-height: 1.55; }
.virt { position: relative; width: 100%; }
.virt .line { position: absolute; top: 0; left: 0; width: 100%; }
.line { display: flex; gap: 10px; padding: 0 12px; white-space: pre; }
.line:hover { background: var(--hover); }
.wrap .line { white-space: pre-wrap; word-break: break-all; }
.ts { color: var(--text-3); flex: none; }
.txt { color: var(--text-1); }
.line.err { color: var(--bad); }
.err .txt { color: var(--bad); }
.warn .txt { color: var(--warn); }
.jump { position: absolute; right: 14px; bottom: 12px; box-shadow: var(--shadow-2); }
</style>
