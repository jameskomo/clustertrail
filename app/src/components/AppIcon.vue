<script setup lang="ts">
import { computed } from 'vue'
import { ICONS, type IconName } from '../icons'

const props = withDefaults(defineProps<{ name: IconName; size?: number }>(), { size: 16 })
const markup = computed(() => ICONS[props.name])
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    class="app-icon"
    v-html="markup"
  />
</template>

<style scoped>
.app-icon { display: inline-block; flex-shrink: 0; vertical-align: -2px; }
</style>
