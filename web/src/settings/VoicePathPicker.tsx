import React from 'react'
import { shownVoicePath, voicePathOptions, type ShownVoicePath, type VoicePath } from '../session/companion/voicePersonas'

export function VoicePathPicker({
  value,
  onChange,
  volcTtsReady = false,
  talkReady = false,
}: {
  value: VoicePath
  onChange: (path: ShownVoicePath) => void
  volcTtsReady?: boolean
  talkReady?: boolean
}): React.JSX.Element {
  const options = voicePathOptions(volcTtsReady)
  const shown = shownVoicePath(value)
  const select = (path: ShownVoicePath) => {
    if (path !== shown) onChange(path)
  }
  return (
    <div className="voice-path-section">
      <p className="voice-path-lead">
        月伴听写与朗读走哪条通道，和对话模型不是一回事。云端：系统识别 + 晓晓，说完再答，打断用按钮。火山听写 seed-asr、朗读 seed-tts，默认单声道级联、说完再答、打断用按钮（可在设置里开「实时全双工通话核」）。本机用 sherpa 听写、GPT-SoVITS 克隆音色，打断用按钮。豆包 App 里的温柔桃子是另一份角色库，不能拿来冒充这里的官方 speaker。
      </p>
      {volcTtsReady && shown === 'cloud' && (
        <p className="voice-path-hint" role="status">
          已配火山朗读。当前仍走晓晓；点「火山」才火山听·火山读。不会自动改通道。
        </p>
      )}
      {talkReady && (
        <p className="voice-path-hint" role="status">
          已配通话核模型。闲聊可走通话核；没接通仍用语模型。舞台接通后灯会写「通话核」。
        </p>
      )}
      <div
        className="voice-path-picker"
        role="radiogroup"
        aria-label="语音通道"
        onKeyDown={event => {
          if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft' && event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return
          event.preventDefault()
          const order = options.map(option => option.value)
          const at = order.indexOf(shown)
          const dir = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : -1
          const next = order[(at + dir + order.length) % order.length]!
          select(next)
          const node = event.currentTarget.querySelector(`[data-voice-path="${next}"]`)
          if (node instanceof HTMLElement) node.focus()
        }}
      >
        {options.map(option => {
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
