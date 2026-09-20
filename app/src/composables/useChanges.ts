// The Change gate from the UI side: propose, show, decide.
import { ref } from 'vue'
import { engine } from '../api/socket'
import type { Change, Op, AuditEntry } from '../api/types'
import { toast } from './useToasts'

export const pendingChange = ref<Change | null>(null)
export const changes = ref<Change[]>([])
export const audit = ref<AuditEntry[]>([])

export async function propose(cluster: string, intent: string, plan: Op[]) {
  try {
    const m = await engine.request({ type: 'change.propose', cluster, intent, plan })
    pendingChange.value = m.change ?? null
  } catch (e: any) {
    toast(e.message, 'bad')
  }
}

export async function decide(id: string, decision: 'approve' | 'reject', note = '') {
  try {
    const m = await engine.request({ type: 'change.decide', name: id, decision, note })
    const c = m.change!
    pendingChange.value = null
    if (c.status === 'applied') toast(`Applied: ${c.intent}`, 'ok')
    else if (c.status === 'failed') toast(`Failed: ${c.error}`, 'bad')
    else toast(`Rejected: ${c.intent}`, 'muted')
    await refreshChanges()
    return c
  } catch (e: any) {
    toast(e.message, 'bad')
  }
}

export async function refreshChanges() {
  try {
    const m = await engine.request({ type: 'change.list' })
    changes.value = m.changes ?? []
    audit.value = m.audit ?? []
  } catch { /* engine down; the socket reconnects */ }
}
