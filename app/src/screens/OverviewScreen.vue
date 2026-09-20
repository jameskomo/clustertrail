<script setup lang="ts">
// The incident starting point. Opens with a sentence about the cluster, not a
// row of tiles; the charts, heatmap and attention lists sit underneath.
import { computed, nextTick, ref } from 'vue'
import { ui, select } from '../state'
import { useResource } from '../composables/useResource'
import type { Row } from '../api/types'
import { now } from '../composables/useClock'
import { timeAgo } from '../composables/useFormat'
import AppIcon from '../components/AppIcon.vue'
import HealthDot from '../components/HealthDot.vue'
import TrendChart from '../components/TrendChart.vue'
import Sparkline from '../components/Sparkline.vue'
import { useHistory } from '../composables/useHistory'
import { cpu, bytes } from '../composables/useFormat'

const cluster = computed(() => ui.cluster)
const ns = computed(() => ui.namespace)
const pods = useResource(computed(() => 'pods'), cluster, ns)
const deps = useResource(computed(() => 'deployments'), cluster, ns)
const nodes = useResource(computed(() => 'nodes'), cluster, computed(() => ''))
const events = useResource(computed(() => 'events'), cluster, ns)

const count = (m: Map<string, Row>, f: (r: Row) => boolean) => [...m.values()].filter(f).length
const podsBad = computed(() => count(pods.rows.value, (r) => r.health === 'bad'))
const podsWarn = computed(() => count(pods.rows.value, (r) => r.health === 'warn'))
const depsBad = computed(() => count(deps.rows.value, (r) => r.health !== 'ok'))
const nodesBad = computed(() => count(nodes.rows.value, (r) => r.health !== 'ok'))
const hourAgo = computed(() => now.value - 3600_000)
const warnings = computed(() => [...events.rows.value.values()].filter((r) => r.health === 'bad' && Date.parse(r.createdAt) > hourAgo.value).sort((a, b) => (a.createdAt < b.createdAt ? 1 : -1)))

const synced = computed(() => pods.synced.value && deps.synced.value && nodes.synced.value)
const { samples } = useHistory(cluster, computed(() => (cluster.value ? 'cluster' : null)))
const range = ref<5 | 15 | 30>(30)
const inRange = computed(() => { const cutoff = now.value - range.value * 60_000; return samples.value.filter((s) => Date.parse(s.t) >= cutoff) })
const cpuPts = computed(() => inRange.value.map((s) => ({ t: s.t, v: s.cpu })))
const memPts = computed(() => inRange.value.map((s) => ({ t: s.t, v: s.mem })))
const topBy = (cell: 'cpu' | 'mem') => [...pods.rows.value.values()].filter((r) => r.cells[cell] > 0).sort((a, b) => b.cells[cell] - a.cells[cell]).slice(0, 6)
const topCpu = computed(() => topBy('cpu'))
const topMem = computed(() => topBy('mem'))
const openPod = (r: Row) => { ui.screen = 'pods'; select('pods', r.key, r.namespace, r.name, 'overview') }
const capacity = computed(() => { let c = 0, m = 0; for (const n of nodes.rows.value.values()) { c += n.cells.cpuCapacity ?? 0; m += n.cells.memCapacity ?? 0 } return { c, m } })
const scope = computed(() => (ns.value ? `${ns.value} in ${cluster.value}` : cluster.value ?? ''))
const problems = computed(() => {
  const p: { n: number; what: string; tone: 'bad' | 'warn' }[] = []
  if (nodesBad.value) p.push({ n: nodesBad.value, what: nodesBad.value === 1 ? 'node not ready' : 'nodes not ready', tone: 'bad' })
  if (podsBad.value) p.push({ n: podsBad.value, what: podsBad.value === 1 ? 'pod failing' : 'pods failing', tone: 'bad' })
  if (depsBad.value) p.push({ n: depsBad.value, what: depsBad.value === 1 ? 'deployment degraded' : 'deployments degraded', tone: 'bad' })
  if (podsWarn.value) p.push({ n: podsWarn.value, what: podsWarn.value === 1 ? 'pod pending' : 'pods pending', tone: 'warn' })
  return p
})
const healthy = computed(() => problems.value.every((p) => p.tone !== 'bad'))

