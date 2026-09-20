<script setup lang="ts">
// Helm releases read straight from what Helm stores in the cluster: no Helm
// binary, no repository access. Read-only for now; rollback is not wired yet.
import { computed, ref, watch } from 'vue'
import { ui } from '../state'
import { engine } from '../api/socket'
import { now } from '../composables/useClock'
import { timeAgo } from '../composables/useFormat'
import { propose } from '../composables/useChanges'
import { toast } from '../composables/useToasts'
import { KINDS } from '../kinds'
import AppIcon from '../components/AppIcon.vue'

interface Release { name: string; namespace: string; revision: number; status: string; chart: string; appVersion: string; updated: string; description?: string }
interface Detail extends Release { values: string; manifest: string; notes: string; history: Release[] }

const releases = ref<Release[] | null>(null)
const error = ref<string | null>(null)
const selected = ref<Detail | null>(null)
const tab = ref<'values' | 'manifest' | 'notes' | 'history'>('values')

const load = async () => {
  releases.value = null; error.value = null; selected.value = null
  if (!ui.cluster) return
  try {
    const m = await engine.request({ type: 'helm.list', cluster: ui.cluster, namespace: ui.namespace })
    releases.value = ((m as any).releases ?? []) as Release[]
  } catch (e: any) { error.value = e.message }
}
watch([() => ui.cluster, () => ui.namespace], load, { immediate: true })

const openRelease = async (r: Release) => {
  try {
    const m = await engine.request({ type: 'helm.get', cluster: ui.cluster!, namespace: r.namespace, name: r.name })
    selected.value = (m as any).release as Detail
    tab.value = 'values'
  } catch (e: any) { error.value = e.message }
}
const tone = (s: string) => (s === 'deployed' ? 'ok' : s === 'failed' ? 'bad' : 'warn')

// Rolling back applies the objects of an older revision through the same
// review gate as any other change. Helm's own history is not rewritten, and
// the dialog says so.
const rollingBack = ref<number | null>(null)
const rollback = async (revision: number) => {
  if (!selected.value || !ui.cluster) return
  rollingBack.value = revision
  try {
    const m = await engine.request({ type: 'helm.rollback.plan', cluster: ui.cluster, namespace: selected.value.namespace, name: selected.value.name, revision })
    const plan = (m as any).rollback
    const ops = plan.objects.map((o: any) => {
      const spec = KINDS.find((k) => k.singular === o.kind)
      return spec ? { op: 'apply' as const, kind: spec.key, namespace: o.namespace, name: o.name, yaml: o.yaml } : null
    }).filter(Boolean)
    const unknown = plan.objects.length - ops.length
    if (!ops.length) { toast('None of the objects in that revision are kinds ClusterTrail can apply yet.', 'bad'); return }
    if (unknown) toast(`${unknown} object${unknown === 1 ? '' : 's'} in that revision use kinds ClusterTrail cannot apply yet, and are left out.`, 'muted', 7000)
    toast(plan.warning, 'muted', 9000)
    await propose(ui.cluster, `Roll ${plan.name} back from revision ${plan.from} to ${plan.to}`, ops)
  } catch (e: any) { error.value = e.message } finally { rollingBack.value = null }
}
const body = computed(() => {
  const r = selected.value
  if (!r) return ''
  return tab.value === 'values' ? r.values : tab.value === 'manifest' ? r.manifest : r.notes || 'This chart ships no notes.'
})
</script>

