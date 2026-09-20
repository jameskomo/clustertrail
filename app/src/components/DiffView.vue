<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ diff: string }>()
const lines = computed(() => props.diff.split('\n').map((raw) => {
  const cls = raw.startsWith('+++') || raw.startsWith('---') ? 'meta' : raw.startsWith('+') ? 'add' : raw.startsWith('-') ? 'del' : raw.startsWith('@@') ? 'hunk' : raw.startsWith('#') ? 'head' : ''
  return { cls, marker: cls === 'add' ? '+' : cls === 'del' ? '-' : ' ', text: cls === 'add' || cls === 'del' ? raw.slice(1) : raw }
}))
</script>

<template>
  <pre class="diff mono"><div v-for="(l, i) in lines" :key="i" class="line" :class="l.cls"><span class="gutter" aria-hidden="true">{{ l.marker }}</span><span class="txt">{{ l.text }}</span></div></pre>
</template>

<style scoped>
.diff { margin: 0; padding: 6px 0; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); overflow: auto; font-size: 12.5px; line-height: 1.55; }
.line { display: flex; padding: 0 12px 0 0; white-space: pre; color: var(--text-1); }
.gutter { flex: 0 0 22px; text-align: center; color: var(--text-3); user-select: none; }
.txt { flex: 1; min-width: 0; }
.add { background: var(--ok-soft); }
.add .gutter { color: var(--ok); font-weight: 600; }
.del { background: var(--bad-soft); }
.del .gutter { color: var(--bad); font-weight: 600; }
.hunk { color: var(--chart-2); padding-top: 3px; padding-bottom: 3px; }
.hunk .gutter { color: var(--chart-2); }
.meta { color: var(--text-3); }
.head { color: var(--text-0); font-weight: 600; padding-top: 4px; padding-bottom: 4px; }
</style>