const attention = computed(() => {
  const list: { kind: string; row: Row; why: string }[] = []
  for (const r of deps.rows.value.values()) if (r.health !== 'ok') list.push({ kind: 'deployments', row: r, why: `${r.cells.ready ?? 0} of ${r.cells.replicas ?? '?'} ready` })
  for (const r of pods.rows.value.values()) if (r.health === 'bad') list.push({ kind: 'pods', row: r, why: `${r.status}${r.cells.restarts ? `, restarted ${r.cells.restarts} times` : ''}` })
  for (const r of nodes.rows.value.values()) if (r.health !== 'ok') list.push({ kind: 'nodes', row: r, why: r.status ?? '' })
  return list
})
const open = (kind: string, r: Row) => { ui.screen = kind; select(kind, r.key, r.namespace, r.name, kind === 'pods' ? 'logs' : 'events') }
const noun = (kind: string) => (kind === 'deployments' ? 'deployment' : kind === 'pods' ? 'pod' : 'node')

const nodesReady = computed(() => [...nodes.rows.value.values()].filter((n) => n.health === 'ok').length)
const podsTotal = computed(() => pods.rows.value.size)
const podsRunning = computed(() => [...pods.rows.value.values()].filter((p) => p.health === 'ok').length)
const warningCount = computed(() => warnings.value.length)
const failingPods = computed(() => [...pods.rows.value.values()].filter((r) => r.health === 'bad').sort((a, b) => a.name.localeCompare(b.name)))

// The headline sentence, as text segments so numbers and names render in mono.
// The lead clause carries the health tone so state reads at a glance.
type Seg = { t: string; mono?: boolean }
const tone = computed<'bad' | 'warn' | 'ok'>(() => (podsBad.value || depsBad.value || nodesBad.value ? 'bad' : podsWarn.value ? 'warn' : 'ok'))
const sentence = computed<{ lead: Seg[]; tail: Seg[] }>(() => {
  const clauses: Seg[][] = []
  let lead: Seg[]
  if (podsBad.value) {
    const extra = podsBad.value - 1
    lead = [{ t: failingPods.value[0]?.name ?? '', mono: true }]
    if (extra > 0) lead.push({ t: ' and ' }, { t: String(extra), mono: true }, { t: extra === 1 ? ' more pod are failing' : ' more pods are failing' })
    else lead.push({ t: ' is failing' })
    if (depsBad.value) clauses.push([{ t: String(depsBad.value), mono: true }, { t: depsBad.value === 1 ? ' deployment degraded' : ' deployments degraded' }])
    if (nodesBad.value) clauses.push([{ t: String(nodesBad.value), mono: true }, { t: nodesBad.value === 1 ? ' node not ready' : ' nodes not ready' }])
  } else if (depsBad.value || nodesBad.value) {
    lead = []
    if (depsBad.value) lead.push({ t: String(depsBad.value), mono: true }, { t: depsBad.value === 1 ? ' deployment degraded' : ' deployments degraded' })
    if (nodesBad.value) {
      if (lead.length) lead.push({ t: '; ' })
      lead.push({ t: String(nodesBad.value), mono: true }, { t: nodesBad.value === 1 ? ' node not ready' : ' nodes not ready' })
    }
  } else if (!podsTotal.value) {
    lead = [{ t: 'No pods running in this scope' }]
  } else if (podsWarn.value) {
    lead = [
      { t: String(podsRunning.value), mono: true }, { t: ' of ' }, { t: String(podsTotal.value), mono: true },
      { t: ' pods ready across ' }, { t: String(nodesReady.value), mono: true }, { t: nodesReady.value === 1 ? ' node' : ' nodes' },
    ]
    clauses.push([{ t: String(podsWarn.value), mono: true }, { t: podsWarn.value === 1 ? ' pod pending' : ' pods pending' }])
  } else {
    lead = [
      { t: 'All ' }, { t: String(podsTotal.value), mono: true }, { t: podsTotal.value === 1 ? ' pod ready across ' : ' pods ready across ' },
      { t: String(nodesReady.value), mono: true }, { t: nodesReady.value === 1 ? ' node' : ' nodes' },
    ]
  }
  if (warningCount.value) clauses.push([{ t: String(warningCount.value), mono: true }, { t: warningCount.value === 1 ? ' warning in the last hour' : ' warnings in the last hour' }])
  if (!clauses.length) return { lead, tail: [] }
  return { lead, tail: [{ t: ', with ' }, ...clauses.flatMap((c, i) => (i ? [{ t: ', ' }, ...c] : c))] }
})

// Pod heatmap sorting
const allPods = computed(() => [...pods.rows.value.values()].sort((a, b) => a.name.localeCompare(b.name)))

