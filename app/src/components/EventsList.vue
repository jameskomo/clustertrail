<script setup lang="ts">
import { ref, watch } from 'vue'
import { engine } from '../api/socket'
import type { EventRow } from '../api/types'
import { now } from '../composables/useClock'
import { timeAgo } from '../composables/useFormat'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ cluster: string; kind: string; namespace?: string; name: string }>()
const events = ref<EventRow[] | null>(null)
const error = ref<string | null>(null)

const load = async () => {
  events.value = null; error.value = null
  try {
    const m = await engine.request({ type: 'events', cluster: props.cluster, kind: props.kind, namespace: props.namespace, name: props.name })
    events.value = m.events ?? []
  } catch (e: any) { error.value = e.message }
}
watch(() => [props.cluster, props.kind, props.namespace, props.name], load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <div class="events">
    <div v-if="error" class="empty bad">{{ error }}</div>
    <div v-else-if="events === null" class="empty">Loading events…</div>
    <div v-else-if="events.length === 0" class="empty"><AppIcon name="timeline" :size="18" /><strong>No events in the last hour.</strong><span>Kubernetes forgets events after an hour, so a quiet object is usually a healthy one.</span></div>
    <div v-for="(e, i) in events" :key="i" class="ev" :class="{ warn: e.type === 'Warning' }">
      <span class="dot" aria-hidden="true"></span>
      <div class="evbody">
        <div class="top">
          <span class="reason">{{ e.reason }}</span>
          <span v-if="e.count > 1" class="pill count">×{{ e.count }}</span>
          <span class="dim mono when">{{ timeAgo(e.lastSeen, now) }} ago</span>
        </div>
        <div class="msg">{{ e.message }}</div>
        <div v-if="e.source" class="dim src">{{ e.source }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.events { display: flex; flex-direction: column; }
.ev { display: flex; gap: 10px; padding: 10px 0; border-bottom: 1px solid var(--line); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--text-3); margin-top: 6px; flex-shrink: 0; }
.ev.warn .dot { background: var(--warn); }
.evbody { flex: 1; min-width: 0; }
.top { display: flex; align-items: center; gap: 8px; }
.reason { font-weight: 600; font-size: 12.5px; color: var(--text-0); }
.warn .reason { color: var(--warn); }
.count { height: 18px; font-size: 11px; padding: 0 6px; }
.when { margin-left: auto; font-size: 12px; }
.msg { margin-top: 3px; color: var(--text-1); font-size: 12.5px; white-space: pre-wrap; word-break: break-word; }
.src { margin-top: 3px; font-size: 11.5px; }
.bad { color: var(--bad); }
</style>
