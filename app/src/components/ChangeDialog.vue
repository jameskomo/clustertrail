<script setup lang="ts">
// The gate, as the person sees it. It speaks in a sentence, shows the exact
// diff, and asks for one decision. This is the one bold element in the
// product; everything else stays quiet so this moment stands out.
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { pendingChange, decide } from '../composables/useChanges'
import { kindByKey } from '../kinds'
import DiffView from './DiffView.vue'
import AppIcon from './AppIcon.vue'

const busy = ref(false)
const note = ref('')
const act = async (d: 'approve' | 'reject') => {
  if (!pendingChange.value || busy.value) return
  busy.value = true
  await decide(pendingChange.value.id, d, note.value)
  busy.value = false
  note.value = ''
}
const close = () => {
  if (!busy.value) pendingChange.value = null
}
const key = (e: KeyboardEvent) => {
  if (!pendingChange.value) return
  if (e.key === 'Escape') act('reject')
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') act('approve')
}
onMounted(() => window.addEventListener('keydown', key))
onUnmounted(() => window.removeEventListener('keydown', key))

// "You are about to scale the deployment checkout in shop from 4 to 5 replicas."
const sentence = computed(() => {
  const c = pendingChange.value
  if (!c) return ''
  const op = c.plan[0]!
  const kind = kindByKey(op.kind)?.singular.toLowerCase() ?? op.kind
  const where = op.namespace ? ` in ${op.namespace}` : ''
  const from = c.diff.match(/^-\s+replicas: (\d+)/m)?.[1]
  const rest = c.plan.length > 1 ? `, and ${c.plan.length - 1} more` : ''
  switch (op.op) {
    case 'scale': return `scale the ${kind} ${op.name}${where}${from ? ` from ${from}` : ''} to ${op.replicas} replicas${rest}`
    case 'restart': return `restart every pod of the ${kind} ${op.name}${where}${rest}`
    case 'delete': return `delete the ${kind} ${op.name}${where}${rest}`
    default: return `change the ${kind} ${op.name}${where}${rest}`
  }
})
const firstOp = computed(() => pendingChange.value?.plan[0])
const fromReplicas = computed(() => pendingChange.value?.diff.match(/^-\s+replicas: (\d+)/m)?.[1])
const target = computed(() => {
  const c = pendingChange.value
  const op = c?.plan[0]
  if (!c || !op) return ''
  const kind = kindByKey(op.kind)?.singular ?? op.kind
  const where = op.namespace ? `${op.namespace}/` : ''
  const rest = c.plan.length > 1 ? ` +${c.plan.length - 1} more` : ''
  return `${kind} ${where}${op.name}${rest}`
})
const isDangerous = computed(() => !!pendingChange.value && (pendingChange.value.blast.deletes > 0 || pendingChange.value.blast.hasSecrets))
// Every op has to be a no-op, not just one of them. The diff is the per-op
// diffs concatenated, each headed "# <op> <Kind>/<name>", so this counts the
// quiet ones against the total. Using includes() meant a change that created
// a Deployment and left a Service alone told the reviewer that applying it
// would change nothing.
const noChange = computed(() => {
  const diff = pendingChange.value?.diff
  if (!diff) return false
  const ops = (diff.match(/^# /gm) ?? []).length
  const quiet = (diff.match(/^\(no change\)$/gm) ?? []).length
  return ops > 0 && quiet === ops
})
</script>

<template>
  <Transition name="fade">
    <div v-if="pendingChange" class="scrim" @click.self="act('reject')">
      <div class="dialog" role="dialog" aria-modal="true">
        <header>
          <div class="eyebrow">
            <span class="kicker">{{ pendingChange.author.type === 'agent' ? 'Agent proposal' : 'Your change' }}</span>
            <span class="pill warn">pending</span>
          </div>
          <h2>{{ sentence }}<span v-if="pendingChange.cluster"> on <span class="mono">{{ pendingChange.cluster }}</span>.</span></h2>
          <p v-if="pendingChange.author.type === 'agent'" class="reason muted">“{{ pendingChange.intent }}”</p>
        </header>

        <dl class="facts">
          <div class="fact"><dt>Target</dt><dd class="mono">{{ target }}</dd></div>
          <div v-if="firstOp?.op === 'scale' && fromReplicas" class="fact"><dt>Replicas</dt><dd class="mono">{{ fromReplicas }} to {{ firstOp?.replicas }}</dd></div>
          <div class="fact"><dt>Objects</dt><dd class="mono">{{ pendingChange.blast.objects }}</dd></div>
          <div class="fact"><dt>Namespaces</dt><dd class="mono">{{ pendingChange.blast.namespaces.join(', ') || 'cluster-wide' }}</dd></div>
          <div class="fact"><dt>Deletes</dt><dd class="mono" :class="{ bad: pendingChange.blast.deletes > 0 }">{{ pendingChange.blast.deletes }}</dd></div>
          <div class="fact"><dt>Secrets</dt><dd :class="{ warn: pendingChange.blast.hasSecrets }">{{ pendingChange.blast.hasSecrets ? 'touched' : 'none' }}</dd></div>
        </dl>

        <p v-if="isDangerous" class="warnline"><AppIcon name="warning" :size="14" /> This change deletes objects or touches secrets.</p>

        <DiffView :diff="pendingChange.diff" class="diff" />
        <p v-if="noChange" class="muted small">The cluster already matches; applying would change nothing.</p>

        <footer>
          <button class="btn danger" :disabled="busy" @click="act('reject')">Reject <span class="kbd">esc</span></button>
          <input v-model="note" class="input note" placeholder="Note for the audit log, optional" />
          <button class="btn" :disabled="busy" @click="close">Cancel</button>
          <button class="btn primary" :disabled="busy" @click="act('approve')">{{ busy ? 'Applying…' : 'Approve and apply' }} <span class="kbd k">Ctrl ⏎</span></button>
        </footer>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; background: rgba(4, 6, 10, 0.6); backdrop-filter: blur(2px); display: flex; align-items: center; justify-content: center; z-index: 40; }
.dialog { width: min(640px, calc(100vw - 48px)); max-height: calc(100vh - 64px); display: flex; flex-direction: column; gap: 14px; padding: 20px 22px 16px; background: var(--panel); border: 1px solid var(--line-2); border-radius: var(--r-lg); box-shadow: var(--shadow-3); }
.eyebrow { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.kicker { font-size: 12px; font-weight: 600; color: var(--text-3); }
h2 { margin: 8px 0 0; font-size: 15px; font-weight: 600; line-height: 1.45; letter-spacing: -0.01em; max-width: 62ch; }
h2 .mono { font-size: 14px; }
.reason { margin: 6px 0 0; font-size: 13px; }
.facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 10px 16px; margin: 0; padding: 12px 14px; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); }
.fact dt { font-size: 11.5px; color: var(--text-3); margin-bottom: 2px; }
.fact dd { margin: 0; font-size: 12.5px; color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fact dd.bad { color: var(--bad); font-weight: 600; }
.fact dd.warn { color: var(--warn); }
.warnline { display: flex; align-items: center; gap: 7px; margin: 0; font-size: 12.5px; color: var(--bad); }
.diff { flex: 1; min-height: 120px; max-height: 44vh; }
.small { margin: 0; font-size: 12.5px; }
footer { display: flex; align-items: center; gap: 8px; }
.note { flex: 1; min-width: 0; }
.k { background: rgba(0, 0, 0, 0.22); border-color: transparent; color: var(--accent-ink); }
.fade-enter-active { transition: opacity 140ms; }
.fade-leave-active { transition: opacity 100ms; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
