<script setup lang="ts">
// Ask a model what is wrong with this object. The model is given the object,
// its events and recent logs, and returns prose. It has no tools and cannot
// reach the cluster, so nothing it says can change anything by itself.
import { ref, watch } from 'vue'
import { engine } from '../api/socket'
import AppIcon from './AppIcon.vue'

type Evidence = { object: string; events: string[]; logs: string[] }

const props = defineProps<{ cluster: string; kind: string; namespace?: string; name: string }>()
const answer = ref<string | null>(null)
// evidence is what would be sent. It is gathered locally and shown first;
// nothing leaves this machine until the person says so.
const evidence = ref<Evidence | null>(null)
const sent = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)
const hasKey = ref<boolean | null>(null)
const keyFromEnv = ref(false)
const keyInput = ref('')
const showEvidence = ref(false)

const checkKey = async () => {
  try {
    const m = await engine.request({ type: 'explain.status' })
    hasKey.value = !!m.hasKey
    keyFromEnv.value = !!m.keyFromEnv
  } catch { hasKey.value = false }
}
const saveKey = async () => {
  try {
    const m = await engine.request({ type: 'explain.key', key: keyInput.value.trim() })
    hasKey.value = !!m.hasKey
    keyFromEnv.value = !!m.keyFromEnv
    keyInput.value = ''
  } catch (e: any) { error.value = e.message }
}
const reset = () => { answer.value = null; evidence.value = null; sent.value = false; error.value = null; showEvidence.value = false }
const preview = async () => {
  busy.value = true; error.value = null; answer.value = null; sent.value = false
  try {
    const m = await engine.request({ type: 'explain.preview', cluster: props.cluster, kind: props.kind, namespace: props.namespace, name: props.name })
    evidence.value = m.evidence ?? null
    showEvidence.value = true
  } catch (e: any) { error.value = e.message } finally { busy.value = false }
}
const send = async () => {
  if (!evidence.value) return
  busy.value = true; error.value = null
  try {
    const m = await engine.request({
      type: 'explain', cluster: props.cluster, kind: props.kind, namespace: props.namespace, name: props.name,
      evidence: evidence.value,
    })
    answer.value = m.answer ?? ''
    sent.value = true
  } catch (e: any) { error.value = e.message } finally { busy.value = false }
}
watch(() => [props.cluster, props.kind, props.name], () => { reset(); checkKey() }, { immediate: true })
</script>

<template>
  <div class="explain">
    <div v-if="hasKey === false" class="setup card">
      <p><strong>Add a model key to use this.</strong> The key is held by the engine for this session only. It is never written to disk and never appears in the audit log.</p>
      <div class="row">
        <input v-model="keyInput" class="input" type="password" placeholder="Anthropic API key" spellcheck="false" @keyup.enter="saveKey" />
        <button class="btn" :disabled="!keyInput.trim()" @click="saveKey">Use this key</button>
      </div>
      <p class="dim">You can also start the engine with ANTHROPIC_API_KEY set.</p>
    </div>

    <template v-else>
      <p class="intro">A quiet second opinion. The model reads this object, its events and recent logs, then explains what it sees.</p>
      <p v-if="keyFromEnv" class="note dim">Using <span class="mono">ANTHROPIC_API_KEY</span> from the environment the engine was started in.</p>
      <div class="actions">
        <button class="btn" :disabled="busy" @click="preview"><AppIcon name="info" :size="14" />{{ evidence ? 'Start again' : 'Prepare a question' }}</button>
      </div>
      <p v-if="busy && !evidence" class="loading">Collecting the object, its events and recent logs…</p>
      <div v-if="error" class="err mono">{{ error }}</div>

      <template v-if="evidence && !sent">
        <div class="review card">
          <p><strong>This is what would be sent to Anthropic.</strong> Secret values are already replaced. Nothing has left this machine yet.</p>
          <button class="btn ghost sm" @click="showEvidence = !showEvidence">{{ showEvidence ? 'Hide it' : 'Show it' }}</button>
          <pre v-if="showEvidence" class="evidence mono">{{ evidence.events.length ? 'events:\n' + evidence.events.join('\n') + '\n\n' : '' }}{{ evidence.logs.length ? 'logs:\n' + evidence.logs.slice(-40).join('\n') + '\n\n' : '' }}{{ evidence.object }}</pre>
          <div class="row">
            <button class="btn primary" :disabled="busy" @click="send">{{ busy ? 'Sending…' : 'Send it' }}</button>
            <button class="btn ghost" :disabled="busy" @click="reset">Cancel</button>
          </div>
        </div>
      </template>

      <template v-if="answer">
        <p class="answer">{{ answer }}</p>
        <p class="foot dim">Read-only analysis. The model has no tools and cannot change anything in the cluster.</p>
        <button class="btn ghost sm" @click="showEvidence = !showEvidence">{{ showEvidence ? 'Hide' : 'Show' }} what was sent</button>
        <pre v-if="showEvidence && evidence" class="evidence mono">{{ evidence.events.length ? 'events:\n' + evidence.events.join('\n') + '\n\n' : '' }}{{ evidence.logs.length ? 'logs:\n' + evidence.logs.slice(-40).join('\n') + '\n\n' : '' }}{{ evidence.object }}</pre>
      </template>
    </template>
  </div>
</template>

<style scoped>
.explain { display: flex; flex-direction: column; gap: 14px; }
.setup { padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; }
.setup p { margin: 0; }
.row { display: flex; gap: 9px; }
.row .input { flex: 1; }
.intro { margin: 0; font-size: 13px; color: var(--text-2); max-width: 68ch; }
.actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.loading { margin: 0; font-size: 13px; color: var(--text-2); animation: breathe 1.7s ease-in-out infinite; }
@keyframes breathe { 0%, 100% { opacity: 1; } 50% { opacity: 0.42; } }
.answer { margin: 0; font-size: 14px; line-height: 1.6; color: var(--text-1); white-space: pre-wrap; max-width: 68ch; }
.foot { margin: 0; font-size: 12.5px; }
.evidence { margin: 0; padding: 12px 14px; max-height: 320px; overflow: auto; background: var(--canvas); border: 1px solid var(--line); border-radius: var(--r-sm); font-size: 12px; color: var(--text-2); white-space: pre-wrap; }
.err { color: var(--bad); white-space: pre-wrap; }
.note { margin: 0; font-size: 12.5px; }
.review { padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; align-items: flex-start; }
.review p { margin: 0; font-size: 13px; }
.review .evidence { align-self: stretch; }
</style>
