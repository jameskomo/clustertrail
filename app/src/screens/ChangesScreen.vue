<script setup lang="ts">
// Every change and every decision, newest first, with the audit chain.
import { onMounted, ref } from 'vue'
import { changes, audit, refreshChanges, pendingChange } from '../composables/useChanges'
import DiffView from '../components/DiffView.vue'
import AppIcon from '../components/AppIcon.vue'
import { now } from '../composables/useClock'
import { timeAgo } from '../composables/useFormat'

const open = ref<string | null>(null)
onMounted(refreshChanges)
const tone = (s: string) => (s === 'applied' ? 'ok' : s === 'pending' ? 'warn' : s === 'failed' || s === 'rejected' ? 'bad' : '')
const authorTone = (t: string) => (t === 'agent' ? 'accent' : t === 'drift' ? 'warn' : '')
</script>

<template>
  <div class="screen">
    <Teleport defer to="#screen-actions">
      <button class="btn sm" @click="refreshChanges">Refresh</button>
    </Teleport>
    <div class="cols">
      <section class="list">
        <div v-if="!changes.length" class="empty">
          <AppIcon name="changes" :size="20" />
          <strong>No changes yet.</strong>
          <span>Scale, restart, delete or edit something. It appears here with its diff and your decision.</span>
        </div>
        <div v-for="c in changes" :key="c.id" class="chg card" :class="{ open: open === c.id }">
          <button class="head" @click="open = open === c.id ? null : c.id">
            <span class="pill" :class="tone(c.status)">{{ c.status }}</span>
            <span class="intent">{{ c.intent }}</span>
            <span class="pill" :class="authorTone(c.author.type)">{{ c.author.type === 'agent' ? 'agent' : c.author.name }}</span>
            <span class="muted meta">on <span class="mono">{{ c.cluster }}</span> · {{ timeAgo(c.createdAt, now) }} ago</span>
            <AppIcon name="chevronDown" :size="14" class="chev" />
          </button>
          <div v-if="open === c.id" class="body">
            <div class="row"><span class="dim">Touches</span>
              <span v-for="n in c.blast.namespaces" :key="n" class="pill mono">ns/{{ n }}</span>
              <span class="pill">{{ c.blast.objects }} object{{ c.blast.objects === 1 ? '' : 's' }}</span>
              <span v-if="c.blast.deletes" class="pill bad">{{ c.blast.deletes }} delete</span>
              <span v-if="c.blast.hasSecrets" class="pill warn">secrets</span>
              <button v-if="c.status === 'pending'" class="btn sm review" @click="pendingChange = c">Review</button>
            </div>
            <div v-if="c.decidedBy" class="row dim">Decided by <b>{{ c.decidedBy }}</b> {{ timeAgo(c.decidedAt!, now) }} ago<span v-if="c.note"> · “{{ c.note }}”</span></div>
            <div v-if="c.error" class="row err mono">{{ c.error }}</div>
            <DiffView :diff="c.diff" />
          </div>
        </div>
      </section>
      <aside class="audit card">
        <div class="panel-head"><h2>Audit log</h2><span class="actions dim note">each entry signs the one before</span></div>
        <div v-if="!audit.length" class="empty"><strong>Nothing signed yet.</strong><span>Decisions land here, each one hashing the one before it.</span></div>
        <div v-else class="chain">
          <div v-for="(e, idx) in audit" :key="e.seq" class="entry" :class="{ head: idx === 0 }">
            <span class="hash mono">{{ e.hash.slice(0, 7) }}</span>
            <div class="info">
              <div class="top">
                <span class="mono seq dim">#{{ e.seq }}</span>
                <span class="action">{{ e.action }}</span>
                <span v-if="idx === 0" class="pill accent">head</span>
                <span class="dim when">{{ timeAgo(e.at, now) }}</span>
              </div>
              <div class="mono sub dim">{{ e.actor }} · {{ e.changeId || e.cluster }}<span v-if="e.prev"> · after {{ e.prev.slice(0, 7) }}</span></div>
            </div>
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.cols { display: grid; grid-template-columns: minmax(0, 1fr) minmax(300px, 360px); gap: 20px; padding: 16px 20px; overflow: hidden; flex: 1; min-height: 0; align-items: start; }
.list { overflow: auto; display: flex; flex-direction: column; gap: 10px; max-height: 100%; }
.chg { transition: border-color 120ms var(--ease); }
.chg.open { border-color: var(--line-2); }
.chg .head { display: flex; align-items: center; gap: 10px; width: 100%; padding: 12px 14px; background: none; border: 0; border-radius: var(--r); text-align: left; color: var(--text-0); cursor: pointer; }
.intent { font-weight: 600; font-size: 13.5px; letter-spacing: -0.01em; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.meta { margin-left: auto; font-size: 12px; white-space: nowrap; }
.chev { color: var(--text-3); transition: transform 140ms var(--ease); }
.open .chev { transform: rotate(180deg); }
.body { display: flex; flex-direction: column; gap: 10px; padding: 12px 14px 14px; border-top: 1px solid var(--line); }
.row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 12.5px; }
.row b { color: var(--text-1); font-weight: 600; }
.review { margin-left: auto; }
.err { color: var(--bad); }
.audit { overflow: hidden; display: flex; flex-direction: column; max-height: 100%; }
.panel-head .note { font-size: 12px; font-weight: 400; }
.chain { overflow: auto; padding: 4px 0; }
.entry { position: relative; display: flex; gap: 10px; padding: 9px 14px; }
.entry::before { content: ''; position: absolute; left: 39px; top: 0; bottom: 0; width: 1px; background: var(--line); }
.entry:first-child::before { top: 18px; }
.entry:last-child::before { bottom: calc(100% - 18px); }
.entry:only-child::before { display: none; }
.hash { position: relative; z-index: 1; flex: 0 0 50px; align-self: flex-start; margin-top: 1px; padding: 1px 0; text-align: center; font-size: 11px; background: var(--panel-2); border: 1px solid var(--line-2); border-radius: var(--r-sm); color: var(--text-2); }
.entry.head .hash { background: var(--accent-soft); border-color: var(--accent-line); color: var(--accent-2); }
:root[data-theme="light"] .entry.head .hash { color: var(--accent); }
.info { flex: 1; min-width: 0; }
.top { display: flex; align-items: center; gap: 7px; }
.seq { font-size: 11px; }
.action { font-weight: 600; font-size: 12.5px; color: var(--text-0); }
.when { margin-left: auto; font-size: 11.5px; white-space: nowrap; }
.sub { font-size: 11px; margin-top: 3px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
