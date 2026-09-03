import React from 'react'
import { previewPendingMemory, type PendingMemoryItem } from './pendingMemory'

export function PendingMemoryBanner({
  item,
  busy = false,
  overlay = false,
  onConfirm,
  onLater,
}: {
  item: PendingMemoryItem
  busy?: boolean
  overlay?: boolean
  onConfirm: () => void
  onLater: () => void
}): React.JSX.Element {
  const memory = (item.kind ?? 'memory') === 'memory'
  return (
    <div className={`pending-memory-banner${overlay ? ' companion-float' : ''}`} role="status" aria-label={memory ? '待确认偏好' : '待确认'}>
      <span>{memory ? `待确认偏好（确认后才进长期记忆）：${previewPendingMemory(item.content)}` : previewPendingMemory(item.content)}</span>
      <button type="button" disabled={busy} onClick={onConfirm}>{busy ? '处理中…' : '确认沉淀'}</button>
      <button type="button" disabled={busy} onClick={onLater}>以后再说</button>
    </div>
  )
}
