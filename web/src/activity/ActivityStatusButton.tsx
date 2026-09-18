import React from 'react'
import type { ActivitySnapshotDTO } from '../generated/bridge'
import { activityQuiet } from './activitySnapshot'

export function ActivityStatusButton({
  items,
  hub,
  open,
  onToggle,
}: {
  items: ActivitySnapshotDTO[]
  hub?: boolean
  open: boolean
  onToggle: () => void
}): React.JSX.Element {
  const quiet = activityQuiet(items)
  const running = items.some(item => item.phase === 'running' || item.phase === 'verifying' || item.phase === 'queued' || item.phase === 'awaiting_approval')
  const failed = items.some(item => item.phase === 'failed' || item.phase === 'uncertain')
  const label = quiet ? '运行状态' : running ? '有任务进行中' : failed ? '有任务需要处理' : '运行状态'
  return (
    <button
      type="button"
      className={`activity-status-btn${hub ? ' is-hub' : ''}${quiet ? ' is-quiet' : ''}${failed ? ' is-alert' : ''}`}
      aria-expanded={open}
      aria-controls="activity-center"
      onClick={onToggle}
    >
      <span aria-hidden="true">{failed ? '!' : running ? '●' : '○'}</span>
      {label}
    </button>
  )
}
