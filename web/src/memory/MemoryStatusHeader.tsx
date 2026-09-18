import React from 'react'
import type { MemorySettingsDraft } from './memorySettings'
import { memoryModeLabel, memoryScopeSummary } from './memorySettings'

export function MemoryStatusHeader({
  draft,
  onOpenSettings,
}: {
  draft: MemorySettingsDraft | null
  onOpenSettings: () => void
}): React.JSX.Element {
  const mode = draft ? memoryModeLabel(draft.captureMode) : '载入中'
  const scope = draft ? memoryScopeSummary(draft) : '…'
  return (
    <header className="memory-status">
      <div>
        <div className="view-title">记忆</div>
        <p className="memory-status-summary" aria-live="polite">{mode} · {scope}</p>
      </div>
      <button type="button" className="ui-btn" onClick={onOpenSettings}>记忆设置</button>
    </header>
  )
}
