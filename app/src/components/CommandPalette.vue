<script setup lang="ts">
// ⌘K. Jumps to a screen, a cluster, a namespace, or an object in the current
// table. Keyboard only by design: this is the "fewer clicks" thesis.
import { computed, ref, watch, nextTick } from 'vue'
import { ui, select } from '../state'
import { KINDS, kindByKey, TOP_SCREENS } from '../kinds'
import type { IconName } from '../icons'
import AppIcon from './AppIcon.vue'
import type { ClusterInfo, Row } from '../api/types'

const props = defineProps<{ clusters: ClusterInfo[]; namespaces: Map<string, Row>; rows: Map<string, Row> }>()
const q = ref('')
const idx = ref(0)
const input = ref<HTMLInputElement | null>(null)

interface Item { group: string; label: string; hint?: string; icon: IconName; run: () => void }

const SCREEN_ICONS: Record<(typeof TOP_SCREENS)[number]['key'], IconName> = { overview: 'overview', timeline: 'timeline', drift: 'drift', changes: 'changes', helm: 'helm' }

const items = computed<Item[]>(() => {
  const out: Item[] = []
  for (const s of TOP_SCREENS) out.push({ group: 'Go to', label: s.label, icon: SCREEN_ICONS[s.key], run: () => (ui.screen = s.key) })
  for (const k of KINDS) out.push({ group: 'Go to', label: k.label, hint: k.group, icon: 'resources', run: () => (ui.screen = k.key) })
  for (const c of props.clusters) out.push({ group: 'Cluster', label: c.name, hint: c.server, icon: 'monitor', run: () => (ui.cluster = c.name) })
  out.push({ group: 'Namespace', label: 'All namespaces', icon: 'resources', run: () => (ui.namespace = '') })
  for (const n of [...props.namespaces.values()].map((r) => r.name).sort()) out.push({ group: 'Namespace', label: n, icon: 'resources', run: () => (ui.namespace = n) })
  const spec = kindByKey(ui.screen)
  if (spec) for (const r of props.rows.values()) out.push({ group: spec.singular, label: r.name, hint: `${r.namespace ?? ''} ${r.status ?? ''}`.trim(), icon: 'resources', run: () => select(spec.key, r.key, r.namespace, r.name) })
  return out
})

const score = (s: string, t: string) => {
  // Subsequence match with a bonus for prefix and word starts.
  s = s.toLowerCase(); t = t.toLowerCase()
  if (!t) return 1
  if (s.startsWith(t)) return 100
  if (s.includes(t)) return 60
  let i = 0
  for (const ch of s) if (ch === t[i]) i++
  return i === t.length ? 20 : 0
}
const results = computed(() => {
  const t = q.value.trim()
  const scored = items.value.map((it) => ({ it, s: score(it.label, t) })).filter((x) => x.s > 0)
  scored.sort((a, b) => b.s - a.s)
  return scored.slice(0, 40).map((x) => x.it)
})
// Sections group the flat, score-sorted results for display. Each row keeps
// its flat index, so keyboard navigation still lines up with `results`.
const sections = computed(() => {
  const out: { name: string; items: { it: Item; i: number }[] }[] = []
  results.value.forEach((it, i) => {
    let s = out.find((x) => x.name === it.group)
    if (!s) { s = { name: it.group, items: [] }; out.push(s) }
    s.items.push({ it, i })
  })
  return out
})
watch(results, () => (idx.value = 0))
watch(() => ui.paletteOpen, async (o) => { if (o) { q.value = ''; await nextTick(); input.value?.focus() } })

const run = (it: Item) => { it.run(); ui.paletteOpen = false }
const key = (e: KeyboardEvent) => {
  if (e.key === 'ArrowDown') { e.preventDefault(); idx.value = Math.min(results.value.length - 1, idx.value + 1) }
  else if (e.key === 'ArrowUp') { e.preventDefault(); idx.value = Math.max(0, idx.value - 1) }
  else if (e.key === 'Enter') { const it = results.value[idx.value]; if (it) run(it) }
  else if (e.key === 'Escape') ui.paletteOpen = false
}
</script>

<template>
  <Transition name="fade">
    <div v-if="ui.paletteOpen" class="scrim" @click.self="ui.paletteOpen = false">
      <div class="palette card">
        <div class="q-row">
          <AppIcon name="search" :size="16" class="q-ic" />
          <input ref="input" v-model="q" class="q" placeholder="Go to a screen, cluster, namespace or object" @keydown="key" />
        </div>
        <div class="list">
          <template v-for="s in sections" :key="s.name">
            <div class="sec">{{ s.name }}</div>
            <div v-for="r in s.items" :key="r.it.group + r.it.label" class="item" :class="{ on: r.i === idx }" @mouseenter="idx = r.i" @click="run(r.it)">
              <AppIcon :name="r.it.icon" :size="14" class="ic" />
              <span class="label mono">{{ r.it.label }}</span>
              <span v-if="r.it.hint" class="hint">{{ r.it.hint }}</span>
            </div>
          </template>
          <div v-if="!results.length" class="empty">No matches</div>
        </div>
        <footer><span><span class="kbd">↑↓</span> navigate</span><span><span class="kbd">⏎</span> open</span><span><span class="kbd">esc</span> close</span></footer>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; background: rgba(4, 6, 10, 0.55); display: flex; align-items: flex-start; justify-content: center; padding-top: 14vh; z-index: 45; }
.palette { width: min(640px, calc(100vw - 48px)); border-radius: var(--r-lg); box-shadow: var(--shadow-3); overflow: hidden; animation: pop 160ms var(--ease); }
@keyframes pop { from { opacity: 0; transform: translateY(-6px) scale(0.985); } }
.q-row { display: flex; align-items: center; gap: 10px; height: 52px; padding: 0 16px; border-bottom: 1px solid var(--line); }
.q-ic { color: var(--text-3); }
.q { flex: 1; min-width: 0; height: 100%; padding: 0; background: transparent; border: 0; font-size: 15px; color: var(--text-0); caret-color: var(--accent); outline: none; box-shadow: none; }
.q:focus-visible { box-shadow: none; }
.q::placeholder { color: var(--text-3); }
.list { max-height: 50vh; overflow: auto; padding: 6px; }
.sec { padding: 8px 10px 3px; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-3); }
.sec:first-child { padding-top: 4px; }
.item { display: flex; align-items: center; gap: 10px; height: 34px; padding: 0 10px; border-radius: var(--r-sm); cursor: default; }
.item.on { background: var(--accent-soft); }
.ic { color: var(--text-3); }
.item.on .ic { color: var(--accent-2); }
.label { color: var(--text-1); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.item.on .label { color: var(--text-0); }
.hint { margin-left: auto; padding-left: 16px; max-width: 240px; font-size: 11.5px; color: var(--text-3); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.empty { padding: 28px 16px; font-size: 13px; }
footer { display: flex; gap: 14px; align-items: center; padding: 9px 16px; border-top: 1px solid var(--line); font-size: 11.5px; color: var(--text-3); }
footer > span { display: inline-flex; align-items: center; gap: 6px; }
.fade-enter-active, .fade-leave-active { transition: opacity 120ms; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
