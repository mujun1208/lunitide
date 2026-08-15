/* M5 views 共享格式化工具 */

export function formatBytes(bytes: number): string {
  const kib = 1024
  const mib = kib * 1024
  const gib = mib * 1024
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  if (bytes >= gib) return `${(bytes / gib).toFixed(1)} GiB`
  if (bytes >= mib) return `${(bytes / mib).toFixed(1)} MiB`
  if (bytes >= kib) return `${(bytes / kib).toFixed(1)} KiB`
  return `${bytes} B`
}

export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function shortSha(sha: string): string {
  return sha.slice(0, 12)
}
