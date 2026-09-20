<script setup lang="ts">
// The one bar that frames every screen: title and description on the left,
// the active screen's actions teleported into the middle, and global context
// (namespace, cluster, search) on the right.
import { computed } from 'vue'
import { ui } from '../state'
import { kindByKey, SCREEN_META } from '../kinds'
import type { ClusterInfo, Row } from '../api/types'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ clusters: ClusterInfo[]; namespaces: Map<string, Row> }>()
const nsList = computed(() => [...props.namespaces.values()].map((r) => r.name).sort())

const title = computed(() => {
  if (ui.screen.startsWith('crd:')) return ui.custom?.kind ?? 'Custom resource'
  const meta = SCREEN_META[ui.screen]
  if (meta) return meta.title
  return kindByKey(ui.screen)?.label ?? ui.screen
})
const desc = computed(() => (ui.screen.startsWith('crd:') ? ui.custom?.crd ?? '' : SCREEN_META[ui.screen]?.desc ?? ''))
</script>

<template>
  <header class="topbar">
    <div class="brand" title="ClusterTrail">
      <span class="word">Cluster<span class="pop">Trail</span></span>
    </div>
    <span class="rule"></span>
    <div class="id">
      <h1>{{ title }}</h1>
      <span v-if="desc" class="desc">{{ desc }}</span>
    </div>

    <div id="screen-actions" class="sactions"></div>

    <div class="global">
      <button class="search" @click="ui.paletteOpen = true">
        <AppIcon name="search" :size="14" />
        <span class="hint">Search or jump to…</span>
        <span class="kbd">⌘K</span>
      </button>
      <label class="sel">
        <select v-model="ui.namespace" class="input" title="Namespace">
          <option value="">All namespaces</option>
          <option v-for="n in nsList" :key="n" :value="n">{{ n }}</option>
        </select>
        <AppIcon name="chevronDown" :size="14" class="chev" />
      </label>
      <label class="sel">
        <select v-model="ui.cluster" class="input" title="Cluster">
          <option v-for="c in clusters" :key="c.name" :value="c.name">{{ c.name }}</option>
        </select>
        <AppIcon name="chevronDown" :size="14" class="chev" />
      </label>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  display: flex; align-items: center; gap: 16px; height: 52px; padding: 0 16px 0 20px;
  background: color-mix(in srgb, var(--panel) 82%, transparent);
  backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px);
  border-bottom: 1px solid var(--line); flex-shrink: 0; min-width: 0;
}
.id { display: flex; align-items: center; gap: 10px; min-width: 0; flex-shrink: 0; }
.brand { flex-shrink: 0; }
.word { font-size: 14.5px; font-weight: 600; letter-spacing: -0.01em; line-height: 1; color: var(--text-0); white-space: nowrap; }
.word .pop { color: var(--accent-2); }
:root[data-theme="light"] .word .pop { color: var(--accent); }
.rule { width: 1px; height: 18px; background: var(--line-2); flex-shrink: 0; }
h1 { margin: 0; font-size: 15.5px; font-weight: 600; letter-spacing: -0.015em; line-height: 1; color: var(--text-0); white-space: nowrap; }
.desc { font-size: 12.5px; line-height: 1; color: var(--text-3); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.sactions { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
.sactions:empty { display: none; }
.global { margin-left: auto; display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.search {
  display: flex; align-items: center; gap: 8px; height: 30px; width: 190px; padding: 0 8px 0 10px;
  background: var(--panel-2); border: 1px solid var(--line-2); border-radius: var(--r-sm);
  color: var(--text-3); font-size: 12.5px; transition: border-color 120ms, color 120ms;
}
.search:hover { border-color: var(--text-3); color: var(--text-2); }
.search .hint { flex: 1; text-align: left; white-space: nowrap; overflow: hidden; }
.sel { position: relative; display: block; }
.sel .input {
  width: 148px; font-family: var(--mono); font-size: 12px; appearance: none; padding-right: 26px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sel:first-of-type .input { width: 132px; }
.chev { position: absolute; right: 8px; top: 50%; transform: translateY(-50%); color: var(--text-3); pointer-events: none; }

/* Teleported screen actions share the topbar rhythm. */
:global(#screen-actions .screen-desc) { font-size: 13px; color: var(--text-2); display: flex; align-items: baseline; gap: 0; min-width: 0; }
:global(#screen-actions .mono) { font-size: 12.5px; }
:global(#screen-actions) :is(.btn, .icon-btn, .chip, .seg, .input) { flex-shrink: 0; }
</style>
