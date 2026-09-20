<script setup lang="ts">
import { toasts, type Toast } from '../composables/useToasts'
import type { IconName } from '../icons'
import AppIcon from './AppIcon.vue'

const ICON_BY_TONE: Record<Toast['tone'], IconName> = { ok: 'check', bad: 'warning', muted: 'info' }
</script>

<template>
  <div class="toasts">
    <TransitionGroup name="t">
      <div v-for="t in toasts" :key="t.id" class="toast" :class="t.tone">
        <AppIcon :name="ICON_BY_TONE[t.tone]" :size="14" class="ic" />
        <span class="msg">{{ t.text }}</span>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toasts { position: fixed; right: 16px; bottom: 16px; display: flex; flex-direction: column; gap: 8px; z-index: 50; pointer-events: none; }
.toast { position: relative; display: flex; align-items: center; gap: 9px; max-width: 420px; padding: 10px 14px 10px 15px; border-radius: var(--r); background: var(--raised); border: 1px solid var(--line-2); box-shadow: var(--shadow-2); color: var(--text-1); font-size: 13px; overflow: hidden; }
.toast::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 3px; background: var(--text-3); }
.toast.ok::before { background: var(--ok); }
.toast.bad::before { background: var(--bad); }
.ic { color: var(--text-2); }
.toast.ok .ic { color: var(--ok); }
.toast.bad .ic { color: var(--bad); }
.msg { min-width: 0; }
.t-enter-active, .t-leave-active { transition: opacity 200ms var(--ease), transform 200ms var(--ease); }
.t-enter-from, .t-leave-to { opacity: 0; transform: translateY(8px); }
.t-move { transition: transform 200ms var(--ease); }
.t-leave-active { position: absolute; }
</style>