<template>
  <div class="screen">
    <Teleport defer to="#screen-actions">
      <button class="btn" @click="load"><AppIcon name="refresh" :size="14" /> Refresh</button>
    </Teleport>

    <div v-if="error" class="err mono">{{ error }}</div>
    <div v-else-if="releases === null" class="empty">Reading releases…</div>
    <div v-else-if="!releases.length" class="empty">
      <AppIcon name="helm" :size="28" />
      <strong>No Helm releases here.</strong>
      <span>ClusterTrail reads the objects Helm stores in the cluster, so a release installed by any Helm version shows up without extra setup.</span>
    </div>

    <div v-else class="cols">
      <div class="list">
        <button v-for="r in releases" :key="r.namespace + r.name" class="rel card" :class="{ on: selected?.name === r.name && selected?.namespace === r.namespace }" @click="openRelease(r)">
          <div class="top">
            <span class="mono name">{{ r.name }}</span>
            <span class="pill" :class="tone(r.status)">{{ r.status }}</span>
          </div>
          <div class="meta dim mono">{{ r.chart }} · app {{ r.appVersion || 'n/a' }} · rev {{ r.revision }}</div>
          <div class="meta dim">{{ r.namespace }} · updated {{ timeAgo(r.updated, now) }} ago</div>
        </button>
      </div>

      <div v-if="selected" class="detail card">
        <div class="panel-head">
          <h2 class="mono">{{ selected.name }}</h2>
          <span class="muted">{{ selected.chart }}</span>
          <div class="actions">
            <span class="pill" :class="tone(selected.status)">{{ selected.status }}</span>
          </div>
        </div>
        <nav class="tabs">
          <button v-for="t in (['values','manifest','notes','history'] as const)" :key="t" class="tab" :class="{ on: tab === t }" @click="tab = t">{{ t }}</button>
        </nav>
        <div v-if="tab === 'history'" class="history">
          <div v-for="h in selected.history" :key="h.revision" class="hrow">
            <span class="mono rev">#{{ h.revision }}</span>
            <span class="pill" :class="tone(h.status)">{{ h.status }}</span>
            <span class="muted chart">{{ h.chart }}</span>
            <span class="muted">{{ h.description }}</span>
            <span class="dim when">{{ timeAgo(h.updated, now) }} ago</span>
            <button v-if="h.revision !== selected.revision" class="btn sm" :disabled="rollingBack !== null" @click="rollback(h.revision)">
              {{ rollingBack === h.revision ? 'Reading…' : 'Roll back to this…' }}
            </button>
            <span v-else class="dim">current</span>
          </div>
        </div>
        <pre v-else class="body mono">{{ body }}</pre>
      </div>
      <div v-else class="detail card empty-detail"><div class="empty"><AppIcon name="helm" :size="28" /><strong>Pick a release.</strong><span>You get the values it was installed with, the manifest it rendered, its notes and its revision history.</span></div></div>
    </div>
  </div>
</template>

<style scoped>
.cols { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(280px, 340px) minmax(0, 1fr); gap: 16px; padding: 16px 20px 20px; }
.list { overflow: auto; display: flex; flex-direction: column; gap: 8px; }
.rel { display: flex; flex-direction: column; gap: 3px; padding: 11px 14px; text-align: left; color: var(--text-0); }
.rel:hover { background: var(--hover); }
.rel.on { background: var(--accent-soft); border-color: var(--accent-line); box-shadow: var(--shadow-1), inset 2px 0 0 var(--accent); }
.rel.on:hover { background: var(--accent-soft); }
.top { display: flex; align-items: center; gap: 10px; }
.name { font-weight: 600; }
.top .pill { margin-left: auto; }
.meta { font-size: 12px; }
.detail { display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
.tabs { display: flex; gap: 2px; padding: 0 12px; border-bottom: 1px solid var(--line); flex-shrink: 0; }
.tab { height: 34px; padding: 0 10px; background: none; border: 0; color: var(--text-2); font-size: 13px; text-transform: capitalize; box-shadow: inset 0 -2px 0 transparent; transition: color 120ms var(--ease), box-shadow 120ms var(--ease); }
.tab:hover { color: var(--text-0); }
.tab.on { color: var(--text-0); box-shadow: inset 0 -2px 0 var(--accent); }
.body { flex: 1; margin: 14px 16px 16px; padding: 12px 14px; overflow: auto; font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; color: var(--text-1); background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r-sm); }
.history { flex: 1; overflow: auto; }
.hrow { display: flex; align-items: center; gap: 12px; padding: 10px 16px; border-bottom: 1px solid var(--line); font-size: 13px; }
.chart { font-family: var(--mono); font-size: 12.5px; }
.rev { color: var(--text-2); }
.when { margin-left: auto; font-size: 12.5px; }
.hrow .btn { flex: none; }
.empty-detail { align-items: center; justify-content: center; }
.err { color: var(--bad); padding: 20px; }
</style>
