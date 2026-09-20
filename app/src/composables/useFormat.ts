export function cpu(milli: number | undefined): string {
  if (milli === undefined) return ''
  if (milli < 1000) return `${Math.round(milli)}m`
  return `${(milli / 1000).toFixed(2)}`
}

export function bytes(b: number | undefined): string {
  if (b === undefined) return ''
  const u = ['B', 'Ki', 'Mi', 'Gi', 'Ti']
  let i = 0
  let v = b
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)}${u[i]}`
}

export function timeAgo(iso: string, now: number): string {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h`
  const d = Math.floor(h / 24)
  return `${d}d`
}
