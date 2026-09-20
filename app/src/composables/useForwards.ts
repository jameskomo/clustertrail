import { ref } from 'vue'
import { engine } from '../api/socket'
import type { Forward } from '../api/types'
import { toast } from './useToasts'

export const forwards = ref<Forward[]>([])

export async function refreshForwards() {
  try { forwards.value = (await engine.request({ type: 'forward.list' })).forwards ?? [] } catch { /* reconnecting */ }
}

export async function startForward(cluster: string, namespace: string, pod: string, port: number, localPort = 0) {
  try {
    const m = await engine.request({ type: 'forward.start', cluster, namespace, name: pod, port, localPort })
    forwards.value = m.forwards ?? []
    const f = forwards.value.find((f) => f.pod === pod && f.port === port)
    if (f) toast(`Forwarding 127.0.0.1:${f.localPort} → ${pod}:${port}`, 'ok', 6000)
  } catch (e: any) { toast(e.message, 'bad') }
}

export async function stopForward(id: string) {
  try { forwards.value = (await engine.request({ type: 'forward.stop', name: id })).forwards ?? [] } catch (e: any) { toast(e.message, 'bad') }
}
