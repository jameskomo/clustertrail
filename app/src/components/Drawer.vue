<script setup lang="ts">
// The detail drawer: overview, events, logs, YAML and terminal for one
// object, with every action routed through the Change gate.
import { computed, ref, watch } from 'vue'
import { ui, closeDrawer, select } from '../state'
import { kindByKey } from '../kinds'
import type { Row } from '../api/types'
import { engine } from '../api/socket'
import { now } from '../composables/useClock'
import { cpu, bytes, timeAgo } from '../composables/useFormat'
import { propose } from '../composables/useChanges'
import { parse as parseYaml } from 'yaml'
import { startForward } from '../composables/useForwards'
import HealthDot from './HealthDot.vue'
import EventsList from './EventsList.vue'
import LogView from './LogView.vue'
import YamlEditor from './YamlEditor.vue'
import TerminalView from './TerminalView.vue'
import TrendChart from './TrendChart.vue'
import ContainerDetail from './ContainerDetail.vue'
import { useObject } from '../composables/useObject'
import ExplainPanel from './ExplainPanel.vue'
import AppIcon from './AppIcon.vue'
import { useHistory } from '../composables/useHistory'

const props = defineProps<{ row: Row | null }>()
const sel = computed(() => ui.selection)
const spec = computed(() => (sel.value ? kindByKey(sel.value.kind) : undefined))
const cluster = computed(() => ui.cluster ?? '')
const containers = computed<string[]>(() => props.row?.cells.containers ?? [])
const historyKey = computed(() => (!sel.value ? null : sel.value.kind === 'pods' ? `pod/${sel.value.key}` : sel.value.kind === 'nodes' ? `node/${sel.value.name}` : null))
const { samples } = useHistory(cluster, historyKey)
// The full object, for the container detail. Workload kinds carry a pod
// template, so the same component serves pods and their owners.
const wantsContainers = computed(() => ['pods', 'deployments', 'statefulsets', 'daemonsets', 'replicasets', 'jobs'].includes(sel.value?.kind ?? ''))
const { obj } = useObject(cluster, computed(() => (wantsContainers.value ? sel.value?.kind ?? null : null)), computed(() => sel.value?.namespace), computed(() => (wantsContainers.value ? sel.value?.name ?? null : null)))
const cpuPts = computed(() => samples.value.map((s) => ({ t: s.t, v: s.cpu })))
const memPts = computed(() => samples.value.map((s) => ({ t: s.t, v: s.mem })))

const tabs = computed(() => {
  const t: { key: typeof ui.drawerTab; label: string }[] = [{ key: 'overview', label: 'Overview' }, { key: 'events', label: 'Events' }, { key: 'explain', label: 'Explain' }]
  if (spec.value?.logs) t.push({ key: 'logs', label: 'Logs' })
  t.push({ key: 'yaml', label: 'YAML' })
  if (spec.value?.exec) t.push({ key: 'terminal', label: 'Terminal' })
  return t
})
watch(tabs, (t) => { if (!t.some((x) => x.key === ui.drawerTab)) ui.drawerTab = 'overview' })

// YAML
const yamlText = ref('')
const yamlLoading = ref(false)
const yamlError = ref<string | null>(null)
const loadYaml = async () => {
  if (!sel.value) return
  yamlLoading.value = true; yamlError.value = null
  try {
    const m = await engine.request({ type: 'get', cluster: cluster.value, kind: sel.value.kind, namespace: sel.value.namespace, name: sel.value.name })
    yamlText.value = m.object ?? ''
  } catch (e: any) { yamlError.value = e.message } finally { yamlLoading.value = false }
}
watch([() => sel.value?.key, () => ui.drawerTab], ([k, t]) => { if (k && t === 'yaml') loadYaml() }, { immediate: true })

