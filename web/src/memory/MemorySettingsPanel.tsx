import React from 'react'
import type { MemorySettingsDraft } from './memorySettings'

const MODES: Array<[MemorySettingsDraft['captureMode'], string, string]> = [
  ['auto', '自动', '捕获并召回'],
  ['manual', '手动', '仅显式保存，仍可召回'],
  ['off', '关闭', '不捕获、不召回'],
]

export function MemorySettingsPanel({
  draft,
  busy,
  error,
  onChange,
  onSave,
}: {
  draft: MemorySettingsDraft
  busy?: boolean
  error?: string
  onChange: (next: MemorySettingsDraft) => void
  onSave: () => void
}): React.JSX.Element {
  return (
    <form
      className="memory-settings-panel"
      onSubmit={event => {
        event.preventDefault()
        onSave()
      }}
    >
      <div className="smart-cap-modes" role="radiogroup" aria-label="自动记忆方式">
        {MODES.map(([value, label, hint]) => (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={draft.captureMode === value}
            className={draft.captureMode === value ? 'smart-cap-mode on' : 'smart-cap-mode'}
            disabled={busy}
            onClick={() => onChange({ ...draft, captureMode: value })}
          >
            <strong>{label}</strong>
            <span>{hint}</span>
          </button>
        ))}
      </div>
      <label className="memory-switch">
        <input
          type="checkbox"
          role="switch"
          aria-label="个人记忆"
          checked={draft.personalMemoryEnabled}
          disabled={busy}
          onChange={event => onChange({ ...draft, personalMemoryEnabled: event.target.checked })}
        />
        个人记忆
      </label>
      <label className="memory-switch">
        <input
          type="checkbox"
          role="switch"
          aria-label="项目记忆"
          checked={draft.projectMemoryEnabled}
          disabled={busy}
          onChange={event => onChange({ ...draft, projectMemoryEnabled: event.target.checked })}
        />
        项目记忆
      </label>
      {error ? <p role="alert">{error}</p> : null}
      <button type="submit" className="ui-btn primary" disabled={busy}>{busy ? '保存中…' : '保存设置'}</button>
    </form>
  )
}
