export function attachmentChipMeta(name: string, size: number): string {
  const dot = name.lastIndexOf('.')
  const ext = dot > 0 && dot < name.length - 1 ? name.slice(dot + 1).replace(/[^A-Za-z0-9]/g, '').toUpperCase() : ''
  return `${ext || 'FILE'} · ${chipSize(size)}`
}

function chipSize(size: number): string {
  if (!Number.isFinite(size) || size < 1024) return `${Math.max(0, Math.round(size || 0))} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 * 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`
  return `${(size / 1024 / 1024 / 1024).toFixed(1)} GB`
}