// Signal strip: utilization, readiness, restarts and warning rate, each with
// the smallest honest visualization that carries the point.
const lastOf = (pts: { t: string; v: number }[]) => pts.at(-1)?.v ?? 0
const cpuPct = computed(() => (capacity.value.c ? Math.min(100, (lastOf(cpuPts.value) / capacity.value.c) * 100) : null))
const memPct = computed(() => (capacity.value.m ? Math.min(100, (lastOf(memPts.value) / capacity.value.m) * 100) : null))
const pctTone = (p: number | null) => (p === null ? '' : p >= 95 ? 'bad' : p >= 80 ? 'warn' : '')
const nodesTotal = computed(() => nodes.rows.value.size)

const restartsOf = (r: Row) => Number(r.cells.restarts ?? 0)
const restartsTotal = computed(() => [...pods.rows.value.values()].reduce((n, r) => n + restartsOf(r), 0))
const topRestarts = computed(() => [...pods.rows.value.values()].filter((r) => restartsOf(r) > 0).sort((a, b) => restartsOf(b) - restartsOf(a)).slice(0, 6))
const maxRestarts = computed(() => Math.max(...topRestarts.value.map(restartsOf), 1))
const openPodLogs = (r: Row) => { ui.screen = 'pods'; select('pods', r.key, r.namespace, r.name, 'logs') }

// Warnings per five minutes over the last hour, oldest first.
const warnHist = computed(() => {
  const buckets = new Array<number>(12).fill(0)
  for (const w of warnings.value) {
    const i = Math.floor((Date.parse(w.createdAt) - hourAgo.value) / 300_000)
    if (i >= 0 && i < 12) buckets[i]!++
  }
  return buckets
})
const maxBucket = computed(() => Math.max(...warnHist.value, 1))

const go = (screen: string) => { ui.screen = screen; ui.selection = null }
// App.vue clears the filter on every screen change, so it goes in after the
// navigation settles.
const goWarnings = () => {
  go('events')
  nextTick(() => { ui.filter = 'warning' })
}
</script>

