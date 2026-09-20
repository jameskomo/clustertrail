// The demo is published. Everything in it was recorded from a real cluster on
// a real machine, and a recording carries more than it looks like it does:
// the audit log names whoever approved each change, review notes are free
// text somebody typed, and paths name a home directory.
//
// Replacing two environment variables is not enough, which is how the first
// published demo went out carrying an operator's name 146 times, an earlier
// internal name for the product, and an absolute path from the machine it was
// built on. This rewrites by field, then sweeps for anything left.
//
// Run standalone to clean a recording that already exists:
//   node scripts/scrub-demo.mjs app/src/demo/fixtures.json

// Fields that always name a person. `actor` is deliberately not here: it
// also carries kubelet, deployment-controller and kubectl-client-side-apply,
// and those are the whole point of the timeline. Replacing every actor with
// one name erases the feature the screenshot exists to show. A real person's
// name in `actor` is caught by the sweep instead.
const IDENTITY_FIELDS = new Set(['decidedBy', 'user', 'author'])
const DROPPED_FIELDS = new Set(['note'])

// redactSecretYaml blanks the values in a recorded Secret.
//
// A recording captures the owner's own view of their own cluster, which is
// deliberately not redacted: in the product, you are allowed to read your own
// Secrets. A published demo is a different audience. Today's fixtures happen
// to hold deliberately fake values, which is luck rather than a control, and
// this is the control.
//
// Line-based rather than a YAML parse, because the fixtures store objects as
// rendered text and re-emitting them through a parser would reformat every
// one of them.
function redactSecretYaml(text) {
  if (!/^kind: Secret$/m.test(text)) return text
  const out = []
  let inData = false
  let skippingBlock = false
  for (const line of text.split('\n')) {
    // kubectl writes the whole object, Secret values included, into this
    // annotation on every apply. Redacting data and leaving it redacts
    // nothing.
    if (/^\s*kubectl\.kubernetes\.io\/last-applied-configuration:/.test(line)) {
      skippingBlock = true
      continue
    }
    if (skippingBlock) {
      if (/^\s{6,}/.test(line)) continue
      skippingBlock = false
    }
    if (/^(data|stringData):\s*$/.test(line)) { inData = true; out.push(line); continue }
    if (inData) {
      const kv = line.match(/^(\s{2,})([\w.\-]+):\s*\S/)
      if (kv) { out.push(`${kv[1]}${kv[2]}: REDACTED-IN-THE-DEMO`); continue }
      if (/^\S/.test(line)) inData = false
    }
    out.push(line)
  }
  // An `annotations:` whose only child was the annotation just removed is
  // left dangling, and renders as `annotations: null`.
  const pruned = []
  for (let i = 0; i < out.length; i++) {
    const here = out[i]
    const key = here.match(/^(\s*)annotations:\s*$/)
    if (key) {
      const nextIndent = (out[i + 1] ?? '').match(/^(\s*)\S/)
      if (!nextIndent || nextIndent[1].length <= key[1].length) continue
    }
    pruned.push(here)
  }
  return pruned.join('\n')
}

export function scrub(value, opts = {}) {
  const { cluster = 'demo-cluster', sweep = [] } = opts

  const walk = (node) => {
    if (Array.isArray(node)) return node.map(walk)
    if (node === null || typeof node !== 'object') return node
    const out = {}
    for (const [k, v] of Object.entries(node)) {
      if (DROPPED_FIELDS.has(k) && typeof v === 'string') {
        // A review note is whatever somebody typed at the time. There is no
        // safe way to guess whether it is publishable.
        out[k] = 'Reviewed and approved.'
        continue
      }
      if (k === 'cluster' && typeof v === 'string') { out[k] = cluster; continue }
      if (IDENTITY_FIELDS.has(k)) {
        if (typeof v === 'string') { out[k] = 'demo-user'; continue }
        if (v && typeof v === 'object') { out[k] = { ...walk(v), name: 'demo-user' }; continue }
      }
      out[k] = walk(v)
    }
    return out
  }

  const cleaned = walk(value)
  if (cleaned && typeof cleaned === 'object' && cleaned.objects) {
    for (const [k, v] of Object.entries(cleaned.objects)) {
      if (typeof v === 'string') cleaned.objects[k] = redactSecretYaml(v)
    }
  }

  let text = JSON.stringify(cleaned)
  // The sweep catches what field names cannot: a cluster name embedded in a
  // metrics key, a node named after the machine, a home directory in a path.
  for (const [pattern, replacement] of sweep) {
    text = text.replaceAll(pattern, replacement)
  }
  return JSON.parse(text)
}

// Standalone: scrub a file in place.
if (process.argv[1] && process.argv[1].endsWith('scrub-demo.mjs') && process.argv[2]) {
  const { readFileSync, writeFileSync } = await import('node:fs')
  const file = process.argv[2]
  const before = JSON.parse(readFileSync(file, 'utf8'))
  // What to sweep for is supplied, not written down here. A scrub list that
  // spells out the operator's username and every cluster they have used is
  // itself the disclosure it exists to prevent, and it would sit in the
  // repository long after the recording it was written for.
  //
  //   SCRUB_CLUSTERS=kind-foo,kind-bar SCRUB_NAMES=alice \
  //     node scripts/scrub-demo.mjs app/src/demo/fixtures.json
  //
  // Clusters default to the one record-demo.mjs reads, names to $USER.
  const list = (v) => (v ?? '').split(',').map((s) => s.trim()).filter(Boolean)
  const clusters = list(process.env.SCRUB_CLUSTERS ?? process.env.CLUSTER ?? 'kind-clustertrail')
  const names = list(process.env.SCRUB_NAMES ?? process.env.USER)

  const sweep = []
  for (const c of clusters) {
    // kind names its nodes after the cluster, so those leak it too.
    const bare = c.replace(/^kind-/, '')
    sweep.push([`${bare}-control-plane`, 'demo-control-plane'])
    sweep.push([`${bare}-worker`, 'demo-worker'])
    sweep.push([c, 'demo-cluster'])
  }
  for (const n of names) sweep.push([n, 'demo-user'])
  sweep.push([process.env.HOME ?? '/home/unknown', '/home/you'])

  const after = scrub(before, { sweep })
  writeFileSync(file, JSON.stringify(after))
  process.stderr.write(`scrubbed ${file}\n`)
}
