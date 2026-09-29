import { useEffect, useMemo, useState } from 'react'
import { MoonSphere } from './MoonSphere'
import type { CompanionState } from './useCompanionMachine'
import { Aurora } from './visual/Aurora'
import { AURORA_STOPS, auroraForEnter } from './visual/moonVisual'
import { useCompanionEnter } from './visual/useCompanionEnter'
import { canUseCompanionWebgl } from './visual/webglSupport'
import { CompanionEntryLights } from './CompanionEntryLights'
import type { CompanionEntryReport } from './companionLights'

const STATES: CompanionState[] = ['idle', 'listening', 'thinking', 'speaking']

const STATE_LABEL: Record<CompanionState, string> = {
  idle: '空闲',
  listening: '聆听',
  thinking: '思考',
  speaking: '说话',
}

export function CompanionVisualPreview(): React.JSX.Element {
  const [replay, setReplay] = useState(0)
  const enter = useCompanionEnter(replay)
  const [state, setState] = useState<CompanionState>('idle')
  const [gain, setGain] = useState(0.45)
  const [level, setLevel] = useState(0.35)
  const [cycle, setCycle] = useState(false)
  const webgl = canUseCompanionWebgl()
  const levels = useMemo(() => Array.from({ length: 12 }, (_, i) => Math.max(0, level * (0.55 + ((i * 17) % 10) / 18))), [level])
  const aurora = auroraForEnter(state, gain, enter)
  const previewLights: CompanionEntryReport['lights'] = [
    { key: 'listen', title: '听', label: '系统识别', state: 'on' },
    { key: 'speak', title: '说', label: '晓晓', state: state === 'speaking' ? 'on' : 'warn' },
    { key: 'think', title: '想', label: state === 'thinking' ? '对话模型' : 'qwen-plus', state: state === 'idle' ? 'warn' : 'on' },
  ]

  useEffect(() => {
    if (!cycle) return
    const timer = window.setInterval(() => {
      setState(current => STATES[(STATES.indexOf(current) + 1) % STATES.length])
    }, 2800)
    return () => window.clearInterval(timer)
  }, [cycle])

  useEffect(() => {
    if (state !== 'listening') return
    const timer = window.setInterval(() => {
      setLevel(0.2 + Math.random() * 0.7)
    }, 160)
    return () => window.clearInterval(timer)
  }, [state])

  useEffect(() => {
    if (state !== 'speaking') return
    const timer = window.setInterval(() => {
      setGain(0.28 + Math.random() * 0.62)
    }, 180)
    return () => window.clearInterval(timer)
  }, [state])

  return (
    <div className="companion-stage companion-preview-stage" data-state={state}>
      <div className="companion-aurora" aria-hidden="true">
        {webgl ? (
          <Aurora colorStops={AURORA_STOPS} amplitude={aurora.amplitude} blend={aurora.blend} speed={aurora.speed} />
        ) : (
          <div className="companion-aurora-fallback" />
        )}
      </div>
      <div className="companion-banners">
        <div className="companion-banner warn persist-failed-banner" role="status">
          电脑控制未启用。第一次控桌面请到设置里打开，月伴不会自己打开。
        </div>
      </div>
      <div className="companion-stage-core">
        <div className="companion-moon-slot">
          <MoonSphere state={state} gain={state === 'speaking' ? gain : 0} levels={levels} enter={enter} interruptible={state !== 'listening'} />
        </div>
        <div className="companion-subtitles" aria-label="对话记录">
          <div className="companion-subtitle-list">
            <p className="companion-line user"><span className="who">你</span>你好，月汐</p>
            <p className="companion-line assistant"><span className="who">月汐</span>你好呀，我在呢。今天想聊点什么，或者让我帮你看点什么？</p>
          </div>
        </div>
      </div>
      <div className="companion-chrome">
        <button type="button" className="companion-exit">退出</button>
        <button type="button" className="companion-interrupt">打断<span className="companion-interrupt-key">Tab</span></button>
        <button type="button" className="companion-pause">停止</button>
        <CompanionEntryLights lights={previewLights} />
      </div>
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${state}`} aria-hidden="true" />
        {STATE_LABEL[state]}
        <span className="companion-status-sub">{state === 'speaking' ? '正在回答…' : webgl ? 'WebGL' : 'CSS 降级'}</span>
      </div>
      <div className="companion-preview-dock" role="toolbar" aria-label="预览控制">
        <div className="companion-preview-dock-row">
          <strong>月伴视觉预览</strong>
          {STATES.map(item => (
            <button key={item} type="button" className={state === item ? 'is-on' : ''} onClick={() => { setCycle(false); setState(item) }}>
              {STATE_LABEL[item]}
            </button>
          ))}
          <button type="button" className={cycle ? 'is-on' : ''} onClick={() => setCycle(on => !on)}>
            {cycle ? '轮播中' : '自动轮播'}
          </button>
          <button
            type="button"
            onClick={() => {
              setCycle(false)
              setState('idle')
              setReplay(value => value + 1)
            }}
          >
            重播入场
          </button>
        </div>
        <span className="companion-preview-hint">Cursor 里截图常是白板，请用系统浏览器看</span>
      </div>
    </div>
  )
}
