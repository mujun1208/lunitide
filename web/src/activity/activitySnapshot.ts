import type { ActivitySnapshotDTO } from '../generated/bridge'

export function activityNeedsAttention(item: ActivitySnapshotDTO): boolean {
  if (item.phase === 'queued' || item.phase === 'awaiting_approval' || item.phase === 'running' || item.phase === 'verifying') return true
  return item.phase === 'failed' || item.phase === 'uncertain'
}

export function activityQuiet(items: ActivitySnapshotDTO[]): boolean {
  return !items.some(activityNeedsAttention)
}
