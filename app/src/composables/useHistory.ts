// Metrics history for one key ("pod/ns/name", "node/name", "cluster"),
// refreshed on the engine's 15 s cadence while the component is alive.
import { ref, watch, onScopeDispose, type Ref } from 'vue'
import { engine } from '../api/socket'

export interface Sample { t: string; cpu: number; mem: number }

export function useHistory(cluster: Ref<string | null>, key: Ref<string | null>) {
  const samples = ref<Sample[]>([])
  let timer: number | undefined
  const load = async () => {
    if (!cluster.value || !key.value) { samples.value = []; return }
    try {
      const m = await engine.request({ type: 'history', cluster: cluster.value, name: key.value })
      samples.value = ((m as any).samples ?? []) as Sample[]
    } catch { /* engine reconnecting */ }
  }
  watch([cluster, key], () => { samples.value = []; load() }, { immediate: true })
  timer = window.setInterval(load, 15_000)
  onScopeDispose(() => window.clearInterval(timer))
  return { samples, reload: load }
}
