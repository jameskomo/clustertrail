<script setup lang="ts">
import type { Health } from '../api/types'
defineProps<{ health?: Health; label?: string }>()
</script>

<template>
  <span class="dot" :class="health || 'none'" :title="label || health"></span>
</template>

<style scoped>
.dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; flex: none; }
.none { background: var(--line-2); }
.ok { background: var(--ok); }
.warn { background: var(--warn); }
.bad { background: var(--bad); animation: pulse 2s infinite; }
</style>
