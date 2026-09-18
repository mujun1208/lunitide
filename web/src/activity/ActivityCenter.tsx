import React, { useMemo, useState } from 'react'
import type { ActivitySnapshotDTO } from '../generated/bridge'
import { activityNeedsAttention } from './activitySnapshot'
import { mediaText } from '../media/mediaCopy'
import { useZh } from '../i18n/language'

export function ActivityCenter({
  items,
  open,
  onClose,
  onRecover,
}: {
  items: ActivitySnapshotDTO[]
  open: boolean
  onClose: () => void
  onRecover: (item: ActivitySnapshotDTO) => void
}): React.JSX.Element | null {
  const zh = useZh()
  const copy = mediaText(zh)
  const [filter, setFilter] = useState<'all' | 'running' | 'failed'>('all')
  const visible = useMemo(() => items.filter(item => {
    if (filter === 'running') return item.phase === 'queued' || item.phase === 'awaiting_approval' || item.phase === 'running' || item.phase === 'verifying'
    if (filter === 'failed') return item.phase === 'failed' || item.phase === 'uncertain'
    return true
  }), [items, filter])
  if (!open) return null
  return (
    <div className="activity-center" id="activity-center" role="dialog" aria-label={copy.activity}>
      <header>
        <h2>{copy.activity}</h2>
        <button type="button" onClick={onClose}>{copy.close}</button>
      </header>
      <nav aria-label={copy.activityFilter}>
        <button type="button" className={filter === 'all' ? 'is-current' : ''} onClick={() => setFilter('all')}>{copy.activityAll}</button>
        <button type="button" className={filter === 'running' ? 'is-current' : ''} onClick={() => setFilter('running')}>{copy.activityRunning}</button>
        <button type="button" className={filter === 'failed' ? 'is-current' : ''} onClick={() => setFilter('failed')}>{copy.activityFailed}</button>
      </nav>
      {visible.length === 0 ? <p>{copy.activityEmpty}</p> : (
        <ul>
          {visible.map(item => (
            <li key={item.activityId}>
              <b>{item.title}</b>
              <span>{item.phase}</span>
              {item.errorCode ? <small>{item.errorCode}</small> : null}
              {item.recoveryAction !== 'none' && activityNeedsAttention(item) ? (
                <button type="button" onClick={() => onRecover(item)}>
                  {item.recoveryAction === 'open_settings' ? copy.openSettings : item.recoveryAction === 'open_player' ? copy.openPlayer : item.recoveryAction === 'retry' ? copy.retry : copy.handle}
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      <p className="activity-center-note">{copy.historyNote}</p>
    </div>
  )
}