<template>
  <div class="screen overview">
    <Teleport defer to="#screen-actions">
      <span v-if="scope" class="scope mono">{{ scope }}</span>
      <span class="pill" :class="healthy ? 'ok' : 'bad'">
        <HealthDot :health="healthy ? 'ok' : 'bad'" />
        {{ healthy ? 'Healthy' : 'Action required' }}
      </span>
    </Teleport>

    <div class="body">
      <div v-if="pods.error.value" class="err mono">{{ pods.error.value }}</div>

      <p v-if="!synced" class="hero">Reading cluster telemetry…</p>
      <template v-else>
        <p class="hero" :class="tone">
          <span class="lead" :class="tone"><template v-for="(s, i) in sentence.lead" :key="'l' + i"><span v-if="s.mono" class="mono">{{ s.t }}</span><template v-else>{{ s.t }}</template></template></span><template v-for="(s, i) in sentence.tail" :key="'t' + i"><span v-if="s.mono" class="mono">{{ s.t }}</span><template v-else>{{ s.t }}</template></template>
        </p>
        <section class="strip">
          <div class="sig card">
            <span class="lbl">CPU utilization</span>
            <span class="val"><span class="mono big" :class="pctTone(cpuPct)">{{ cpuPct === null ? '—' : Math.round(cpuPct) + '%' }}</span><span class="dim sub">{{ cpu(lastOf(cpuPts)) || '0m' }} of {{ cpu(capacity.c) || '—' }}</span></span>
            <Sparkline :points="cpuPts" />
          </div>
          <div class="sig card">
            <span class="lbl">Memory utilization</span>
            <span class="val"><span class="mono big" :class="pctTone(memPct)">{{ memPct === null ? '—' : Math.round(memPct) + '%' }}</span><span class="dim sub">{{ bytes(lastOf(memPts)) || '0' }} of {{ bytes(capacity.m) || '—' }}</span></span>
            <Sparkline :points="memPts" style="--chart: var(--chart-2)" />
          </div>
          <button class="sig card link" @click="go('pods')">
            <span class="lbl">Pods ready</span>
            <span class="val"><span class="mono big" :class="podsBad ? 'bad' : podsWarn ? 'warn' : ''">{{ podsRunning }}<span class="dim of">/{{ podsTotal }}</span></span><span class="dim sub" v-if="podsBad">{{ podsBad }} failing</span></span>
            <span class="segbar"><span class="sb ok" :style="{ flex: podsRunning || 0.001 }"></span><span v-if="podsWarn" class="sb warn" :style="{ flex: podsWarn }"></span><span v-if="podsBad" class="sb bad" :style="{ flex: podsBad }"></span></span>
          </button>
          <button class="sig card link" @click="go('pods')">
            <span class="lbl">Restarts</span>
            <span class="val"><span class="mono big" :class="restartsTotal > 20 ? 'bad' : restartsTotal ? 'warn' : ''">{{ restartsTotal }}</span><span class="dim sub" v-if="topRestarts[0]">worst: {{ topRestarts[0].name }}</span></span>
            <span class="hist"><span v-for="r in topRestarts" :key="r.key" class="hb" :class="restartsOf(r) > 10 ? 'bad' : 'warn'" :style="{ height: Math.max(10, (restartsOf(r) / maxRestarts) * 100) + '%' }"></span></span>
          </button>
          <button class="sig card link" @click="goWarnings">
            <span class="lbl">Warnings, last hour</span>
            <span class="val"><span class="mono big" :class="warningCount > 10 ? 'bad' : warningCount ? 'warn' : ''">{{ warningCount }}</span><span class="dim sub">5 min buckets</span></span>
            <span class="hist"><span v-for="(b, i) in warnHist" :key="i" class="hb" :class="{ warn: b }" :style="{ height: Math.max(8, (b / maxBucket) * 100) + '%' }"></span></span>
          </button>
          <div class="sig card">
            <span class="lbl">Nodes ready</span>
            <span class="val"><span class="mono big" :class="nodesBad ? 'bad' : ''">{{ nodesReady }}<span class="dim of">/{{ nodesTotal }}</span></span><span class="dim sub" v-if="nodesBad">{{ nodesBad }} not ready</span></span>
            <span class="segbar"><span class="sb ok" :style="{ flex: nodesReady || 0.001 }"></span><span v-if="nodesBad" class="sb bad" :style="{ flex: nodesBad }"></span></span>
          </div>
        </section>

        <div class="dash-grid" :class="{ 'no-metrics': !pods.metrics.value }">
          <div v-if="pods.metrics.value" class="dash-col">
            <section class="card">
              <div class="panel-head">
                <h2>Cluster usage</h2>
                <div class="actions">
                  <div class="seg" role="radiogroup" aria-label="Time range">
                    <button v-for="r in [5, 15, 30]" :key="r" :class="{ on: range === r }" role="radio" :aria-checked="range === r" @click="range = r as 5 | 15 | 30">{{ r }} min</button>
                  </div>
                </div>
              </div>
              <div class="charts">
                <TrendChart :points="cpuPts" title="CPU, all pods" :format="(v: number) => cpu(v) || '0m'" :context="capacity.c ? `of ${cpu(capacity.c)} available` : ''" :height="110" />
                <TrendChart :points="memPts" title="Memory, all pods" :format="(v: number) => bytes(v) || '0'" :context="capacity.m ? `of ${bytes(capacity.m)} available` : ''" :height="110" style="--chart: var(--chart-2)" />
              </div>
            </section>

            <section class="card">
              <div class="panel-head">
                <h2>Top consumers</h2>
              </div>
              <div class="tops">
                <div class="consumers">
                  <h3>By CPU</h3>
                  <button v-for="r in topCpu" :key="r.key" class="bar" @click="openPod(r)">
                    <span class="mono name">{{ r.name }}</span>
                    <span class="track"><span class="fill" :style="{ width: (100 * r.cells.cpu / (topCpu[0]?.cells.cpu || 1)) + '%' }"></span></span>
                    <span class="mono val">{{ cpu(r.cells.cpu) }}</span>
                  </button>
                </div>
                <div class="consumers">
                  <h3>By memory</h3>
                  <button v-for="r in topMem" :key="r.key" class="bar" @click="openPod(r)">
                    <span class="mono name">{{ r.name }}</span>
                    <span class="track"><span class="fill" :style="{ width: (100 * r.cells.mem / (topMem[0]?.cells.mem || 1)) + '%' }"></span></span>
                    <span class="mono val">{{ bytes(r.cells.mem) }}</span>
                  </button>
                </div>
              </div>
            </section>
          </div>

          <section class="card heatmap">
            <div class="panel-head">
              <h2>Pods</h2>
              <div class="actions"><span class="pill">{{ allPods.length }} total</span></div>
            </div>
            <div class="heatmap-body">
              <div v-if="!allPods.length" class="empty"><strong>No active pods in this scope.</strong></div>
              <div v-else class="heatmap-grid">
                <div
                  v-for="p in allPods"
                  :key="p.key"
                  class="pod-square"
                  :class="p.health"
                  :title="`${p.name} (${p.status})`"
                  @click="openPod(p)"
                ></div>
              </div>
            </div>
            <div class="legend" v-if="allPods.length">
              <span class="legend-item"><span class="dot ok"></span>Healthy</span>
              <span class="legend-item"><span class="dot warn"></span>Pending</span>
              <span class="legend-item"><span class="dot bad"></span>Failing</span>
            </div>
          </section>
        </div>

        <div class="cols">
          <section class="card attention">
            <div class="panel-head">
              <h2>Needs attention</h2>
              <div class="actions"><span v-if="attention.length" class="pill bad">{{ attention.length }}</span></div>
            </div>
            <div class="scroll">
              <div v-if="!attention.length" class="empty">
                <AppIcon name="check" :size="16" />
                <strong>Nothing is failing, degraded or unschedulable.</strong>
                <span>When something breaks it shows here first, with a link straight to its logs.</span>
              </div>
              <button v-for="a in attention" :key="a.kind + a.row.key" class="line" @click="open(a.kind, a.row)">
                <HealthDot :health="a.row.health" />
                <span class="mono name">{{ a.row.name }}</span>
                <span class="muted what">{{ noun(a.kind) }}<template v-if="a.row.namespace"> in {{ a.row.namespace }}</template></span>
                <span class="why" :class="a.row.health">{{ a.why }}</span>
              </button>
            </div>
          </section>

          <section class="card restarts">
            <div class="panel-head">
              <h2>Top restarts</h2>
              <div class="actions"><span v-if="restartsTotal" class="pill warn">{{ restartsTotal }} total</span></div>
            </div>
            <div class="scroll">
              <div v-if="!topRestarts.length" class="empty">
                <AppIcon name="check" :size="16" />
                <strong>No restarts recorded.</strong>
                <span>Containers that keep crashing land here, worst first.</span>
              </div>
              <button v-for="r in topRestarts" :key="r.key" class="line" @click="openPodLogs(r)">
                <HealthDot :health="r.health" />
                <span class="mono name">{{ r.name }}</span>
                <span class="muted what">{{ r.status }}</span>
                <span class="why" :class="restartsOf(r) > 10 ? 'bad' : 'warn'">{{ restartsOf(r) }}×</span>
              </button>
            </div>
          </section>

          <section class="card feed">
            <div class="panel-head">
              <h2>Warnings</h2>
              <div class="actions"><span class="hint">Last 60 min</span></div>
            </div>
            <div class="scroll">
              <div v-if="!warnings.length" class="empty">
                <AppIcon name="info" :size="16" />
                <strong>No warning events.</strong>
                <span>Failed probes, image pulls, scheduling and restarts appear here as they happen.</span>
              </div>
              <div v-for="w in warnings.slice(0, 80)" :key="w.key" class="ev">
                <div class="top"><span class="reason">{{ w.cells.reason }}</span><span class="mono obj muted">{{ w.cells.object }}</span><span class="dim when mono">{{ timeAgo(w.createdAt, now) }}</span></div>
                <div class="msg">{{ w.cells.message }}</div>
              </div>
            </div>
          </section>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.body { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; gap: 14px; padding: 16px 20px 20px; }
