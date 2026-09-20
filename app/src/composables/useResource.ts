// Holds the rows of one subscription. Snapshot replaces, delta merges.
// Rows live in a Map keyed by namespace/name so upserts and deletes are O(1)
// and idempotent, matching the engine's contract.
import { ref, shallowRef, watch, onScopeDispose, triggerRef, type Ref } from 'vue'
import { engine, nextId } from '../api/socket'
import type { Row } from '../api/types'

export function useResource(kind: Ref<string | null>, cluster: Ref<string | null>, namespace: Ref<string>) {
  const id = nextId('sub')
  const rows = shallowRef(new Map<string, Row>())
  const synced = ref(false)
  // True while the table is showing the rows we saw last time rather than
  // rows the cluster just confirmed.
  const cached = ref(false)
  const cachedAt = ref<string | null>(null)
  const error = ref<string | null>(null)
  const metrics = ref(false)

  const off = engine.on((m) => {
    if (m.id !== id) return
    if (m.type === 'snapshot') {
      rows.value = new Map((m.rows ?? []).map((r) => [r.key, { ...r, cells: r.cells ?? {} }]))
      cached.value = !!m.cached
      cachedAt.value = m.cachedAt ?? null
      // A cached or recorded snapshot carries no metricsAvailable flag, but
      // the rows themselves say whether metrics arrived. Believe the rows,
      // rather than showing "no metrics" beside a column full of numbers.
      const rowsHaveMetrics = (m.rows ?? []).some((r) => r.cells?.cpu !== undefined)
      metrics.value = m.cached ? rowsHaveMetrics : !!m.metricsAvailable || rowsHaveMetrics
      synced.value = true
      error.value = null
    } else if (m.type === 'delta') {
      for (const r of m.upsert ?? []) rows.value.set(r.key, { ...r, cells: r.cells ?? {} })
      for (const k of m.delete ?? []) rows.value.delete(k)
      triggerRef(rows)
    } else if (m.type === 'error') {
      error.value = m.message ?? 'error'
    }
  })

  watch([kind, cluster, namespace], ([k, c, ns]) => {
    rows.value = new Map()
    synced.value = false
    cached.value = false
    cachedAt.value = null
    error.value = null
    if (!c || !k) return engine.unsubscribe(id)
    engine.subscribe({ type: 'subscribe', id, cluster: c, kind: k, namespace: ns })
  }, { immediate: true })

  onScopeDispose(() => { off(); engine.unsubscribe(id) })
  return { rows, synced, error, metrics, cached, cachedAt }
}
