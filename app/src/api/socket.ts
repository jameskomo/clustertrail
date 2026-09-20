// The one socket to the engine. Reconnects with backoff; on reconnect every
// live subscription is re-sent so the UI receives a fresh snapshot.
import { ref, readonly } from 'vue'
import type { ClientMsg, ServerMsg } from './types'
import { demoSocket, isDemo } from '../demo/socket'

export type Status = 'connecting' | 'open' | 'closed'
type Listener = (m: ServerMsg) => void

const url = () => {
  const p = new URLSearchParams(location.search)
  const token = p.get('token') ?? import.meta.env.VITE_ENGINE_TOKEN ?? 'dev'
  const host = p.get('engine') ?? import.meta.env.VITE_ENGINE_ADDR ?? '127.0.0.1:7777'
  return `ws://${host}/ws?token=${encodeURIComponent(token)}`
}

let seq = 0
export const nextId = (prefix = 'r') => `${prefix}-${++seq}`

class EngineSocket {
  readonly status = ref<Status>('connecting')
  readonly version = ref('')
  private ws: WebSocket | null = null
  private listeners = new Set<Listener>()
  private subscriptions = new Map<string, ClientMsg>()
  private pending = new Map<string, { resolve: (m: ServerMsg) => void; reject: (e: Error) => void }>()
  private backoff = 250
  private queue: ClientMsg[] = []

  constructor() { this.connect() }

  private connect() {
    this.status.value = 'connecting'
    const ws = new WebSocket(url())
    this.ws = ws
    ws.onopen = () => {
      this.status.value = 'open'
      this.backoff = 250
      for (const m of this.subscriptions.values()) ws.send(JSON.stringify(m))
      for (const m of this.queue.splice(0)) ws.send(JSON.stringify(m))
    }
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data) as ServerMsg
      if (m.type === 'hello') this.version.value = m.version ?? ''
      if (m.id && this.pending.has(m.id)) {
        const p = this.pending.get(m.id)!
        this.pending.delete(m.id)
        m.type === 'error' ? p.reject(new Error(m.message)) : p.resolve(m)
        return
      }
      for (const l of this.listeners) l(m)
    }
    ws.onclose = () => {
      this.status.value = 'closed'
      for (const p of this.pending.values()) p.reject(new Error('engine disconnected'))
      this.pending.clear()
      setTimeout(() => this.connect(), this.backoff)
      this.backoff = Math.min(this.backoff * 2, 5000)
    }
    ws.onerror = () => ws.close()
  }

  send(m: ClientMsg) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(m))
    else this.queue.push(m)
  }

  /** One request, one answer (or an error). */
  request(m: ClientMsg): Promise<ServerMsg> {
    const id = m.id ?? nextId('q')
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.send({ ...m, id })
    })
  }

  /** Subscribe and keep the subscription across reconnects. */
  subscribe(m: ClientMsg & { id: string }) {
    this.subscriptions.set(m.id, m)
    this.send(m)
  }

  unsubscribe(id: string) {
    if (!this.subscriptions.delete(id)) return
    this.send({ type: 'unsubscribe', id })
  }

  on(l: Listener): () => void {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }
}

// In demo mode the interface talks to recorded fixtures instead of an engine,
// so the page can be published without a backend or a cluster behind it.
export const engine: EngineSocket = isDemo() ? (demoSocket as unknown as EngineSocket) : new EngineSocket()
export const engineStatus = readonly(engine.status)
