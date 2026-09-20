<script setup lang="ts">
// What is running that is not in Git, and what is in Git that is not running.
// Nothing on this screen writes to a cluster on its own: applying an item
// goes through the same review gate as every other change.
import { computed, ref } from 'vue'
import { ui } from '../state'
import { engine } from '../api/socket'
import { propose } from '../composables/useChanges'
import { toast } from '../composables/useToasts'
import type { IconName } from '../icons'
import AppIcon from '../components/AppIcon.vue'
import DiffView from '../components/DiffView.vue'

interface Item { state: 'missing' | 'modified' | 'in-sync' | 'unmanaged'; kind: string; kindName: string; namespace: string; name: string; file?: string; diff?: string; yaml?: string; error?: string }
interface Report { path: string; cluster: string; scannedAt: string; files: number; items: Item[]; summary: { missing: number; modified: number; inSync: number; unmanaged: number } }

const stored = (() => { try { return localStorage.getItem('clustertrail.repo') ?? '' } catch { return '' } })()
const path = ref(stored)
// Shipped with the project so there is always something to scan.
// Relative to wherever the engine was started, so nothing here names the
// machine this was built on.
const samplePath = (import.meta.env.VITE_SAMPLE_REPO as string) || 'deploy/sample-repo'
const includeUnmanaged = ref(true)
const report = ref<Report | null>(null)
const scanning = ref(false)
const error = ref<string | null>(null)
const open = ref<string | null>(null)
const show = ref<'all' | 'missing' | 'modified' | 'unmanaged'>('all')

const scan = async () => {
  if (!path.value || !ui.cluster) return
  scanning.value = true; error.value = null
  try { localStorage.setItem('clustertrail.repo', path.value) } catch { /* private window */ }
  try {
    const m = await engine.request({ type: 'drift.scan', cluster: ui.cluster, path: path.value.trim(), namespace: ui.namespace, includeUnmanaged: includeUnmanaged.value })
    report.value = (m as any).drift as Report
  } catch (e: any) { error.value = e.message; report.value = null } finally { scanning.value = false }
}

const visible = computed(() => {
  const items = report.value?.items ?? []
  return show.value === 'all' ? items.filter((i) => i.state !== 'in-sync') : items.filter((i) => i.state === show.value)
})
const key = (i: Item) => `${i.kind}/${i.namespace}/${i.name}`

const apply = (i: Item) => {
  if (!i.yaml) return
  propose(ui.cluster!, `${i.state === 'missing' ? 'Create' : 'Revert'} ${i.kindName} ${i.name} from ${i.file ?? 'Git'}`,
    [{ op: 'apply', kind: i.kind, namespace: i.namespace, name: i.name, yaml: i.yaml }])
}
const remove = (i: Item) => propose(ui.cluster!, `Delete unmanaged ${i.kindName} ${i.name}`, [{ op: 'delete', kind: i.kind, namespace: i.namespace, name: i.name }])
const copy = async (i: Item) => {
  try { await navigator.clipboard.writeText(i.yaml ?? ''); toast(`Copied ${i.name} as YAML. Paste it into your repo.`, 'ok') }
  catch { toast('The browser would not give access to the clipboard.', 'bad') }
}

// Capturing goes the other way from applying: the cluster is right and Git is
// behind, so the objects are written into the repository on a branch. Nothing
// touches a cluster.
const capturing = ref(false)
const captureDir = ref('captured')
const pushBranch = ref(true)
const unmanagedItems = computed(() => (report.value?.items ?? []).filter((i) => i.state === 'unmanaged'))

const secretsAmong = computed(() => unmanagedItems.value.filter((i) => i.kindName === 'Secret').length)

const captureAll = async () => {
  const objects = unmanagedItems.value
    .filter((i) => i.yaml)
    .map((i) => ({ kind: i.kindName, namespace: i.namespace, name: i.name, yaml: i.yaml! }))
  if (!objects.length) return
  capturing.value = true
  try {
    const m = await engine.request({
      type: 'drift.capture', cluster: ui.cluster!, path: path.value.trim(),
      dir: captureDir.value.trim(), push: pushBranch.value, objects,
    })
    const c = (m as any).capture
    toast(c.message, 'ok', 11000)
    if (c.compareUrl) window.open(c.compareUrl, '_blank', 'noopener')
    await scan()
  } catch (e: any) { toast(e.message, 'bad', 9000) } finally { capturing.value = false }
}
const label: Record<string, string> = { missing: 'in Git, not running', modified: 'differs from Git', unmanaged: 'running, not in Git', 'in-sync': 'in sync' }
const tone: Record<string, string> = { missing: 'warn', modified: 'bad', unmanaged: 'accent', 'in-sync': 'ok' }
const icon: Record<Item['state'], IconName> = { missing: 'file', modified: 'drift', unmanaged: 'resources', 'in-sync': 'check' }
</script>

