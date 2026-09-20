// One shared ticking clock so every age cell re-renders together, once a second.
import { ref } from 'vue'

export const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 1000)

export function age(iso: string, at = now.value): string {
  const s = Math.max(0, Math.floor((at - Date.parse(iso)) / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m${s % 60 ? s % 60 + 's' : ''}`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h${m % 60 ? m % 60 + 'm' : ''}`
  const d = Math.floor(h / 24)
  return `${d}d${h % 24 ? h % 24 + 'h' : ''}`
}
