import { ref } from 'vue'

export interface Toast { id: number; text: string; tone: 'ok' | 'bad' | 'muted' }
export const toasts = ref<Toast[]>([])
let n = 0

export function toast(text: string, tone: Toast['tone'] = 'muted', ms = 4000) {
  const id = ++n
  toasts.value.push({ id, text, tone })
  setTimeout(() => { toasts.value = toasts.value.filter((t) => t.id !== id) }, ms)
}
