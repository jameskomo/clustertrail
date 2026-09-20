import { ref, shallowRef, watch, onScopeDispose, triggerRef, type Ref } from 'vue'
import { engine, nextId } from '../api/socket'

const MAX_LINES = 5000

export function useLogs(cluster: Ref<string | null>, namespace: Ref<string>, pod: Ref<string>, container: Ref<string>, previous: Ref<boolean>) {
  const id = nextId('log')
  const lines = shallowRef<string[]>([])
  const eof = ref(false)
  const error = ref<string | null>(null)
  const paused = ref(false)
  let buffer: string[] = []

  const off = engine.on((m) => {
    if (m.id !== id) return
    if (m.type === 'log') {
      const incoming = m.lines ?? []
      if (paused.value) {
        // A paused view still receives everything the container writes. The
        // buffer is capped like the live one, so leaving a chatty pod paused
        // over lunch costs a fixed amount rather than all of it.
        buffer = buffer.concat(incoming)
        if (buffer.length > MAX_LINES) buffer = buffer.slice(buffer.length - MAX_LINES)
      } else {
        lines.value.push(...incoming)
        if (lines.value.length > MAX_LINES) lines.value.splice(0, lines.value.length - MAX_LINES)
        triggerRef(lines)
      }
      if (m.eof) eof.value = true
    } else if (m.type === 'error') error.value = m.message ?? 'error'
  })

  watch(paused, (p) => {
    if (!p && buffer.length) {
      // concat rather than push(...buffer): spreading an array as arguments
      // throws RangeError somewhere past a hundred thousand elements.
      lines.value = lines.value.concat(buffer).slice(-MAX_LINES)
      buffer = []
      triggerRef(lines)
    }
  })

  watch([cluster, namespace, pod, container, previous], ([c, ns, p, ct, prev]) => {
    lines.value = []; buffer = []; eof.value = false; error.value = null
    if (!c || !p || !ct) return engine.unsubscribe(id)
    engine.subscribe({ type: 'subscribe', id, cluster: c, kind: 'logs', namespace: ns, name: p, container: ct, previous: prev, tail: 500 })
  }, { immediate: true })

  onScopeDispose(() => { off(); engine.unsubscribe(id) })
  return { lines, eof, error, paused }
}
