// The demo engine: the real interface, driven by recorded answers instead of
// a live cluster.
//
// This exists so someone can judge the product before downloading anything.
// It is deliberately inert: there is no socket, no network, no cluster, and
// every write is refused rather than faked, because pretending a change
// applied would teach the wrong thing about a product whose whole argument is
// that nothing is applied without review.
import type { ClientMsg, ServerMsg } from '../api/types'
import fixtures from './fixtures.json'

type Listener = (m: ServerMsg) => void

const f = fixtures as any

/** True when the page was opened as a demo rather than against an engine. */
export function isDemo(): boolean {
  const p = new URLSearchParams(location.search)
  if (p.has('demo')) return p.get('demo') !== '0'
  // A build published as the demo sets this at compile time.
  return import.meta.env.VITE_DEMO === '1'
}

/** When the fixtures were captured, for the banner. */
export const recordedAt: string = f.recordedAt ?? ''

// The recording is a moment in time. Shifting every timestamp by the same
// amount keeps the relative story intact ("rolled out 4 minutes before it
// started crashing") while making the ages read as now rather than as
// whenever this was captured.
const shift = Date.now() - Date.parse(f.recordedAt ?? new Date().toISOString())
const retime = (iso: string | undefined): string =>
  iso ? new Date(Date.parse(iso) + shift).toISOString() : (iso as any)

const rows = (kind: string) =>
  (f.snapshots?.[kind] ?? []).map((r: any) => ({ ...r, createdAt: retime(r.createdAt) }))

const timeline = () => {
  const t = f.timeline
  if (!t) return null
  return { ...t, since: retime(t.since), items: (t.items ?? []).map((i: any) => ({ ...i, at: retime(i.at) })) }
}

const changes = () => (f.changes ?? []).map((c: any) => ({ ...c, createdAt: retime(c.createdAt), decidedAt: retime(c.decidedAt) }))
const auditEntries = () => (f.audit ?? []).map((e: any) => ({ ...e, at: retime(e.at) }))
const history = (key: string) => (f.history?.[key] ?? []).map((s: any) => ({ ...s, t: retime(s.t) }))

const refusal =
  'This is the demo. It is driven by recorded data, so nothing can be changed. ' +
  'Download ClusterTrail to point it at your own clusters.'

class DemoSocket {
  readonly status = { value: 'open' as const }
  readonly version = { value: 'demo' }
  private listeners = new Set<Listener>()

  constructor() {
    // Let the interface mount before its first frame arrives.
    setTimeout(() => this.emit({ type: 'hello', version: 'demo' }), 0)
  }

  private emit(m: ServerMsg) {
    for (const l of this.listeners) l(m)
  }

