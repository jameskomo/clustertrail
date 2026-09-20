<script setup lang="ts">
import { computed, watch } from 'vue'
import { ui, select } from '../state'
import { kindByKey, customSpec } from '../kinds'
import { useResource } from '../composables/useResource'
import ResourceTable from '../components/ResourceTable.vue'
import CreateDialog from '../components/CreateDialog.vue'
import AppIcon from '../components/AppIcon.vue'
import { report } from '../composables/useCounts'
import { ref } from 'vue'
const creating = ref(false)

const props = defineProps<{ kind: string }>()
const emit = defineEmits<{ rows: [rows: Map<string, import('../api/types').Row>] }>()
const cluster = computed(() => ui.cluster)
const nsForSub = computed(() => (props.kind.startsWith('crd:') ? (ui.custom?.namespaced ? ui.namespace : '') : kindByKey(props.kind)?.namespaced ? ui.namespace : ''))
const { rows, synced, error, metrics, cached, cachedAt } = useResource(computed(() => props.kind), cluster, nsForSub)
const customColumns = computed(() => {
  // The engine projects a custom resource with its CRD's printer columns, so
  // the column set is whatever the rows carry, in first-seen order.
  const seen: string[] = []
  for (const r of rows.value.values()) for (const k of Object.keys(r.cells)) if (!seen.includes(k)) seen.push(k)
  return seen
})
const spec = computed(() => (props.kind.startsWith('crd:') && ui.custom ? customSpec(ui.custom.crd, ui.custom.kind, ui.custom.namespaced, customColumns.value) : kindByKey(props.kind)!))

// A CRD row is a doorway: clicking it browses that custom resource.
const onSelect = (r: import('../api/types').Row) => {
  if (props.kind === 'customresourcedefinitions') {
    ui.custom = { crd: r.name, kind: r.cells.kind, namespaced: r.cells.scope === 'Namespaced', columns: [] }
    ui.screen = `crd:${r.name}`
    ui.selection = null
    return
  }
  select(props.kind, r.key, r.namespace, r.name)
}
watch(rows, (r) => { emit('rows', r); report(props.kind, r) }, { immediate: true })

const counts = computed(() => {
  let ok = 0, warn = 0, bad = 0
  for (const r of rows.value.values()) { if (r.health === 'ok') ok++; else if (r.health === 'warn') warn++; else if (r.health === 'bad') bad++ }
  return { ok, warn, bad }
})
</script>

<template>
  <div class="screen">
    <Teleport defer to="#screen-actions">
      <span v-if="spec.namespaced" class="screen-desc">{{ rows.size }}&nbsp;in&nbsp;<span class="mono">{{ ui.namespace || 'all namespaces' }}</span></span>
      <span v-else class="screen-desc">{{ rows.size }} in this cluster</span>
      <span v-if="counts.bad" class="pill bad">{{ counts.bad }} failing</span>
      <span v-if="counts.warn" class="pill warn">{{ counts.warn }} pending</span>
      <span v-if="kind === 'pods' && synced && !metrics && !cached" class="pill" title="metrics-server is not installed in this cluster">no metrics</span>
      <span v-if="cached" class="pill warn" :title="cachedAt ? `Last seen ${new Date(cachedAt).toLocaleString()}` : ''">as of last session, syncing…</span>
      <span class="search">
        <AppIcon name="search" :size="14" />
        <input v-model="ui.filter" class="input" placeholder="Filter" spellcheck="false" />
      </span>
      <button v-if="!kind.startsWith('crd:') && kind !== 'events' && kind !== 'nodes'" class="btn" @click="creating = true"><AppIcon name="plus" :size="14" />New {{ spec.singular.toLowerCase() }}</button>
    </Teleport>
    <CreateDialog :open="creating" :kind="kind" @close="creating = false" />
    <div v-if="error" class="err mono">{{ error }}</div>
    <div v-else-if="!synced" class="empty">Reading {{ spec.label.toLowerCase() }}…</div>
    <ResourceTable v-else :rows="rows" :kind="spec" :filter="ui.filter" :show-namespace="spec.namespaced && !ui.namespace" :selected-key="ui.selection?.kind === kind ? ui.selection.key : null" @select="onSelect" />
  </div>
</template>

<style scoped>
.search { position: relative; display: inline-flex; align-items: center; }
.search .app-icon { position: absolute; left: 9px; color: var(--text-3); pointer-events: none; }
.search .input { padding-left: 28px; width: 240px; }
.err { padding: 24px; color: var(--bad); white-space: pre-wrap; }
</style>
