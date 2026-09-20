// Live counts per kind for the rail. Only kinds you have opened are counted,
// plus the few the overview always watches, so the rail never fans out a
// watch on every resource in the cluster.
import { ref } from 'vue'
import type { Row } from '../api/types'

export const counts = ref<Record<string, { total: number; bad: number }>>({})

export function report(kind: string, rows: Map<string, Row>) {
  let bad = 0
  for (const r of rows.values()) if (r.health === 'bad') bad++
  counts.value = { ...counts.value, [kind]: { total: rows.size, bad } }
}

export function clearCounts() { counts.value = {} }
