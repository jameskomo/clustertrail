// Theme preference: system by default, overridable, remembered per browser.
import { computed, ref, watchEffect } from 'vue'

export type Pref = 'system' | 'light' | 'dark'
const stored = (() => { try { return localStorage.getItem('clustertrail.theme') as Pref | null } catch { return null } })()
export const pref = ref<Pref>(stored ?? 'system')
const mq = window.matchMedia('(prefers-color-scheme: light)')
const systemLight = ref(mq.matches)
mq.addEventListener('change', (e) => (systemLight.value = e.matches))

export const isDark = computed(() => (pref.value === 'system' ? !systemLight.value : pref.value === 'dark'))

watchEffect(() => {
  document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light'
  try { localStorage.setItem('clustertrail.theme', pref.value) } catch { /* private window */ }
})

export function cycleTheme() {
  pref.value = pref.value === 'system' ? 'light' : pref.value === 'light' ? 'dark' : 'system'
}
export const themeLabel = computed(() => (pref.value === 'system' ? `System (${isDark.value ? 'dark' : 'light'})` : pref.value === 'dark' ? 'Dark' : 'Light'))
