<script setup lang="ts">
// What OpenLens shows when you open a pod: each container with its image,
// state, environment (including where each value comes from), mounts, ports,
// resources and probes. Secret-backed values stay hidden until asked for.
import { computed, ref } from 'vue'
import { toast } from '../composables/useToasts'
import { readSecretKey, readConfigKey } from '../composables/useObject'
import AppIcon from './AppIcon.vue'


const props = defineProps<{ obj: any; cluster: string; namespace: string }>()

interface EnvEntry { name: string; value?: string; from?: string; source?: { kind: 'secret' | 'configmap'; name: string; key: string }; hidden?: boolean }
interface Mount { path: string; volume: string; backing: string; readOnly: boolean }
interface Box {
  name: string; image: string; kind: 'init' | 'container'
  state: string; ready?: boolean; restarts?: number
  ports: string[]; env: EnvEntry[]; envFrom: string[]; mounts: Mount[]
  requests: string; limits: string; probes: string[]; command?: string
}

// A pod carries its containers directly; a workload carries a pod template.
const spec = computed(() => props.obj?.spec?.template?.spec ?? props.obj?.spec ?? {})
const statuses = computed<Record<string, any>>(() => {
  const out: Record<string, any> = {}
  for (const s of [...(props.obj?.status?.containerStatuses ?? []), ...(props.obj?.status?.initContainerStatuses ?? [])]) out[s.name] = s
  return out
})
const volumes = computed<Record<string, string>>(() => {
  const out: Record<string, string> = {}
  for (const v of spec.value?.volumes ?? []) {
    if (v.secret) out[v.name] = `secret ${v.secret.secretName}`
    else if (v.configMap) out[v.name] = `configmap ${v.configMap.name}`
    else if (v.persistentVolumeClaim) out[v.name] = `claim ${v.persistentVolumeClaim.claimName}`
    else if (v.emptyDir) out[v.name] = 'emptyDir'
    else if (v.hostPath) out[v.name] = `hostPath ${v.hostPath.path}`
    else if (v.projected) out[v.name] = 'projected'
    else out[v.name] = Object.keys(v).filter((k) => k !== 'name')[0] ?? 'volume'
  }
  return out
})

const stateOf = (s: any): string => {
  if (!s) return 'not started'
  if (s.state?.running) return 'Running'
  if (s.state?.waiting) return s.state.waiting.reason ?? 'Waiting'
  if (s.state?.terminated) return `${s.state.terminated.reason ?? 'Terminated'} (exit ${s.state.terminated.exitCode})`
  return 'Unknown'
}
const res = (r: any) => {
  if (!r) return ''
  const parts: string[] = []
  if (r.cpu) parts.push(`cpu ${r.cpu}`)
  if (r.memory) parts.push(`memory ${r.memory}`)
  return parts.join(', ')
}
const probe = (p: any, label: string): string | null => {
  if (!p) return null
  const what = p.httpGet ? `GET ${p.httpGet.path ?? '/'}:${p.httpGet.port}` : p.exec ? `exec ${(p.exec.command ?? []).join(' ')}` : p.tcpSocket ? `tcp ${p.tcpSocket.port}` : 'probe'
  return `${label}: ${what} every ${p.periodSeconds ?? 10}s`
}

const build = (c: any, kind: 'init' | 'container'): Box => {
  const st = statuses.value[c.name]
  const env: EnvEntry[] = []
  for (const e of c.env ?? []) {
    if (e.value !== undefined) env.push({ name: e.name, value: String(e.value) })
    else if (e.valueFrom?.secretKeyRef) env.push({ name: e.name, from: `secret ${e.valueFrom.secretKeyRef.name}.${e.valueFrom.secretKeyRef.key}`, source: { kind: 'secret', name: e.valueFrom.secretKeyRef.name, key: e.valueFrom.secretKeyRef.key }, hidden: true })
    else if (e.valueFrom?.configMapKeyRef) env.push({ name: e.name, from: `configmap ${e.valueFrom.configMapKeyRef.name}.${e.valueFrom.configMapKeyRef.key}`, source: { kind: 'configmap', name: e.valueFrom.configMapKeyRef.name, key: e.valueFrom.configMapKeyRef.key } })
    else if (e.valueFrom?.fieldRef) env.push({ name: e.name, from: `field ${e.valueFrom.fieldRef.fieldPath}` })
    else if (e.valueFrom?.resourceFieldRef) env.push({ name: e.name, from: `resource ${e.valueFrom.resourceFieldRef.resource}` })
    else env.push({ name: e.name, from: 'unresolved' })
  }
  return {
    name: c.name, image: c.image, kind,
    state: stateOf(st), ready: st?.ready, restarts: st?.restartCount,
    ports: (c.ports ?? []).map((p: any) => `${p.containerPort}/${p.protocol ?? 'TCP'}${p.name ? ` ${p.name}` : ''}`),
    env,
    envFrom: (c.envFrom ?? []).map((f: any) => (f.secretRef ? `all of secret ${f.secretRef.name}` : f.configMapRef ? `all of configmap ${f.configMapRef.name}` : 'envFrom')),
    mounts: (c.volumeMounts ?? []).map((m: any) => ({ path: m.mountPath, volume: m.name, backing: volumes.value[m.name] ?? 'volume', readOnly: !!m.readOnly })),
    requests: res(c.resources?.requests), limits: res(c.resources?.limits),
    probes: [probe(c.livenessProbe, 'liveness'), probe(c.readinessProbe, 'readiness'), probe(c.startupProbe, 'startup')].filter(Boolean) as string[],
    command: [...(c.command ?? []), ...(c.args ?? [])].join(' ') || undefined,
  }
}