// What would break if this changed. Nothing else in the category answers
// this, and grepping manifests misses anything applied by hand.
const consumers = ref<{ kindName: string; kind: string; namespace: string; name: string; via: string; detail: string }[] | null>(null)
const loadConsumers = async () => {
  consumers.value = null
  const kindName = spec.value?.singular
  if (!sel.value || (kindName !== 'Secret' && kindName !== 'ConfigMap')) return
  try {
    const m = await engine.request({ type: 'consumers', cluster: cluster.value, kind: kindName, namespace: sel.value.namespace, name: sel.value.name })
    consumers.value = (m as any).consumers ?? []
  } catch { consumers.value = [] }
}
watch(() => sel.value?.key, loadConsumers, { immediate: true })
const openConsumer = (c: { kind: string; namespace: string; name: string }) => {
  ui.screen = c.kind
  select(c.kind, c.namespace ? `${c.namespace}/${c.name}` : c.name, c.namespace, c.name, 'overview')
}

// Data for ConfigMaps and Secrets, parsed from the object itself.
const data = ref<{ key: string; value: string; secret: boolean; shown: boolean }[]>([])
const loadData = async () => {
  data.value = []
  if (!sel.value || (sel.value.kind !== 'configmaps' && sel.value.kind !== 'secrets')) return
  try {
    const m = await engine.request({ type: 'get', cluster: cluster.value, kind: sel.value.kind, namespace: sel.value.namespace, name: sel.value.name })
    const obj = parseYaml(m.object ?? '') as { data?: Record<string, string>; binaryData?: Record<string, string>; stringData?: Record<string, string> }
    const secret = sel.value.kind === 'secrets'
    for (const [k, raw] of Object.entries(obj.data ?? {})) {
      let v = String(raw ?? '')
      if (secret) { try { v = new TextDecoder().decode(Uint8Array.from(atob(v), (c) => c.charCodeAt(0))) } catch { /* keep raw */ } }
      data.value.push({ key: k, value: v, secret, shown: !secret })
    }
    for (const k of Object.keys(obj.binaryData ?? {})) data.value.push({ key: k, value: '(binary)', secret, shown: true })
    data.value.sort((a, b) => a.key.localeCompare(b.key))
  } catch { /* shown in YAML tab */ }
}
watch(() => sel.value?.key, loadData, { immediate: true })

// Actions
const name = computed(() => sel.value?.name ?? '')
const ns = computed(() => sel.value?.namespace ?? '')
const kindKey = computed(() => sel.value?.kind ?? '')
const target = () => ({ kind: kindKey.value, namespace: ns.value, name: name.value })
const scaleTo = ref<number | null>(null)
const doScale = () => { if (scaleTo.value !== null) propose(cluster.value, `Scale ${spec.value?.singular} ${name.value} to ${scaleTo.value}`, [{ op: 'scale', ...target(), replicas: scaleTo.value }]) }
const doRestart = () => propose(cluster.value, `Rollout restart ${spec.value?.singular} ${name.value}`, [{ op: 'restart', ...target() }])
const doDelete = () => propose(cluster.value, `Delete ${spec.value?.singular} ${name.value}`, [{ op: 'delete', ...target() }])
const doSave = (yaml: string) => propose(cluster.value, `Edit ${spec.value?.singular} ${name.value}`, [{ op: 'apply', ...target(), yaml }])
const fwdPort = ref<number | null>(null)
const doForward = () => { if (fwdPort.value) startForward(cluster.value, ns.value, name.value, fwdPort.value) }

const facts = computed(() => {
  const r = props.row
  if (!r) return []
  const f: [string, string][] = []
  if (r.namespace) f.push(['Namespace', r.namespace])
  if (r.status) f.push(['Status', r.status])
  const c = r.cells
  const push = (label: string, v: any) => { if (v !== undefined && v !== '' && v !== null) f.push([label, Array.isArray(v) ? v.join(', ') : String(v)]) }
  push('Ready', c.ready); push('Restarts', c.restarts); push('Node', c.node); push('IP', c.ip); push('Owner', c.owner)
  push('Replicas', c.replicas); push('Images', c.images); push('Schedule', c.schedule); push('Type', c.type); push('Cluster IP', c.clusterIP)
  push('Ports', c.ports); push('Hosts', c.hosts); push('Capacity', c.capacity); push('Storage class', c.storageClass); push('Roles', c.roles); push('Version', c.version)
  f.push(['Created', `${new Date(r.createdAt).toLocaleString()} (${timeAgo(r.createdAt, now.value)} ago)`])
  return f
})
</script>

