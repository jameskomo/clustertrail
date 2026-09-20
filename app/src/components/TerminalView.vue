<script setup lang="ts">
// xterm bound to an exec session over the engine socket. Bytes are base64 in
// JSON frames; fine for a terminal, and it keeps the protocol to one socket.
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { engine, nextId } from '../api/socket'
import { isDark } from '../composables/useTheme'

const props = defineProps<{ cluster: string; namespace: string; pod: string; containers: string[] }>()
const container = ref(props.containers[0] ?? '')
const host = ref<HTMLElement | null>(null)
const status = ref<'connecting' | 'open' | 'closed'>('connecting')
const closedMsg = ref('')
let term: Terminal | null = null
let fit: FitAddon | null = null
let id = ''
let off: (() => void) | null = null
let ro: ResizeObserver | null = null

const termTheme = (d: boolean) => d
  ? { background: '#12161d', foreground: '#f2f5f9', cursor: '#8b5cf6', selectionBackground: 'rgba(139,92,246,0.3)', brightBlack: '#525c6e' }
  : { background: '#f5f7fb', foreground: '#0f1420', cursor: '#4f46e5', selectionBackground: 'rgba(79,70,229,0.2)', black: '#0f1420', brightBlack: '#97a1b1', white: '#eef1f6', brightWhite: '#0f1420' }
watch(isDark, (d) => { if (term) term.options.theme = termTheme(d) })
const enc = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)))
const dec = (b: string) => new TextDecoder().decode(Uint8Array.from(atob(b), (c) => c.charCodeAt(0)))

const open = () => {
  if (id) engine.send({ type: 'exec.stop', id })
  id = nextId('term')
  status.value = 'connecting'; closedMsg.value = ''
  term?.reset()
  engine.send({ type: 'exec.start', id, cluster: props.cluster, namespace: props.namespace, name: props.pod, container: container.value, cols: term?.cols, rows: term?.rows })
}

onMounted(() => {
  term = new Terminal({
    cursorBlink: true, fontSize: 13, fontFamily: "'JetBrains Mono Variable', ui-monospace, 'SF Mono', Menlo, monospace", lineHeight: 1.25,
    theme: termTheme(isDark.value),
    allowProposedApi: true,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host.value!)
  fit.fit()
  term.onData((d) => engine.send({ type: 'exec.input', id, data: enc(d) }))
  term.onResize(({ cols, rows }) => engine.send({ type: 'exec.resize', id, cols, rows }))
  ro = new ResizeObserver(() => fit?.fit())
  ro.observe(host.value!)
  off = engine.on((m) => {
    if (m.id !== id) return
    if (m.type === 'term') {
      if (m.eof) { status.value = 'closed'; closedMsg.value = m.message ?? ''; term?.write('\r\n\x1b[90m[session closed]\x1b[0m\r\n') }
      else { status.value = 'open'; term?.write(dec(m.data ?? '')) }
    } else if (m.type === 'error') { status.value = 'closed'; closedMsg.value = m.message ?? '' }
  })
  open()
  term.focus()
})
onUnmounted(() => { if (id) engine.send({ type: 'exec.stop', id }); off?.(); ro?.disconnect(); term?.dispose() })
watch(container, open)
</script>

<template>
  <div class="term">
    <div class="frame">
      <div class="tbar">
        <span class="dot" :class="status" :title="status"></span>
        <span class="tlabel">Terminal</span>
        <span v-if="containers.length <= 1" class="mono cname">{{ container }}</span>
        <span v-else class="selwrap">
          <select v-model="container" class="input mono sel">
            <option v-for="c in containers" :key="c" :value="c">{{ c }}</option>
          </select>
          <AppIcon name="chevronDown" :size="12" class="chev" />
        </span>
        <span v-if="closedMsg" class="dim msg">{{ closedMsg }}</span>
        <span class="spacer"></span>
        <button class="icon-btn" title="Reconnect" @click="open"><AppIcon name="refresh" :size="14" /></button>
      </div>
      <div ref="host" class="host"></div>
    </div>
  </div>
</template>

<style scoped>
.term { display: flex; flex-direction: column; height: 100%; min-height: 0; }
.frame { flex: 1; min-height: 0; display: flex; flex-direction: column; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); overflow: hidden; }
.tbar { display: flex; align-items: center; gap: 8px; padding: 5px 10px; border-bottom: 1px solid var(--line); flex-shrink: 0; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--warn); flex: none; }
.dot.open { background: var(--ok); }
.dot.closed { background: var(--bad); }
.tlabel { font-size: 12.5px; font-weight: 600; color: var(--text-1); }
.cname { font-size: 12px; color: var(--text-2); }
.selwrap { position: relative; display: inline-flex; align-items: center; flex: none; }
.sel { width: 150px; height: 26px; font-size: 12px; appearance: none; padding-right: 24px; }
.chev { position: absolute; right: 7px; color: var(--text-3); pointer-events: none; }
.spacer { flex: 1; }
.msg { font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 300px; }
.host { flex: 1; min-height: 0; padding: 8px 6px 6px 10px; overflow: hidden; }
.host :deep(.xterm) { height: 100%; }
</style>