const boxes = computed<Box[]>(() => [
  ...(spec.value?.initContainers ?? []).map((c: any) => build(c, 'init')),
  ...(spec.value?.containers ?? []).map((c: any) => build(c, 'container')),
])

const openBox = ref<string | null>(null)
const isOpen = (n: string) => openBox.value === n || boxes.value.length === 1
const revealed = ref<Record<string, string>>({})
const reveal = async (box: string, e: EnvEntry) => {
  if (!e.source) return
  const id = `${box}/${e.name}`
  if (revealed.value[id] !== undefined) { const { [id]: _, ...rest } = revealed.value; revealed.value = rest; return }
  try {
    const v = e.source.kind === 'secret'
      ? await readSecretKey(props.cluster, props.namespace, e.source.name, e.source.key)
      : await readConfigKey(props.cluster, props.namespace, e.source.name, e.source.key)
    revealed.value = { ...revealed.value, [id]: v }
  } catch (err: any) { toast(err.message, 'bad') }
}
const shown = (box: string, e: EnvEntry) => revealed.value[`${box}/${e.name}`]
const tone = (b: Box) => (b.state === 'Running' || b.state.startsWith('Completed') ? 'ok' : /Waiting|Pending|not started/.test(b.state) ? 'warn' : 'bad')
</script>

<template>
  <div v-if="boxes.length" class="containers">
    <h3>{{ boxes.length }} {{ boxes.length === 1 ? 'container' : 'containers' }}</h3>
    <div v-for="b in boxes" :key="b.name" class="box" :class="{ open: isOpen(b.name) }">
      <button class="head" @click="openBox = openBox === b.name ? null : b.name">
        <AppIcon name="chevronRight" :size="12" class="chev" />
        <span class="mono cname">{{ b.name }}</span>
        <span class="pill" :class="tone(b)">{{ b.state }}</span>
        <span v-if="b.kind === 'init'" class="pill">init</span>
        <span class="mono image">{{ b.image }}</span>
        <span v-if="b.restarts" class="restarts">{{ b.restarts }} restarts</span>
      </button>

      <div v-if="isOpen(b.name)" class="body">
        <div v-if="b.command" class="line"><span class="k">Command</span><span class="mono v">{{ b.command }}</span></div>
        <div v-if="b.ports.length" class="line"><span class="k">Ports</span><span class="mono v">{{ b.ports.join(', ') }}</span></div>
        <div v-if="b.requests || b.limits" class="line"><span class="k">Resources</span><span class="mono v">{{ [b.requests && `requests ${b.requests}`, b.limits && `limits ${b.limits}`].filter(Boolean).join(' · ') }}</span></div>
        <div v-for="p in b.probes" :key="p" class="line"><span class="k"></span><span class="mono v">{{ p }}</span></div>

        <template v-if="b.env.length || b.envFrom.length">
          <div class="sub">Environment</div>
          <div v-for="e in b.env" :key="e.name" class="env">
            <span class="mono ename">{{ e.name }}</span>
            <span v-if="e.value !== undefined" class="mono evalue">{{ e.value }}</span>
            <template v-else>
              <span class="mono evalue" :class="{ hidden: e.hidden && shown(b.name, e) === undefined }">{{ shown(b.name, e) ?? (e.hidden ? '••••••••' : e.from) }}</span>
              <button v-if="e.source" class="btn sm ghost" @click="reveal(b.name, e)"><AppIcon name="search" :size="12" />{{ shown(b.name, e) === undefined ? 'Resolve' : 'Hide' }}</button>
            </template>
            <span v-if="e.from && shown(b.name, e) !== undefined" class="dim src">{{ e.from }}</span>
          </div>
          <div v-for="f in b.envFrom" :key="f" class="env"><span class="dim" style="grid-column: 1 / -1">{{ f }}</span></div>
        </template>

        <template v-if="b.mounts.length">
          <div class="sub">Mounts</div>
          <div v-for="m in b.mounts" :key="m.path" class="env">
            <span class="mono ename">{{ m.path }}</span>
            <span class="mono evalue">{{ m.backing }}<span v-if="m.readOnly" class="dim"> (read only)</span></span>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.containers { margin-top: 20px; }
h3 { margin: 0 0 9px; font-size: 12px; font-weight: 600; color: var(--text-2); }
.box { border: 1px solid var(--line); border-radius: var(--r); margin-bottom: 8px; overflow: hidden; background: var(--panel-2); }
.box.open { border-color: var(--line-2); }
.head { display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 12px; background: none; border: 0; text-align: left; color: var(--text-0); }
.head:hover { background: var(--hover); }
.chev { color: var(--text-3); transition: transform 120ms var(--ease); }
.box.open .chev { transform: rotate(90deg); }
.cname { font-weight: 600; font-size: 13px; }
.image { color: var(--text-2); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-left: auto; }
.restarts { color: var(--warn); font-size: 12.5px; white-space: nowrap; }
.body { padding: 4px 12px 12px; border-top: 1px solid var(--line); }
.line { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 12px; padding: 5px 0; font-size: 13px; }
.k { color: var(--text-2); }
.v { color: var(--text-0); word-break: break-word; }
.sub { margin: 12px 0 4px; font-size: 12px; font-weight: 600; color: var(--text-2); border-top: 1px solid var(--line); padding-top: 10px; }
.env { display: grid; grid-template-columns: minmax(90px, max-content) minmax(0, 1fr) max-content; gap: 12px; align-items: center; padding: 4px 0; font-size: 12.5px; }
.ename { color: var(--text-2); overflow: hidden; text-overflow: ellipsis; }
.evalue { color: var(--text-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.evalue.hidden { color: var(--text-3); letter-spacing: 1px; }
.src { grid-column: 2 / -1; font-size: 12px; }
</style>
