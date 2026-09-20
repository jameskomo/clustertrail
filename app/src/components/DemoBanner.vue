<script setup lang="ts">
// Says plainly that this is a recording. A demo that pretends to be live is
// a lie the first click exposes.
import { computed } from 'vue'
import { isDemo, recordedAt } from '../demo/socket'
const show = isDemo()
const when = computed(() => (recordedAt ? new Date(recordedAt).toLocaleDateString(undefined, { day: 'numeric', month: 'long' }) : ''))
</script>

<template>
  <div v-if="show" class="demo">
    <strong>This is a demo.</strong>
    <span>
      A real cluster, recorded{{ when ? ` on ${when}` : '' }}. Everything is here to look at;
      nothing can be changed, because there is no cluster behind it.
    </span>
    <a class="get" href="/">Get ClusterTrail</a>
  </div>
</template>

<style scoped>
.demo {
  display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
  padding: 9px 16px; background: var(--accent-soft);
  border-bottom: 1px solid var(--line); color: var(--text-1); font-size: 13.5px;
}
.demo strong { color: var(--text-0); }
.get { margin-left: auto; color: var(--accent-2); font-weight: 550; text-decoration: none; white-space: nowrap; }
.get:hover { text-decoration: underline; }
</style>