<template>
  <div class="screen">
    <Teleport defer to="#screen-actions">
      <label class="opt"><input v-model="includeUnmanaged" type="checkbox" /> also find what is running but not in Git</label>
      <input v-model="path" class="input path mono" placeholder="/path/to/the/folder/holding/your/yaml" spellcheck="false" @keyup.enter="scan" />
      <button class="btn" :disabled="!path || scanning" @click="scan"><AppIcon name="refresh" :size="14" /> {{ scanning ? 'Scanning…' : 'Scan' }}</button>
    </Teleport>

    <div v-if="error" class="err mono">{{ error }}</div>

    <div v-if="!report" class="empty">
      <AppIcon name="drift" :size="28" />
      <strong>Point ClusterTrail at the directory your cluster is deployed from.</strong>
      <span>Every YAML file under it is compared against the live objects with a server-side dry run, so defaulted fields do not show up as drift. Nothing is written.</span>
      <button class="btn sm sample" @click="path = samplePath; scan()">Try it with the sample repo</button>
      <span class="dim mono">{{ samplePath }}</span>
    </div>

    <template v-else>
      <div class="summary">
        <button class="chip" :class="{ on: show === 'all' }" @click="show = 'all'">Everything that differs</button>
        <button class="chip" :class="{ on: show === 'missing' }" @click="show = 'missing'"><span class="mono">{{ report.summary.missing }}</span> in Git, not running</button>
        <button class="chip" :class="{ on: show === 'modified' }" @click="show = 'modified'"><span class="mono">{{ report.summary.modified }}</span> differ</button>
        <button class="chip" :class="{ on: show === 'unmanaged' }" @click="show = 'unmanaged'"><span class="mono">{{ report.summary.unmanaged }}</span> running, not in Git</button>
        <span class="dim right">{{ report.summary.inSync }} in sync · {{ report.files }} files read</span>
      </div>

      <div v-if="unmanagedItems.length && (show === 'all' || show === 'unmanaged')" class="capture">
        <span>
          {{ unmanagedItems.length }} object{{ unmanagedItems.length === 1 ? '' : 's' }} running with no file behind
          {{ unmanagedItems.length === 1 ? 'it' : 'them' }}. Write {{ unmanagedItems.length === 1 ? 'it' : 'them' }} into the repository on a branch:
        </span>
        <input v-model="captureDir" class="input dir mono" placeholder="folder in the repo" spellcheck="false" />
        <label class="opt"><input v-model="pushBranch" type="checkbox" /> push and open a pull request</label>
        <span v-if="secretsAmong" class="secrets">
          {{ secretsAmong }} {{ secretsAmong === 1 ? 'is a Secret; its values are' : 'are Secrets; their values are' }}
          replaced with a placeholder, because a repository is not a secret store.
        </span>
        <button class="btn primary" :disabled="capturing" @click="captureAll">
          {{ capturing ? 'Writing…' : 'Capture into Git…' }}
        </button>
      </div>

      <div class="list">
        <div v-if="!visible.length" class="empty"><strong>Nothing to show here.</strong><span>Every object in this view agrees with Git.</span></div>
        <div v-for="i in visible" :key="key(i)" class="item card">
          <button class="head" @click="open = open === key(i) ? null : key(i)">
            <AppIcon name="chevronRight" :size="14" class="chev" :class="{ open: open === key(i) }" />
            <AppIcon :name="icon[i.state]" :size="14" class="cat" />
            <span class="pill" :class="tone[i.state]">{{ label[i.state] }}</span>
            <span class="mono what">{{ i.kindName }}/{{ i.name }}</span>
            <span class="muted where">{{ i.namespace }}</span>
            <span v-if="i.file" class="dim file mono">{{ i.file }}</span>
          </button>
          <div v-if="open === key(i)" class="body">
            <div v-if="i.error" class="err mono">{{ i.error }}</div>
            <DiffView v-if="i.diff" :diff="i.diff" class="diff" />
            <pre v-else-if="i.yaml" class="yaml mono">{{ i.yaml }}</pre>
            <div class="acts">
              <template v-if="i.state === 'unmanaged'">
                <button class="btn ghost" @click="copy(i)"><AppIcon name="copy" :size="14" /> Copy YAML for Git</button>
                <button class="btn danger" @click="remove(i)"><AppIcon name="trash" :size="14" /> Delete from cluster…</button>
              </template>
              <template v-else>
                <button class="btn ghost" @click="apply(i)"><AppIcon :name="i.state === 'missing' ? 'plus' : 'refresh'" :size="14" /> {{ i.state === 'missing' ? 'Create from Git…' : 'Revert to Git…' }}</button>
              </template>
              <span class="dim note">Every action shows its diff and is recorded before anything is written.</span>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.opt { display: flex; align-items: center; gap: 6px; color: var(--text-2); font-size: 13px; white-space: nowrap; }
.path { flex: 1; min-width: 220px; }
.capture { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin: 0 0 14px; padding: 13px 16px;
  background: var(--panel); border: 1px solid var(--line); border-left: 2px solid var(--accent); border-radius: var(--r); font-size: 14px; }
.capture .dir { width: 160px; }
.capture .secrets { flex-basis: 100%; color: var(--warn); font-size: 13px; }
.summary { display: flex; align-items: center; gap: 8px; padding: 14px 20px 10px; flex-wrap: wrap; }
.chip .mono { font-size: 12px; }
.right { margin-left: auto; font-size: 13px; }
.list { flex: 1; overflow: auto; padding: 4px 20px 20px; display: flex; flex-direction: column; gap: 8px; }
.item .head { display: flex; align-items: center; gap: 10px; width: 100%; padding: 10px 14px; background: none; border: 0; text-align: left; color: var(--text-0); }
.item .head:hover { background: var(--hover); }
.chev { color: var(--text-3); transition: transform 150ms var(--ease); }
.chev.open { transform: rotate(90deg); }
.cat { color: var(--text-2); }
.what { font-weight: 600; }
.where { font-size: 13px; }
.file { margin-left: auto; font-size: 12px; }
.body { padding: 0 14px 14px 38px; display: flex; flex-direction: column; gap: 11px; }
.diff, .yaml { max-height: 380px; }
.yaml { margin: 0; padding: 10px 12px; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r-sm); overflow: auto; font-size: 12.5px; }
.acts { display: flex; align-items: center; gap: 8px; }
.note { font-size: 12.5px; }
.err { color: var(--bad); padding: 14px 20px; white-space: pre-wrap; }
.sample { margin-top: 10px; }
</style>