.hero { margin: 0; padding: 0 2px; font-size: 14px; color: var(--text-2); }
.hero .mono { color: var(--text-1); }
.hero .lead { font-weight: 500; color: var(--text-1); }
.hero .lead.bad, .hero .lead.bad .mono { color: var(--bad); }
.hero .lead.warn, .hero .lead.warn .mono { color: var(--warn); }
.scope { color: var(--text-2); font-size: 12.5px; }
.hint { font-size: 12px; color: var(--text-3); }
.err { color: var(--bad); }

.dash-grid { display: grid; grid-template-columns: 1.3fr 1fr; gap: 14px; align-items: stretch; }
.dash-grid.no-metrics { grid-template-columns: 1fr; }
.dash-col { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
.charts { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; padding: 14px 16px 16px; }
.tops { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; padding: 12px 16px 14px; }
.consumers { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.consumers h3 { margin: 0 0 8px; font-size: 12px; font-weight: 500; color: var(--text-2); }
.bar { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(40px, 1fr) 56px; align-items: center; gap: 12px; width: 100%; height: 26px; padding: 0; background: none; border: 0; border-radius: var(--r-sm); color: var(--text-0); text-align: left; }
.bar:hover .name { color: var(--text-0); text-decoration: underline; }
.bar .name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 12px; color: var(--text-1); }
.track { height: 5px; background: var(--panel-2); border-radius: 2px; overflow: hidden; }
.fill { display: block; height: 100%; background: var(--chart-a); border-radius: 2px; }
.val { text-align: right; font-size: 12px; color: var(--text-1); }

.heatmap { display: flex; flex-direction: column; min-height: 0; }
.heatmap-body { padding: 14px 16px; flex: 1; overflow: auto; min-height: 120px; }
.heatmap-grid { display: flex; flex-wrap: wrap; gap: 3px; align-content: flex-start; }
.pod-square { width: 13px; height: 13px; border-radius: 2px; background: var(--raised); border: 1px solid var(--line); cursor: pointer; transition: transform 80ms var(--ease), border-color 80ms var(--ease); }
.pod-square:hover { transform: scale(1.2); border-color: var(--text-2); z-index: 1; }
.pod-square.ok { background: var(--ok); border-color: transparent; }
.pod-square.warn { background: var(--warn); border-color: transparent; }
.pod-square.bad { background: var(--bad); border-color: transparent; animation: pulse 2s infinite; }
.legend { display: flex; gap: 14px; padding: 9px 16px; border-top: 1px solid var(--line); font-size: 12px; color: var(--text-2); }
.legend-item { display: inline-flex; align-items: center; gap: 6px; }
.legend-item .dot { display: block; width: 8px; height: 8px; border-radius: 50%; }
.legend-item .dot.ok { background: var(--ok); }
.legend-item .dot.warn { background: var(--warn); }
.legend-item .dot.bad { background: var(--bad); }

.cols { display: grid; grid-template-columns: minmax(0, 5fr) minmax(0, 4fr) minmax(0, 4fr); grid-template-rows: minmax(0, 1fr); gap: 14px; flex: 1; min-height: 240px; }
.attention, .feed, .restarts { display: flex; flex-direction: column; min-height: 0; overflow: hidden; }

/* Signal strip */
.strip { display: grid; grid-template-columns: repeat(auto-fit, minmax(175px, 1fr)); gap: 14px; }
.sig { display: flex; flex-direction: column; gap: 7px; padding: 11px 14px 12px; text-align: left; color: var(--text-0); cursor: default; }
button.sig { font: inherit; letter-spacing: inherit; }
.sig.link { cursor: pointer; transition: border-color 120ms var(--ease); }
.sig.link:hover { border-color: var(--line-2); }
.lbl { font-size: 12px; font-weight: 500; color: var(--text-2); }
.val { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
.big { font-size: 19px; font-weight: 600; letter-spacing: -0.02em; }
.big.warn { color: var(--warn); }
.big.bad { color: var(--bad); }
.of { font-size: 13px; font-weight: 500; }
.sub { font-size: 11.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.segbar { display: flex; height: 5px; border-radius: 3px; overflow: hidden; background: var(--panel-2); }
.sb.ok { background: var(--ok); }
.sb.warn { background: var(--warn); }
.sb.bad { background: var(--bad); }
.hist { display: flex; align-items: flex-end; gap: 2px; height: 26px; }
.hb { flex: 1; min-height: 2px; border-radius: 1px; background: var(--line-2); }
.hb.warn { background: var(--warn); }
.hb.bad { background: var(--bad); }
.scroll { flex: 1; min-height: 0; overflow: auto; padding: 4px 8px 8px; }
.line { display: grid; grid-template-columns: auto minmax(0, 1.6fr) minmax(0, 1fr) auto; align-items: center; gap: 12px; width: 100%; height: 34px; padding: 0 8px; background: none; border: 0; border-radius: var(--r-sm); text-align: left; color: var(--text-0); }
.line:hover { background: var(--hover); }
.line .name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 13px; }
.what { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 12px; }
.why { font-size: 12px; white-space: nowrap; font-weight: 500; }
.why.bad { color: var(--bad); }
.why.warn { color: var(--warn); }
.ev { padding: 8px; border-bottom: 1px solid var(--line); }
.ev:last-child { border-bottom: 0; }
.top { display: flex; align-items: center; gap: 10px; }
.reason { color: var(--bad); font-size: 12px; font-weight: 600; white-space: nowrap; }
.obj { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.when { margin-left: auto; font-size: 11px; }
.msg { margin-top: 2px; color: var(--text-1); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
