// Records a live engine's answers into fixtures the demo mode replays.
//
// The demo shows real data from a real cluster, captured once, rather than
// invented data. Anything that would identify a private cluster is rewritten
// here, not in the browser, so the fixtures are safe to publish.
//
//   node scripts/record-demo.mjs > app/src/demo/fixtures.json
import { scrub } from './scrub-demo.mjs'
const ENGINE = process.env.ENGINE ?? '127.0.0.1:7777'
const TOKEN = process.env.TOKEN ?? 'dev'
const CLUSTER = process.env.CLUSTER ?? 'kind-clustertrail'
const NS = process.env.NAMESPACE ?? 'shop'

const KINDS = [
  'pods', 'deployments', 'statefulsets', 'daemonsets', 'replicasets', 'jobs', 'cronjobs',
  'configmaps', 'secrets', 'services', 'ingresses', 'persistentvolumeclaims',
  'serviceaccounts', 'roles', 'rolebindings', 'clusterroles', 'clusterrolebindings',
  'nodes', 'namespaces', 'events', 'customresourcedefinitions',
]

const ws = new WebSocket(`ws://${ENGINE}/ws?token=${TOKEN}`)
const waiting = new Map()
const out = { recordedAt: new Date().toISOString(), cluster: 'demo-cluster', snapshots: {}, history: {} }

const ask = (msg, expect) => new Promise((resolve) => {
  const id = msg.id ?? `r${waiting.size}-${Math.random().toString(36).slice(2, 7)}`
  waiting.set(id, { resolve, expect })
  ws.send(JSON.stringify({ ...msg, id }))
  setTimeout(() => { if (waiting.delete(id)) resolve(null) }, 12000)
})

// A log subscription answers with a stream of frames rather than one reply,
// so it needs a listener rather than the ask/resolve pair.
const streams = new Set()

ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (!m.id) return
  for (const fn of streams) fn(m)
  const entry = waiting.get(m.id)
  if (!entry) return
  if (m.type === 'error') { waiting.delete(m.id); entry.resolve(null); return }
  if (entry.expect && m.type !== entry.expect) return
  waiting.delete(m.id)
  entry.resolve(m)
}

