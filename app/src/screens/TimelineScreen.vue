<script setup lang="ts">
// What changed. The first question of any incident, answered from the four
// places the answer normally hides: rollouts, the managedFields record of who
// wrote which fields, the cluster's own events, and ClusterTrail's audit log.
import { computed, ref, watch } from 'vue'
import { ui, select } from '../state'
import { engine } from '../api/socket'
import { kindByKey } from '../kinds'
import { now } from '../composables/useClock'
import { timeAgo } from '../composables/useFormat'
import AppIcon from '../components/AppIcon.vue'

type Source = 'rollout' | 'field' | 'event' | 'clustertrail'
interface Item {
  at: string; source: Source; kind?: string; kindName?: string; namespace?: string
  name?: string; actor?: string; summary: string; detail?: string; severity: string; count?: number
}
interface Report { cluster: string; namespace?: string; since: string; minutes: number; items: Item[]; counts: Record<Source, number>; truncated?: boolean }

const minutes = ref(180)
const report = ref<Report | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const hide = ref<Set<Source>>(new Set())
const peopleOnly = ref(false)

const load = async () => {
  if (!ui.cluster) return
  loading.value = true; error.value = null
  try {
    const m = await engine.request({ type: 'timeline', cluster: ui.cluster, namespace: ui.namespace, minutes: minutes.value })
    report.value = (m as any).timeline as Report
  } catch (e: any) { error.value = e.message; report.value = null } finally { loading.value = false }
}
watch([() => ui.cluster, () => ui.namespace, minutes], load, { immediate: true })

const machine = (i: Item) => !!i.detail?.startsWith('written by a controller') || i.source === 'event' && i.actor === 'kubelet'
const visible = computed(() => {
  let items = report.value?.items ?? []
  if (hide.value.size) items = items.filter((i) => !hide.value.has(i.source))
  if (peopleOnly.value) items = items.filter((i) => i.source === 'clustertrail' || (i.source === 'field' && !machine(i)))
  return items
})
const toggle = (s: Source) => {
  const next = new Set(hide.value)
  next.has(s) ? next.delete(s) : next.add(s)
  hide.value = next
}

// Group into coarse buckets so a long window still reads as a story.
const groups = computed(() => {
  const out: { label: string; items: Item[] }[] = []
  for (const i of visible.value) {
    const mins = Math.floor((now.value - Date.parse(i.at)) / 60000)
    const label = mins < 5 ? 'Just now' : mins < 60 ? 'Last hour' : mins < 180 ? 'Last three hours' : 'Earlier'
    const last = out[out.length - 1]
    if (last && last.label === label) last.items.push(i)
    else out.push({ label, items: [i] })
  }
  return out
})

const sourceLabel: Record<Source, string> = { rollout: 'rollouts', field: 'field writes', event: 'cluster events', clustertrail: 'your changes' }
const open = (i: Item) => {
  if (!i.kind || !i.name || !kindByKey(i.kind)) return
  ui.screen = i.kind
  select(i.kind, i.namespace ? `${i.namespace}/${i.name}` : i.name, i.namespace, i.name, i.source === 'event' ? 'events' : 'overview')
}
const ranges = [30, 180, 720]
const rangeLabel = (m: number) => (m < 60 ? `${m} min` : `${m / 60} hours`)
</script>

<template>
  <div class="screen">
    <Teleport defer to="#screen-actions">
      <template v-if="report">
        <button v-for="s in (['rollout','field','event','clustertrail'] as Source[])" :key="s" class="chip" :class="{ on: !hide.has(s) }" @click="toggle(s)">
          {{ report.counts[s] ?? 0 }} {{ sourceLabel[s] }}
        </button>
        <button class="chip" :class="{ on: peopleOnly }" @click="peopleOnly = !peopleOnly">People only</button>
        <span v-if="report.truncated" class="dim note">latest 400</span>
      </template>
      <div class="seg" role="radiogroup" aria-label="Time range">
        <button v-for="r in ranges" :key="r" :class="{ on: minutes === r }" role="radio" :aria-checked="minutes === r" @click="minutes = r">{{ rangeLabel(r) }}</button>
      </div>
      <button class="btn sm" :disabled="loading" @click="load">{{ loading ? 'Reading…' : 'Refresh' }}</button>
    </Teleport>

    <div v-if="error" class="err mono">{{ error }}</div>

    <div v-else-if="!report && loading" class="empty">Reading the last {{ rangeLabel(minutes) }}…</div>

    <div v-else-if="report" class="feed">
      <div v-if="!visible.length" class="empty">
        <AppIcon name="timeline" :size="20" />
        <strong>Nothing changed in this window.</strong>
        <span>No rollout, no edited field, no notable event, and no change made through ClusterTrail.</span>
      </div>

      <template v-for="g in groups" :key="g.label + g.items[0]!.at">
        <div class="bucket">{{ g.label }}</div>
        <button v-for="i in g.items" :key="i.at + i.source + i.name + i.summary" class="row" :class="{ clickable: i.kind && kindByKey(i.kind) }" @click="open(i)">
          <span class="time mono dim">{{ timeAgo(i.at, now) }}</span>
          <span class="rail" :class="[i.source, i.severity]"></span>
          <span class="what">
            <span class="line">
              <span class="mono subject">{{ i.kindName }}<template v-if="i.name">/{{ i.name }}</template></span>
              <span class="summary" :class="i.severity">{{ i.summary }}</span>
              <span v-if="i.count && i.count > 1" class="pill">×{{ i.count }}</span>
            </span>
            <span v-if="i.detail" class="detail dim">{{ i.detail }}</span>
          </span>
          <span class="actor dim mono">{{ i.actor }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.actions .note { font-size: 12px; }
.feed { flex: 1; overflow: auto; padding: 2px 20px 20px; }
.bucket { padding: 16px 0 6px; font-size: 12px; font-weight: 600; letter-spacing: 0.02em; color: var(--text-3); }
.row { display: grid; grid-template-columns: 62px 3px minmax(0, 1fr) max-content; align-items: start; gap: 12px; width: 100%; padding: 8px 10px 8px 6px; background: none; border: 0; border-radius: var(--r-sm); text-align: left; color: var(--text-0); }
.row.clickable { cursor: pointer; }
.row.clickable:hover { background: var(--panel); }
.time { padding-top: 1px; font-size: 12px; text-align: right; }
.rail { align-self: stretch; min-height: 20px; border-radius: 2px; background: var(--line-2); }
.rail.rollout { background: var(--accent); }
.rail.field { background: var(--chart-b); }
.rail.clustertrail { background: var(--ok); }
.rail.warn { background: var(--warn); }
.rail.bad { background: var(--bad); }
.what { min-width: 0; }
.line { display: flex; align-items: baseline; gap: 9px; flex-wrap: wrap; }
.subject { font-weight: 550; font-size: 13px; }
.summary { font-size: 13.5px; }
.summary.bad { color: var(--bad); }
.summary.warn { color: var(--warn); }
.detail { display: block; margin-top: 2px; font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.actor { padding-top: 2px; font-size: 12px; max-width: 190px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.err { color: var(--bad); padding: 20px; white-space: pre-wrap; }
</style>
