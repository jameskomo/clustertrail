// The full object behind the selected row, fetched on demand and parsed.
// Rows carry only what a table needs; the drawer needs the spec.
import { ref, watch, onScopeDispose, type Ref } from 'vue'
import { parse as parseYaml } from 'yaml'
import { engine } from '../api/socket'

export function useObject(cluster: Ref<string | null>, kind: Ref<string | null>, namespace: Ref<string | undefined>, name: Ref<string | null>) {
  const obj = ref<any>(null)
  const error = ref<string | null>(null)
  let live = true
  onScopeDispose(() => (live = false))

  const load = async () => {
    obj.value = null
    error.value = null
    if (!cluster.value || !kind.value || !name.value) return
    const want = `${kind.value}/${namespace.value}/${name.value}`
    try {
      const m = await engine.request({ type: 'get', cluster: cluster.value, kind: kind.value, namespace: namespace.value, name: name.value })
      if (!live || want !== `${kind.value}/${namespace.value}/${name.value}`) return // selection moved on
      obj.value = parseYaml(m.object ?? '')
    } catch (e: any) {
      if (live) error.value = e.message
    }
  }
  watch([cluster, kind, namespace, name], load, { immediate: true })
  return { obj, error, reload: load }
}

/** Reads one key out of a Secret, base64-decoded, for reveal-on-demand. */
export async function readSecretKey(cluster: string, namespace: string, secret: string, key: string): Promise<string> {
  const m = await engine.request({ type: 'get', cluster, kind: 'secrets', namespace, name: secret })
  const o = parseYaml(m.object ?? '')
  const raw = o?.data?.[key]
  if (raw === undefined) throw new Error(`${secret} has no key ${key}`)
  try { return new TextDecoder().decode(Uint8Array.from(atob(raw), (c) => c.charCodeAt(0))) } catch { return String(raw) }
}

/** Reads one key out of a ConfigMap. */
export async function readConfigKey(cluster: string, namespace: string, cm: string, key: string): Promise<string> {
  const m = await engine.request({ type: 'get', cluster, kind: 'configmaps', namespace, name: cm })
  const o = parseYaml(m.object ?? '')
  const v = o?.data?.[key]
  if (v === undefined) throw new Error(`${cm} has no key ${key}`)
  return String(v)
}
