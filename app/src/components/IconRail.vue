<script setup lang="ts">
// The activity rail: always-visible section navigation, VS Code style.
// Icons only. Labels live in tooltips and the topbar.
import { computed } from 'vue'
import { ui } from '../state'
import { kindByKey } from '../kinds'
import { changes } from '../composables/useChanges'
import { engineStatus } from '../api/socket'
import { pref, cycleTheme, type Pref } from '../composables/useTheme'
import AppIcon from './AppIcon.vue'
import type { IconName } from '../icons'

const SECTIONS: { key: string; icon: IconName; label: string }[] = [
  { key: 'overview', icon: 'overview', label: 'Overview' },
  { key: 'pods', icon: 'resources', label: 'Resources' },
  { key: 'timeline', icon: 'timeline', label: 'What changed' },
  { key: 'drift', icon: 'drift', label: 'Drift' },
  { key: 'changes', icon: 'changes', label: 'Changes' },
  { key: 'helm', icon: 'helm', label: 'Helm' },
]

const pending = computed(() => changes.value.filter((c) => c.status === 'pending').length)
const onResources = computed(() => Boolean(kindByKey(ui.screen)) || ui.screen.startsWith('crd:'))
const active = (key: string) => (key === 'pods' ? onResources.value : ui.screen === key)
const go = (key: string) => { ui.screen = key; ui.selection = null }

const THEME_ICONS: Record<Pref, IconName> = { system: 'monitor', light: 'sun', dark: 'moon' }
const nextMode = computed(() => (pref.value === 'system' ? 'light' : pref.value === 'light' ? 'dark' : 'system'))
</script>

<template>
  <nav class="rail">
    <div class="mark" title="ClusterTrail"><AppIcon name="helm" :size="17" /></div>

    <div class="sections">
      <button
        v-for="s in SECTIONS"
        :key="s.key"
        class="slot"
        :class="{ on: active(s.key) }"
        :title="s.label"
        @click="go(s.key)"
      >
        <AppIcon :name="s.icon" :size="17" />
        <span v-if="s.key === 'changes' && pending" class="badge">{{ pending }}</span>
      </button>
    </div>

    <div class="tail">
      <button class="slot" title="Search (⌘K)" @click="ui.paletteOpen = true"><AppIcon name="search" :size="17" /></button>
      <button class="slot" :title="`Switch to ${nextMode} theme`" @click="cycleTheme"><AppIcon :name="THEME_ICONS[pref]" :size="17" /></button>
      <span
        class="led"
        :class="engineStatus"
        :title="engineStatus === 'open' ? 'Engine connected' : 'Engine reconnecting'"
      ></span>
    </div>
  </nav>
</template>

<style scoped>
.rail {
  display: flex; flex-direction: column; align-items: center; gap: 2px;
  padding: 10px 0 12px; background: var(--panel-2); border-right: 1px solid var(--line);
}
.mark {
  display: flex; align-items: center; justify-content: center; width: 34px; height: 34px; margin-bottom: 10px;
  background: var(--accent-soft); border: 1px solid var(--accent-line); border-radius: 9px; color: var(--accent-2);
  box-shadow: var(--edge);
}
:root[data-theme="light"] .mark { color: var(--accent); }
.sections { display: flex; flex-direction: column; gap: 2px; }
.slot {
  position: relative; display: flex; align-items: center; justify-content: center;
  width: 34px; height: 34px; padding: 0; background: none; border: 1px solid transparent; border-radius: 8px;
  color: var(--text-2); transition: background 120ms var(--ease), color 120ms var(--ease);
}
.slot:hover { background: var(--raised); color: var(--text-0); }
.slot.on { background: var(--accent-soft); border-color: var(--accent-line); color: var(--accent-2); }
:root[data-theme="light"] .slot.on { color: var(--accent); }
.slot.on::before {
  content: ''; position: absolute; left: -11px; top: 8px; bottom: 8px; width: 2.5px;
  border-radius: 2px; background: var(--accent);
}
.badge {
  position: absolute; top: 2px; right: 1px; min-width: 14px; height: 14px; padding: 0 4px;
  border-radius: 7px; background: var(--warn); color: #0b0e13;
  font: 600 9px/14px var(--sans); text-align: center;
}
.tail { margin-top: auto; display: flex; flex-direction: column; align-items: center; gap: 2px; }
.led { width: 7px; height: 7px; margin-top: 8px; border-radius: 50%; background: var(--text-3); }
.led.open { background: var(--ok); box-shadow: 0 0 6px var(--ok); }
.led.closed { background: var(--bad); }
</style>
