import React from 'react'
import { shownVoicePath, VOICE_PATHS, type ShownVoicePath, type VoicePath } from '../session/companion/voicePersonas'

export function VoicePathPicker({
  value,
  onChange,
}: {
  value: VoicePath
  onChange: (path: ShownVoicePath) => void
}): React.JSX.Element {
  const shown = shownVoicePath(value)
  const select = (path: ShownVoicePath) => {
    if (path !== shown) onChange(path)
  }
  return (
    <div className="voice-path-section">
      <p className="voice-path-lead">
        月伴听写与朗读走哪条通道，和对话模型不是一回事。云端默认晓晓；火山只要 ASR（seed-asr），朗读仍是晓晓；本机用 sherpa 听写、GPT-SoVITS 克隆音色。豆包 App 里的温柔桃子等是火山 TTS 角色库，和 seed-asr 不是同一份能力，火山通道用不上。
      </p>
      <div
        className="voice-path-picker"
        role="radiogroup"
        aria-label="语音通道"
        onKeyDown={event => {
          if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft' && event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return
          event.preventDefault()
          const order = VOICE_PATHS.map(option => option.value)
          const at = order.indexOf(shown)
          const dir = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : -1
          const next = order[(at + dir + order.length) % order.length]!
          select(next)
          const node = event.currentTarget.querySelector(`[data-voice-path="${next}"]`)
          if (node instanceof HTMLElement) node.focus()
        }}
      >
        {VOICE_PATHS.map(option => {
          const on = option.value === shown
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={on}
              aria-label={`${option.label}，${option.badge}，${option.meta}`}
              data-voice-path={option.value}
              tabIndex={on ? 0 : -1}
              className={on ? 'voice-path-card on' : 'voice-path-card'}
              onClick={() => select(option.value)}
            >
              <span className="voice-path-card-top">
                <span className="voice-path-kicker">{option.kicker}</span>
                <span className="voice-path-card-marks">
                  <span className="voice-path-badge">{option.badge}</span>
                  <span className={on ? 'voice-path-pip on' : 'voice-path-pip'} aria-hidden="true" />
                </span>
              </span>
              <b className="voice-path-title">{option.label}</b>
              <span className="voice-path-meta">{option.meta}</span>
              <small className="voice-path-desc">{option.desc}</small>
            </button>
          )
        })}
      </div>
    </div>
  )
}