<template>
  <Transition name="drawer">
    <aside v-if="sel" class="drawer">
      <header>
        <HealthDot :health="row?.health" :label="row?.status" />
        <span class="name mono">{{ name }}</span>
        <span class="pill">{{ spec?.singular }}</span>
        <span v-if="ns" class="muted ns">in {{ ns }}</span>
        <button class="icon-btn close" title="Close (esc)" @click="closeDrawer"><AppIcon name="close" :size="14" /></button>
      </header>

      <nav class="tabs">
        <button v-for="t in tabs" :key="t.key" class="tab" :class="{ on: ui.drawerTab === t.key }" @click="ui.drawerTab = t.key">{{ t.label }}</button>
      </nav>

      <section v-if="ui.drawerTab === 'overview'" class="pane scroll">
        <div v-if="!row" class="empty"><strong>This object is no longer in the cluster.</strong></div>
        <template v-else>
          <div v-if="historyKey && samples.length >= 2" class="charts">
            <TrendChart :points="cpuPts" title="CPU" :format="(v: number) => cpu(v) || '0m'" :max="sel.kind === 'nodes' ? row.cells.cpuCapacity : undefined" :height="88" style="--chart: var(--chart-a)" />
            <TrendChart :points="memPts" title="Memory" :format="(v: number) => bytes(v) || '0'" :max="sel.kind === 'nodes' ? row.cells.memCapacity : undefined" :height="88" style="--chart: var(--chart-b)" />
          </div>
          <p v-else-if="historyKey" class="nometrics">{{ row.cells.cpu === undefined ? 'No metrics for this object. metrics-server reports nothing for a pod that is not running.' : 'Charting begins after two readings, about 30 seconds.' }}</p>
          <dl class="facts">
            <template v-for="[k, v] in facts" :key="k"><dt>{{ k }}</dt><dd class="mono">{{ v }}</dd></template>
          </dl>

          <ContainerDetail v-if="obj" :obj="obj" :cluster="cluster" :namespace="ns" />

          <div v-if="consumers" class="consumers">
            <h3>Used by</h3>
            <p v-if="!consumers.length" class="dim none">
              Nothing in this namespace refers to it. Safe to change, as far as the live objects show.
            </p>
            <button v-for="(c, i) in consumers" :key="i" class="consumer" @click="openConsumer(c)">
              <span class="mono who">{{ c.kindName }}/{{ c.name }}</span>
              <span class="pill">{{ c.via }}</span>
              <span class="muted how">{{ c.detail }}</span>
            </button>
          </div>

          <div v-if="data.length" class="data">
            <h3>Data</h3>
            <div v-for="d in data" :key="d.key" class="kv">
              <div class="k mono">{{ d.key }}</div>
              <pre class="v mono">{{ d.shown ? d.value : '•'.repeat(Math.min(24, d.value.length || 8)) }}</pre>
              <button v-if="d.secret" class="btn ghost sm" @click="d.shown = !d.shown">{{ d.shown ? 'Hide' : 'Reveal' }}</button>
            </div>
          </div>

          <div class="actions">
            <h3>Actions</h3>
            <div class="act-row" v-if="spec?.scalable">
              <input v-model.number="scaleTo" class="input num" type="number" min="0" :placeholder="String(row.cells.replicas ?? '')" />
              <button class="btn ghost" :disabled="scaleTo === null" @click="doScale"><AppIcon name="scale" :size="14" />Scale…</button>
            </div>
            <div class="act-row" v-if="spec?.forward">
              <input v-model.number="fwdPort" class="input num" type="number" min="1" max="65535" placeholder="port" />
              <button class="btn ghost" :disabled="!fwdPort" @click="doForward"><AppIcon name="forwards" :size="14" />Port-forward</button>
            </div>
            <div class="act-row">
              <button v-if="spec?.restarts" class="btn ghost" @click="doRestart"><AppIcon name="refresh" :size="14" />Rollout restart…</button>
              <button class="btn ghost danger" @click="doDelete"><AppIcon name="trash" :size="14" />Delete…</button>
            </div>
            <p class="dim hint">Each action shows its diff first and is written to the audit log.</p>
          </div>
        </template>
      </section>

      <section v-else-if="ui.drawerTab === 'explain'" class="pane scroll">
        <ExplainPanel :cluster="cluster" :kind="kindKey" :namespace="ns" :name="name" />
      </section>

      <section v-else-if="ui.drawerTab === 'events'" class="pane scroll">
        <EventsList :cluster="cluster" :kind="spec?.singular ?? ''" :namespace="ns" :name="name" />
      </section>

      <section v-else-if="ui.drawerTab === 'logs'" class="pane">
        <LogView :cluster="cluster" :namespace="ns" :pod="name" :containers="containers" />
      </section>

      <section v-else-if="ui.drawerTab === 'yaml'" class="pane">
        <YamlEditor :text="yamlText" :loading="yamlLoading" :error="yamlError" @save="doSave" @reload="loadYaml" />
      </section>

      <section v-else-if="ui.drawerTab === 'terminal'" class="pane">
        <TerminalView :key="sel.key" :cluster="cluster" :namespace="ns" :pod="name" :containers="containers" />
      </section>
    </aside>
  </Transition>
