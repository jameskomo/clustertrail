<script setup lang="ts">
// The resource tree: every kind visible, with live counts so the list carries
// information instead of just links.
import { ui } from '../state'
import { KINDS, GROUPS } from '../kinds'
import { forwards, stopForward } from '../composables/useForwards'
import { counts } from '../composables/useCounts'
import AppIcon from './AppIcon.vue'

const go = (screen: string) => { ui.screen = screen; ui.selection = null }
const bad = (key: string) => counts.value[key]?.bad ?? 0
const total = (key: string) => counts.value[key]?.total
</script>

<template>
  <aside class="ctx">
    <nav class="tree">
      <template v-for="g in GROUPS" :key="g">
        <div class="group">{{ g }}</div>
        <button v-for="k in KINDS.filter((k) => k.group === g)" :key="k.key" class="item" :class="{ on: ui.screen === k.key }" @click="go(k.key)">
          <span v-if="bad(k.key)" class="sdot"></span>
          <span class="label">{{ k.label }}</span>
          <span v-if="bad(k.key)" class="tally bad">{{ bad(k.key) }}</span>
          <span v-else-if="total(k.key) !== undefined" class="tally dim">{{ total(k.key) }}</span>
        </button>
      </template>
    </nav>

    <div v-if="forwards.length" class="fwds">
      <div class="group">Port forwards</div>
      <div v-for="f in forwards" :key="f.id" class="fwd">
        <AppIcon name="forwards" :size="14" class="glyph" />
        <a class="mono" :href="`http://127.0.0.1:${f.localPort}`" target="_blank" rel="noreferrer">{{ f.localPort }}:{{ f.port }}</a>
        <span class="dim name">{{ f.pod }}</span>
        <button class="icon-btn rm" title="Stop forward" @click="stopForward(f.id)"><AppIcon name="close" :size="14" /></button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.ctx { display: flex; flex-direction: column; min-height: 0; background: var(--panel); border-right: 1px solid var(--line); }
.tree { flex: 1; overflow: auto; padding: 10px 8px 8px; }
.group { padding: 13px 9px 5px; font-size: 10.5px; font-weight: 600; color: var(--text-3); letter-spacing: 0.06em; text-transform: uppercase; }
.group:first-child { padding-top: 2px; }
.item { display: flex; align-items: center; gap: 8px; width: 100%; height: 28px; padding: 2px 9px 0; background: none; border: 1px solid transparent; border-radius: var(--r-sm);
  text-align: left; color: var(--text-2); font-size: 13px; font-weight: 450; line-height: 1; transition: background 100ms, color 100ms, border-color 100ms; }
.item:hover { background: var(--hover); color: var(--text-0); }
.item.on { background: var(--accent-soft); border-color: var(--accent-line); color: var(--text-0); font-weight: 550; box-shadow: var(--edge); }
.label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sdot { width: 5px; height: 5px; border-radius: 50%; background: var(--bad); flex: none; animation: pulse 2s infinite; }
.tally { font-family: var(--mono); margin-left: auto; font-size: 11.5px; }
.tally.bad { color: var(--bad); font-weight: 550; }
.fwds { border-top: 1px solid var(--line); padding: 0 8px 8px; }
.fwd { display: flex; align-items: center; gap: 7px; height: 28px; padding: 0 4px 0 9px; font-size: 12px; }
.fwd .glyph { color: var(--text-3); }
.fwd a { color: var(--accent-2); text-decoration: none; }
:root[data-theme="light"] .fwd a { color: var(--accent); }
.fwd a:hover { text-decoration: underline; }
.fwd .name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fwd .rm { width: 22px; height: 22px; }
</style>