  /** Answers as the engine would, on the next tick. */
  private answer(m: ClientMsg): ServerMsg | null {
    const id = m.id
    switch (m.type) {
      case 'clusters':
        return { type: 'clusters', clusters: f.clusters ?? [] }

      case 'subscribe': {
        if (m.kind === 'logs') {
          const lines = f.logs?.[`${m.namespace}/${m.name}`] ?? [
            `${new Date().toISOString()} this is the demo; logs are recorded, not live`,
          ]
          return { type: 'log', id, lines, eof: true }
        }
        let list = rows(m.kind ?? '')
        if (m.namespace) list = list.filter((r: any) => !r.namespace || r.namespace === m.namespace)
        const hasMetrics = list.some((r: any) => r.cells?.cpu !== undefined)
        return { type: 'snapshot', id, rows: list, metricsAvailable: hasMetrics }
      }

      case 'unsubscribe':
      // A terminal never opens here, because exec.start is refused below.
      // These arrive only if one somehow did, and they carry no reply.
      case 'exec.input':
      case 'exec.resize':
      case 'exec.stop':
        return null

      case 'events':
        return { type: 'events', id, events: f.events?.[`${m.kind}/${m.namespace}/${m.name}`] ?? findEvents(m) }

      case 'get':
        return { type: 'object', id, object: f.objects?.[`${m.kind}/${m.namespace}/${m.name}`] ?? findObject(m) }

      case 'history':
        return { type: 'history', id, samples: history(m.name ?? 'cluster') }

      case 'timeline':
        return { type: 'timeline', id, timeline: timeline() } as ServerMsg

      case 'change.list':
        return { type: 'changes', id, changes: changes(), audit: auditEntries() }

      case 'helm.list':
        return { type: 'releases', id, releases: f.releases ?? [] }

      case 'helm.get':
        return { type: 'release', id, release: f.release ?? null } as ServerMsg

      case 'forward.list':
        return { type: 'forwards', id, forwards: [] }

      // Proposing is a server-side dry run: it asks the API server what a
      // change would do and writes nothing. That is why the demo can answer
      // one, and why the refusal belongs on the decision instead. Otherwise a
      // visitor never sees the review dialog, which is the product's whole
      // argument.
      case 'change.propose': {
        const want = m.plan?.[0]
        const recorded = (Array.isArray(f.pending) ? f.pending : f.pending ? [f.pending] : []) as any[]
        const match = want && recorded.find((c) => {
          const got = c.plan?.[0]
          return got && got.op === want.op && got.kind === want.kind &&
            got.namespace === want.namespace && got.name === want.name
        })
        if (match) {
          return { type: 'change', id, change: { ...match, createdAt: retime(match.createdAt) } } as ServerMsg
        }
        const offers = recorded
          .map((c) => c.plan?.[0])
          .filter(Boolean)
          .map((o: any) => `${o.op} on ${o.kind}/${o.name}`)
        return {
          type: 'error',
          id,
          message:
            (offers.length
              ? `This demo can dry-run ${offers.length === 1 ? 'one change' : 'these changes'}: ${offers.join('; ')}. `
              : '') +
            'A proposal is a dry run against a live API server, and there is no cluster behind this page.',
        }
      }

      // What refers to a Secret or ConfigMap. A read, and the panel is named
      // after a differentiator, so leaving it unanswered had the demo
      // demonstrating the feature failing.
      case 'consumers':
        return {
          type: 'consumers',
          id,
          consumers: f.consumers?.[`${m.kind}/${m.namespace}/${m.name}`] ?? [],
        } as ServerMsg

      // Planning a rollback is two reads and a manifest split: it fetches the
      // current revision and the target one and says what would be applied.
      // Rolling back is the write, and there is no message for it here.
      case 'helm.rollback.plan': {
        const plan = f.rollback?.[`${m.namespace}/${m.name}/${m.revision}`]
        return plan
          ? ({ type: 'rollback', id, rollback: plan } as ServerMsg)
          : {
              type: 'error',
              id,
              message:
                'This demo carries one recorded rollback plan, for shop-frontend back to revision 1. ' +
                'Everything here is recorded, so no other revision can be read from a cluster.',
            }
      }

      // A drift scan is a read, so the demo can answer it from the recording.
      // Capturing the result into Git is a write, and stays refused below.
      case 'drift.scan':
        return f.drift
          ? ({ type: 'drift', id, drift: { ...f.drift, scannedAt: retime(f.drift.scannedAt) } } as ServerMsg)
          : { type: 'error', id, message: refusal }

      case 'explain.status':
        return { type: 'explain.key', id, hasKey: false }

      // Gathering the evidence is local and sends nothing, but there is no
      // cluster here to gather from, and no key, so the pane stops earlier.
      case 'explain.preview':
        return {
          type: 'error',
          id,
          message: 'The explain pane needs a model key and a cluster to read. Run ClusterTrail against your own.',
        }

      // Everything that would change something, or reach outside the browser.
      // change.propose is answered above: it is a dry run and writes nothing,
      // so the refusal lands on the decision instead.
      case 'change.decide':
      case 'drift.capture':
      case 'explain':
      case 'explain.key':
      case 'exec.start':
      case 'forward.start':
      case 'forward.stop':
        return { type: 'error', id, message: refusal }

      default:
        return null
    }
  }

  send(m: ClientMsg) {
    const reply = this.answer(m)
    if (reply) setTimeout(() => this.emit(reply), 40)
  }

  request(m: ClientMsg): Promise<ServerMsg> {
    const id = m.id ?? `demo-${Math.random().toString(36).slice(2)}`
    const reply = this.answer({ ...m, id })
    return new Promise((resolve, reject) => {
      setTimeout(() => {
        if (!reply) return reject(new Error(refusal))
        if (reply.type === 'error') return reject(new Error(reply.message))
        resolve(reply)
      }, 40)
    })
  }

  subscribe(m: ClientMsg & { id: string }) { this.send(m) }
  unsubscribe(_id: string) { /* nothing to stop */ }

  on(l: Listener): () => void {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }
}

/**
 * Says so when an object was not recorded.
 *
 * This used to fall back to any recorded object of the same kind so the
 * drawer was never blank, which meant clicking one pod showed a different
 * pod's spec. A blank drawer is confusing; the wrong object is wrong.
 */
function findObject(m: ClientMsg): string {
  const kind = m.kind ?? ''
  const recorded = Object.keys(f.objects ?? {})
    .filter((k) => k.startsWith(`${kind}/`))
    .map((k) => k.slice(kind.length + 1))
  return (
    `# ${m.namespace ? `${m.namespace}/` : ''}${m.name}\n` +
    '#\n# This object is not in the demo recording, so there is nothing to show.\n' +
    (recorded.length ? `# Recorded for this kind: ${recorded.join(', ')}\n` : '') +
    '#\n# Run ClusterTrail against your own cluster and every object is here.\n'
  )
}

function findEvents(m: ClientMsg): any[] {
  for (const [key, list] of Object.entries(f.events ?? {})) {
    if (key.includes(`/${m.name}`)) return list as any[]
  }
  return []
}

export const demoSocket = new DemoSocket()
