// Mirrors engine/internal/api/frames.go. Hand-kept for now; generation from
// the Go structs is planned. Arrays and maps may be omitted when empty.

export interface ClusterInfo { name: string; server: string; current: boolean }

export type Health = '' | 'ok' | 'warn' | 'bad'

export interface Row {
  key: string
  name: string
  namespace?: string
  createdAt: string
  status?: string
  health?: Health
  cells: Record<string, any>
}

export interface EventRow { type: string; reason: string; message: string; count: number; source?: string; lastSeen: string }

export interface Op { op: 'apply' | 'scale' | 'restart' | 'delete'; kind: string; namespace?: string; name: string; yaml?: string; replicas?: number }

export interface Change {
  id: string
  cluster: string
  author: { type: string; name: string }
  intent: string
  plan: Op[]
  diff: string
  blast: { namespaces: string[]; kinds: string[]; objects: number; hasSecrets: boolean; deletes: number }
  status: 'pending' | 'approved' | 'rejected' | 'applied' | 'failed'
  createdAt: string
  decidedAt?: string
  decidedBy?: string
  note?: string
  error?: string
}

export interface AuditEntry { seq: number; at: string; actor: string; action: string; changeId?: string; cluster: string; detail?: any; prev: string; hash: string }

export interface Forward { id: string; cluster: string; namespace: string; pod: string; port: number; localPort: number; error?: string }

export interface ClientMsg {
  type: string
  id?: string
  cluster?: string
  kind?: string
  namespace?: string
  name?: string
  container?: string
  previous?: boolean
  tail?: number
  intent?: string
  plan?: Op[]
  decision?: 'approve' | 'reject'
  note?: string
  data?: string
  cols?: number
  rows?: number
  port?: number
  localPort?: number
  path?: string
  includeUnmanaged?: boolean
  includeSecretValues?: boolean
  dir?: string
  branch?: string
  push?: boolean
  objects?: { kind: string; namespace?: string; name: string; yaml: string }[]
  revision?: number
  minutes?: number
  provider?: string
  key?: string
  // The payload the person reviewed, sent back so what leaves the machine
  // is exactly what they were shown.
  evidence?: { object: string; events: string[]; logs: string[] }
}

export interface ServerMsg {
  type: 'hello' | 'clusters' | 'snapshot' | 'delta' | 'events' | 'log' | 'object' | 'change' | 'changes' | 'term' | 'forwards' | 'history' | 'drift' | 'capture' | 'consumers' | 'timeline' | 'releases' | 'release' | 'rollback' | 'explain' | 'explain.key' | 'error'
  id?: string
  cluster?: string
  message?: string
  version?: string
  clusters?: ClusterInfo[]
  rows?: Row[]
  upsert?: Row[]
  delete?: string[]
  metricsAvailable?: boolean
  cached?: boolean
  cachedAt?: string
  events?: EventRow[]
  lines?: string[]
  eof?: boolean
  object?: string
  change?: Change
  changes?: Change[]
  audit?: AuditEntry[]
  data?: string
  forwards?: Forward[]
  samples?: { t: string; cpu: number; mem: number }[]
  drift?: any
  capture?: any
  consumers?: { kind: string; kindName: string; namespace: string; name: string; via: string; detail: string }[]
  timeline?: any
  releases?: any[]
  release?: any
  rollback?: any
  answer?: string
  evidence?: { object: string; events: string[]; logs: string[] }
  hasKey?: boolean
  keyFromEnv?: boolean
}