ws.onopen = async () => {
  out.clusters = [{ name: 'demo-cluster', server: 'https://demo.example:6443', current: true }]

  for (const kind of KINDS) {
    const namespaced = !['nodes', 'namespaces', 'clusterroles', 'clusterrolebindings', 'customresourcedefinitions'].includes(kind)
    const m = await ask({ type: 'subscribe', cluster: CLUSTER, kind, namespace: namespaced ? '' : '' }, 'snapshot')
    out.snapshots[kind] = m?.rows ?? []
    if (kind === 'pods') out.metricsAvailable = !!m?.metricsAvailable
    process.stderr.write(`  ${kind}: ${out.snapshots[kind].length}\n`)
  }

  const timeline = await ask({ type: 'timeline', cluster: CLUSTER, namespace: NS, minutes: 180 }, 'timeline')
  out.timeline = timeline?.timeline ?? null
  const releases = await ask({ type: 'helm.list', cluster: CLUSTER, namespace: '' }, 'releases')
  out.releases = releases?.releases ?? []
  if (out.releases.length) {
    const r = out.releases[0]
    const detail = await ask({ type: 'helm.get', cluster: CLUSTER, namespace: r.namespace, name: r.name }, 'release')
    out.release = detail?.release ?? null
  }
  const changes = await ask({ type: 'change.list' }, 'changes')
  out.changes = changes?.changes ?? []
  out.audit = changes?.audit ?? []

  for (const key of ['cluster']) {
    const h = await ask({ type: 'history', cluster: CLUSTER, name: key }, 'history')
    out.history[key] = h?.samples ?? []
  }

  // One representative object of each kind we can open, plus its events.
  out.objects = {}
  out.events = {}
  out.logs = {}
  for (const kind of ['pods', 'deployments', 'configmaps', 'secrets', 'services', 'nodes']) {
    const row = (out.snapshots[kind] ?? [])[0]
    if (!row) continue
    const obj = await ask({ type: 'get', cluster: CLUSTER, kind, namespace: row.namespace, name: row.name }, 'object')
    if (obj?.object) out.objects[`${kind}/${row.key}`] = obj.object
    const ev = await ask({ type: 'events', cluster: CLUSTER, kind: '', namespace: row.namespace ?? '', name: row.name }, 'events')
    out.events[`${kind}/${row.key}`] = ev?.events ?? []
  }
  const pod = (out.snapshots.pods ?? []).find((p) => p.health === 'ok') ?? (out.snapshots.pods ?? [])[0]
  if (pod) {
    out.history[`pod/${pod.key}`] = (await ask({ type: 'history', cluster: CLUSTER, name: `pod/${pod.key}` }, 'history'))?.samples ?? []
  }

  // Every object behind a row in the namespace being recorded, not just the
  // first of each kind. A drawer with nothing in it is the commonest way the
  // demo disappoints somebody, and the recording is the only chance to fill
  // it.
  for (const kind of ['pods', 'deployments', 'configmaps', 'secrets', 'services']) {
    for (const row of out.snapshots?.[kind] ?? []) {
      if (row.namespace !== NS) continue
      const key = `${kind}/${row.namespace}/${row.name}`
      if (out.objects[key]) continue
      const obj = await ask({ type: 'get', cluster: CLUSTER, kind, namespace: row.namespace, name: row.name }, 'object')
      if (obj?.object) out.objects[key] = obj.object
    }
  }

  // Container logs. out.logs was declared and never filled, so a re-record
  // silently dropped them and every Logs tab fell back to a placeholder line.
  // One pod keeps a full tail and the rest keep enough to look real, rather
  // than the bundle carrying several copies of the same access log.
  {
    const want = (out.snapshots.pods ?? []).filter((r) => r.namespace === NS)
    const seen = new Map()
    const collect = (m) => {
      if (m.type === 'log' && m.lines?.length) seen.set(m.id, (seen.get(m.id) ?? []).concat(m.lines))
    }
    streams.add(collect)
    want.forEach((p, i) => ws.send(JSON.stringify({
      type: 'subscribe', id: `log${i}`, cluster: CLUSTER, kind: 'logs',
      namespace: p.namespace, name: p.name, container: '', tail: 500,
    })))
    await new Promise((r) => setTimeout(r, 6000))
    streams.delete(collect)
    want.forEach((p, i) => ws.send(JSON.stringify({ type: 'unsubscribe', id: `log${i}` })))

    const got = want
      .map((p, i) => ({ key: `${p.namespace}/${p.name}`, lines: seen.get(`log${i}`) ?? [] }))
      .filter((e) => e.lines.length)
    const richest = Math.max(0, ...got.map((e) => e.lines.length))
    for (const e of got) {
      out.logs[e.key] = e.lines.length === richest ? e.lines.slice(-500) : e.lines.slice(-60)
    }
  }

  // What refers to each Secret and ConfigMap, so the "Used by" panel has
  // something to show.
  out.consumers = {}
  for (const [kind, apiKind] of [['secrets', 'Secret'], ['configmaps', 'ConfigMap']]) {
    for (const row of out.snapshots?.[kind] ?? []) {
      if (row.namespace !== NS) continue
      const c = await ask(
        { type: 'consumers', cluster: CLUSTER, kind: apiKind, namespace: row.namespace, name: row.name },
        'consumers',
      )
      if (c?.consumers) out.consumers[`${apiKind}/${row.namespace}/${row.name}`] = c.consumers
    }
  }

  // A rollback plan for the first release found, back one revision. Planning
  // is a read: it fetches both revisions and says what would be applied.
  const rel = (out.releases ?? [])[0]
  if (rel && rel.revision > 1) {
    const to = rel.revision - 1
    const rb = await ask(
      { type: 'helm.rollback.plan', cluster: CLUSTER, namespace: rel.namespace, name: rel.name, revision: to },
      'rollback',
    )
    if (rb?.rollback) out.rollback = { [`${rel.namespace}/${rel.name}/${to}`]: rb.rollback }
  }

  // A proposal, left pending. Proposing is a dry run and writes nothing, so
  // this is safe to record and it is what lets the demo show the review
  // dialog, which is the product's central claim.
  const replicas = Number(process.env.SCALE_TO ?? 8)
  const pending = await ask(
    {
      type: 'change.propose', cluster: CLUSTER,
      intent: `Scale checkout to ${replicas} for the Friday sale`,
      plan: [{ op: 'scale', kind: 'deployments', namespace: NS, name: 'checkout', replicas }],
    },
    'change',
  )
  out.pending = []
  if (pending?.change) out.pending.push(pending.change)

  // And the proposal a rollback produces, so that whole path works rather
  // than showing the plan and then refusing the change it leads to.
  const rbPlan = Object.values(out.rollback ?? {})[0]
  if (rbPlan) {
    const keys = { Deployment: 'deployments', Service: 'services', ConfigMap: 'configmaps', Secret: 'secrets', Ingress: 'ingresses', StatefulSet: 'statefulsets', DaemonSet: 'daemonsets', Job: 'jobs', CronJob: 'cronjobs' }
    const ops = rbPlan.objects
      .map((o) => (keys[o.kind] ? { op: 'apply', kind: keys[o.kind], namespace: o.namespace, name: o.name, yaml: o.yaml } : null))
      .filter(Boolean)
    if (ops.length) {
      const rbChange = await ask(
        { type: 'change.propose', cluster: CLUSTER, intent: `Roll ${rbPlan.name} back from revision ${rbPlan.from} to ${rbPlan.to}`, plan: ops },
        'change',
      )
      if (rbChange?.change) out.pending.push(rbChange.change)
    }
  }

  // A drift scan against the sample repository shipped with the project.
  // Without this the demo's Drift screen, one of the four things the product
  // is about, has nothing to show a visitor.
  const drift = await ask(
    { type: 'drift.scan', cluster: CLUSTER, path: 'deploy/sample-repo', namespace: NS, unmanaged: true },
    'drift',
  )
  if (drift?.drift) out.drift = drift.drift

  // Scrub anything that names a real environment or a real person. This is
  // published, so the rule is allowlist by field and then sweep, not two
  // string replacements. See scripts/scrub-demo.mjs.
  const clean = scrub(out, {
    sweep: [
      [CLUSTER, 'demo-cluster'],
      [process.env.HOME ?? '/home/unknown', '/home/you'],
      [process.env.USER ?? '\u0000never', 'demo-user'],
    ],
  })
  process.stdout.write(JSON.stringify(clean))
  process.stderr.write('recorded\n')
  ws.close()
  process.exit(0)
}
setTimeout(() => { process.stderr.write('timed out\n'); process.exit(1) }, 120000)
