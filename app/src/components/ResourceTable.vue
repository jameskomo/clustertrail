<script setup lang="ts">
// One table for every kind. Virtualised: only the rows in view exist in the
// DOM, so 10k rows cost the same as 40. Rows arrive pre-projected; the only
// work here is filter, sort and paint.
import { computed, ref, watch } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import type { Row } from '../api/types'
import type { Column, KindSpec } from '../kinds'
import { now } from '../composables/useClock'
import { cpu, bytes, timeAgo } from '../composables/useFormat'
import HealthDot from './HealthDot.vue'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ rows: Map<string, Row>; kind: KindSpec; filter: string; showNamespace: boolean; selectedKey?: string | null }>()
const emit = defineEmits<{ select: [row: Row] }>()

const columns = computed(() => props.kind.columns.filter((c) => props.showNamespace || c.key !== 'namespace'))
const template = computed(() => columns.value.map((c) => c.width).join(' '))
// Sum of each track's minimum, so the grid never squeezes below it: the
// table scrolls sideways instead of hiding columns.
const minWidth = computed(() => columns.value.reduce((n, c) => n + (parseInt(c.width.match(/minmax\((\d+)px/)?.[1] ?? c.width) || 0), 0) + 'px')

const sortKey = ref(props.kind.key === 'events' ? 'createdAt' : 'name')
const sortDir = ref<1 | -1>(props.kind.key === 'events' ? -1 : 1)
watch(() => props.kind.key, (k) => { sortKey.value = k === 'events' ? 'createdAt' : 'name'; sortDir.value = k === 'events' ? -1 : 1 })
const setSort = (c: Column) => {
  if (sortKey.value === c.key) sortDir.value = sortDir.value === 1 ? -1 : 1
  else { sortKey.value = c.key; sortDir.value = c.type === 'age' || c.type === 'time' ? -1 : 1 }
}

const valueOf = (r: Row, key: string): any => (key in r ? (r as any)[key] : r.cells[key])

// Searchable text is built once per row object and remembered. Rows are
// replaced rather than mutated when they change, so the entry goes with the
// old object and there is nothing to invalidate. Without this, every
// keystroke concatenated every cell of every row, at ten deltas a second.
const haystacks = new WeakMap<object, string>()
const haystackOf = (r: Row) => {
  let hay = haystacks.get(r)
  if (hay === undefined) {
    hay = `${r.name} ${r.namespace ?? ''} ${r.status ?? ''} ${Object.values(r.cells).flat().join(' ')}`.toLowerCase()
    haystacks.set(r, hay)
  }
  return hay
}

const visible = computed(() => {
  const q = props.filter.trim().toLowerCase()
  let list = [...props.rows.values()]
  if (q) {
    const terms = q.split(/\s+/)
    list = list.filter((r) => {
      const hay = haystackOf(r)
      return terms.every((t) => hay.includes(t))
    })
  }
  const k = sortKey.value, d = sortDir.value
  list.sort((a, b) => {
    const x = valueOf(a, k) ?? '', y = valueOf(b, k) ?? ''
    if (x === y) return a.key < b.key ? -1 : 1
    return (x < y ? -1 : 1) * d
  })
  return list
})

const scroller = ref<HTMLElement | null>(null)
const virt = useVirtualizer(computed(() => ({
  count: visible.value.length,
  getScrollElement: () => scroller.value,
  estimateSize: () => 38,
  overscan: 14,
})))

const statusTone = (r: Row) => (r.health === 'bad' ? 'bad' : r.health === 'warn' ? 'warn' : '')
const list = (v: any) => (Array.isArray(v) ? v.join(', ') : v ?? '')
const fmtTime = (v: any) => (v ? new Date(v).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '—')
</script>

<template>
  <div ref="scroller" class="table">
    <div class="inner" :style="{ minWidth }">
    <div class="head row" :style="{ gridTemplateColumns: template }">
      <button v-for="c in columns" :key="c.key" class="th" :class="[{ on: sortKey === c.key, right: c.align === 'right' }]" @click="setSort(c)">
        {{ c.label }}<AppIcon v-if="sortKey === c.key" :name="sortDir === 1 ? 'sortAsc' : 'sortDesc'" :size="12" />
      </button>
    </div>
    <div class="body">
      <div :style="{ height: virt.getTotalSize() + 'px', position: 'relative' }">
        <div
          v-for="v in virt.getVirtualItems()"
          :key="visible[v.index]!.key"
          class="row data"
          :class="{ selected: visible[v.index]!.key === selectedKey }"
          :style="{ transform: `translateY(${v.start}px)`, gridTemplateColumns: template }"
          @click="emit('select', visible[v.index]!)"
        >
          <template v-for="c in columns" :key="c.key">
            <div v-if="c.key === 'name'" class="cell name mono"><HealthDot :health="visible[v.index]!.health" :label="visible[v.index]!.status" />{{ visible[v.index]!.name }}</div>
            <div v-else-if="c.type === 'status'" class="cell status" :class="statusTone(visible[v.index]!)">{{ visible[v.index]!.status }}</div>
            <div v-else-if="c.type === 'age'" class="cell num dim mono">{{ timeAgo(valueOf(visible[v.index]!, c.key), now) }}</div>
            <div v-else-if="c.type === 'time'" class="cell dim">{{ fmtTime(valueOf(visible[v.index]!, c.key)) }}</div>
            <div v-else-if="c.type === 'cpu'" class="cell num mono">{{ cpu(visible[v.index]!.cells.cpu) || '—' }}</div>
            <div v-else-if="c.type === 'mem'" class="cell num mono">{{ bytes(visible[v.index]!.cells.mem) || '—' }}</div>
            <div v-else-if="c.type === 'num'" class="cell num mono" :class="{ warn: c.key === 'restarts' && valueOf(visible[v.index]!, c.key) > 0 }">{{ valueOf(visible[v.index]!, c.key) ?? '' }}</div>
            <div v-else-if="c.type === 'list'" class="cell mono muted" :title="list(valueOf(visible[v.index]!, c.key))">{{ list(valueOf(visible[v.index]!, c.key)) }}</div>
            <div v-else-if="c.type === 'message'" class="cell message" :title="valueOf(visible[v.index]!, c.key)">{{ valueOf(visible[v.index]!, c.key) }}</div>
            <div v-else class="cell" :class="{ mono: c.type === 'mono', muted: c.key === 'namespace' }">{{ valueOf(visible[v.index]!, c.key) ?? '' }}</div>
          </template>
        </div>
      </div>
      <div v-if="visible.length === 0" class="empty">
        <AppIcon name="resources" :size="20" />
        <strong>{{ rows.size === 0 ? `No ${kind.label.toLowerCase()} in this scope.` : 'Nothing matches the filter.' }}</strong>
        <span v-if="rows.size > 0">The filter matches names, namespaces, statuses and every column. Esc clears it.</span>
      </div>
    </div>
    </div>
  </div>
</template>

<style scoped>
.table { height: 100%; min-height: 0; overflow: auto; background: var(--panel); }
.inner { position: relative; }
.row { display: grid; align-items: center; height: var(--row); }
.head { position: sticky; top: 0; z-index: 1; background: var(--panel-2); border-bottom: 1px solid var(--line-2); height: 36px; }
.th { background: none; border: 0; text-align: left; padding: 0 14px; color: var(--text-2); font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; line-height: 36px; }
.th:hover { color: var(--text-0); }
.th.on, .th.on:hover { color: var(--accent-2); }
:root[data-theme="light"] .th.on, :root[data-theme="light"] .th.on:hover { color: var(--accent); }
.th.right { text-align: right; }
.th .app-icon { margin-left: 5px; }
.data { position: absolute; top: 0; left: 0; right: 0; border-bottom: 1px solid var(--line); cursor: default; transition: background 80ms; }
.data:hover { background: var(--hover); }
.data.selected, .data.selected:hover { background: var(--accent-soft); box-shadow: inset 2.5px 0 0 var(--accent); }
.cell { padding: 0 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; color: var(--text-1); font-size: 13px; }
.name { display: flex; align-items: center; gap: 10px; color: var(--text-0); font-weight: 600; transition: color 80ms; }
.data:hover .name { color: var(--accent-2); }
:root[data-theme="light"] .data:hover .name { color: var(--accent); }
.data.selected .name, .data.selected:hover .name { color: var(--text-0); }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.status.bad { color: var(--bad); font-weight: 500; }
.status.warn { color: var(--warn); font-weight: 500; }
.warn { color: var(--warn); }
.message { color: var(--text-1); }
</style>