</template>

<style scoped>
.drawer { display: flex; flex-direction: column; width: 100%; height: 100%; min-height: 0; background: var(--panel); border-left: 1px solid var(--line); box-shadow: -18px 0 40px rgba(0, 0, 0, 0.38); }
:root[data-theme="light"] .drawer { box-shadow: -18px 0 40px rgba(15, 20, 32, 0.12); }
header { display: flex; align-items: center; gap: 10px; padding: 0 10px 0 16px; height: 46px; border-bottom: 1px solid var(--line); background: var(--panel); }
.ns { font-size: 12.5px; white-space: nowrap; }
.name { font-size: 14px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.close { margin-left: auto; }
.tabs { display: flex; gap: 2px; padding: 0 10px; border-bottom: 1px solid var(--line); background: var(--panel); }
.tab { height: 38px; padding: 0 12px; background: none; border: 0; border-bottom: 2px solid transparent; color: var(--text-2); font-weight: 500; font-size: 13px; margin-bottom: -1px; transition: color 100ms; }
.tab:hover { color: var(--text-0); }
.tab.on { color: var(--text-0); font-weight: 600; border-bottom-color: var(--accent); }
.pane { flex: 1; min-height: 0; padding: 14px 16px; display: flex; flex-direction: column; }
.scroll { overflow: auto; display: block; }
.charts { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; padding: 12px 14px; margin-bottom: 16px; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); }
.nometrics { margin: 0 0 16px; padding: 12px 14px; color: var(--text-2); font-size: 13px; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); }
.facts { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 20px; margin: 0; font-size: 13px; align-content: start; }
dt { color: var(--text-3); font-size: 12px; }
dd { margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-1); font-family: var(--mono); font-size: 12.5px; }
.data { margin-top: 20px; }
.consumers { margin-top: 20px; }
.consumers .none { margin: 0; font-size: 13px; max-width: 46ch; }
.consumer { display: flex; align-items: baseline; gap: 10px; width: 100%; padding: 7px 8px; background: none; border: 0;
  border-radius: var(--r-sm); text-align: left; color: var(--text-0); font-size: 13px; }
.consumer:hover { background: var(--hover); }
.consumer .who { font-weight: 550; white-space: nowrap; }
.consumer .how { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.kv { display: grid; grid-template-columns: max-content 1fr auto; gap: 10px; align-items: start; padding: 8px 0; border-bottom: 1px solid var(--line); }
.k { color: var(--text-1); font-weight: 600; }
.v { margin: 0; white-space: pre-wrap; word-break: break-all; color: var(--text-1); font-size: 12px; max-height: 160px; overflow: auto; }
.actions { margin-top: 22px; display: flex; flex-direction: column; gap: 9px; }
h3 { margin: 0 0 8px; font-size: 12px; font-weight: 600; color: var(--text-2); }
.act-row { display: flex; gap: 8px; align-items: center; }
.btn.ghost.danger { color: var(--bad); }
.btn.ghost.danger:hover { background: var(--bad-soft); border-color: transparent; }
.num { width: 96px; }
.hint { margin: 6px 0 0; font-size: 12.5px; }
.drawer-enter-active { transition: transform 180ms var(--ease), opacity 180ms; }
.drawer-leave-active { transition: transform 120ms var(--ease), opacity 120ms; }
.drawer-enter-from, .drawer-leave-to { transform: translateX(24px); opacity: 0; }
</style>
