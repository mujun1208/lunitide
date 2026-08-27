import React from 'react'
import { VOICE_PATHS, type VoicePath } from '../session/companion/voicePersonas'

export function VoicePathPicker({
  value,
  onChange,
}: {
  value: VoicePath
  onChange: (path: VoicePath) => void
}): React.JSX.Element {
  return (
    <div className="voice-path-picker" role="radiogroup" aria-label="语音通道">
      {VOICE_PATHS.map(option => {
        const on = option.value === value
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={on}
            className={on ? 'voice-path-card on' : 'voice-path-card'}
            onClick={() => onChange(option.value)}
          >
            <b>{option.label}</b>
            <small>{option.desc}</small>
          </button>
        )
      })}
    </div>
  )
}
