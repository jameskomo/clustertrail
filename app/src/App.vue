<script setup lang="ts">
import { computed, onMounted, ref, shallowRef, watch } from 'vue'
import { ui, closeDrawer } from './state'
import { engine } from './api/socket'
import type { ClusterInfo, Row } from './api/types'
import { kindByKey } from './kinds'
import { useResource } from './composables/useResource'
import { refreshChanges } from './composables/useChanges'
import { refreshForwards } from './composables/useForwards'
import { clearCounts } from './composables/useCounts'
import IconRail from './components/IconRail.vue'
import ContextPanel from './components/ContextPanel.vue'
import TopBar from './components/TopBar.vue'
import Drawer from './components/Drawer.vue'
import ChangeDialog from './components/ChangeDialog.vue'
import CommandPalette from './components/CommandPalette.vue'
import Toasts from './components/Toasts.vue'
import DemoBanner from './components/DemoBanner.vue'
import OverviewScreen from './screens/OverviewScreen.vue'
import ResourceScreen from './screens/ResourceScreen.vue'
import ChangesScreen from './screens/ChangesScreen.vue'
import DriftScreen from './screens/DriftScreen.vue'
import TimelineScreen from './screens/TimelineScreen.vue'
import HelmScreen from './screens/HelmScreen.vue'

const clusters = ref<ClusterInfo[]>([])
engine.on((m) => {
  if (m.type === 'hello') { engine.send({ type: 'clusters' }); refreshChanges(); refreshForwards() }
  if (m.type === 'clusters') {
    clusters.value = m.clusters ?? []
    if (!ui.cluster && clusters.value.length) ui.cluster = (clusters.value.find((c) => c.current) ?? clusters.value[0]!).name
  }
})

const cluster = computed(() => ui.cluster)
const namespaces = useResource(computed(() => 'namespaces'), cluster, computed(() => ''))
watch(cluster, () => { ui.namespace = ''; closeDrawer(); clearCounts() })
watch(() => ui.screen, () => (ui.filter = ''))

const currentRows = shallowRef(new Map<string, Row>())
const selectedRow = computed(() => (ui.selection ? currentRows.value.get(ui.selection.key) ?? null : null))
const kindScreen = computed(() => kindByKey(ui.screen) ?? (ui.screen.startsWith('crd:') ? ui.custom : null))

onMounted(() => {
  window.addEventListener('keydown', (e) => {
    const inField = (e.target as HTMLElement)?.closest('input, textarea, select, .cm-editor, .xterm')
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') { e.preventDefault(); ui.paletteOpen = !ui.paletteOpen; return }
    if (e.key === 'Escape' && !ui.paletteOpen) {
      if (inField) (e.target as HTMLElement).blur()
      else if (ui.selection) closeDrawer()
      else ui.filter = ''
    }
    if (inField || ui.paletteOpen) return
    if (e.key === '/') { e.preventDefault(); (document.querySelector('.search') as HTMLInputElement | null)?.focus() }
  })
})
</script>

<template>
  <DemoBanner />
  <div class="app" :class="{ open: ui.selection }">
    <IconRail />
    <TopBar :clusters="clusters" :namespaces="namespaces.rows.value" />
    <ContextPanel />
    <main class="main">
      <OverviewScreen v-if="ui.screen === 'overview'" />
      <ChangesScreen v-else-if="ui.screen === 'changes'" />
      <TimelineScreen v-else-if="ui.screen === 'timeline'" />
      <DriftScreen v-else-if="ui.screen === 'drift'" />
      <HelmScreen v-else-if="ui.screen === 'helm'" />
      <ResourceScreen v-else-if="kindScreen" :key="ui.screen" :kind="ui.screen" @rows="currentRows = $event" />
    </main>
    <div class="side"><Drawer :row="selectedRow" /></div>
    <ChangeDialog />
    <CommandPalette :clusters="clusters" :namespaces="namespaces.rows.value" :rows="currentRows" />
    <Toasts />
  </div>
</template>

<style scoped>
.app {
  display: grid; height: 100%;
  grid-template-areas: "rail top top top" "rail ctx main side";
  grid-template-columns: 53px 208px minmax(0, 1fr) 0;
  grid-template-rows: 52px minmax(0, 1fr);
  transition: grid-template-columns 200ms var(--ease);
}
.app.open { grid-template-columns: 53px 208px minmax(0, 1fr) clamp(440px, 40vw, 720px); }
.app > .rail { grid-area: rail; }
.app > .topbar { grid-area: top; }
.app > .ctx { grid-area: ctx; }
.main { grid-area: main; min-width: 0; min-height: 0; }
.side { grid-area: side; min-width: 0; min-height: 0; overflow: hidden; }
.app.open .side { border-left: 1px solid var(--line); }
</style>
