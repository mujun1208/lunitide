import React, { useState } from 'react'
import { formatInterruptHotkey, interruptHotkeyFromEvent, type InterruptHotkey } from '../session/companion/companionSettings'

export function Toggle({ on, onChange, label, desc }: { on: boolean; onChange: (v: boolean) => void; label: string; desc?: string }): React.JSX.Element {
  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">{label}</div>
        {desc && <div className="setting-desc">{desc}</div>}
      </div>
      <button
        className={`toggle ${on ? 'on' : ''}`}
        onClick={() => onChange(!on)}
        role="switch"
        aria-checked={on}
        aria-label={label}
      >
        <span className="toggle-knob" />
      </button>
    </div>
  )
}

export function HotkeyRow({ label, desc, hotkey, onChange }: {
  label: string
  desc: string
  hotkey: InterruptHotkey
  onChange: (next: InterruptHotkey) => void
}): React.JSX.Element {
  const [capturing, setCapturing] = useState(false)
  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">{label}</div>
        <div className="setting-desc">{desc}</div>
      </div>
      <button
        type="button"
        className={`setting-hotkey${capturing ? ' capturing' : ''}`}
        aria-label={label}
        aria-pressed={capturing}
        onClick={() => setCapturing(true)}
        onBlur={() => setCapturing(false)}
        onKeyDown={event => {
          if (!capturing) {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault()
              setCapturing(true)
            }
            return
          }
          event.preventDefault()
          event.stopPropagation()
          const next = interruptHotkeyFromEvent(event.nativeEvent)
          if (!next) return
          onChange(next)
          setCapturing(false)
        }}
      >
        {capturing ? '按下要设置的快捷键…' : formatInterruptHotkey(hotkey)}
      </button>
    </div>
  )
}

export function SelectRow({ label, desc, value, options, onChange }: {
  label: string; desc?: string; value: string; options: { value: string; label: string }[]; onChange: (v: string) => void
}): React.JSX.Element {
  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">{label}</div>
        {desc && <div className="setting-desc">{desc}</div>}
      </div>
      <select value={value} onChange={e => onChange(e.target.value)} className="setting-select">
        {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    </div>
  )
}
