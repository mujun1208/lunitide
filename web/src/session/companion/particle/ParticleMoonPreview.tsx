import { useEffect, useMemo, useRef, useState } from 'react'
import { canUseCompanionWebgl, prefersCompanionStillVisual } from '../visual/webglSupport'
import { ParticleMoonScene } from './ParticleMoonScene'
import { parseCompanionSkinCommand, type ParticleMoonState } from './particleMoon'

const STATES: ParticleMoonState[] = ['idle', 'listening', 'thinking', 'speaking']

const STATE_LABEL: Record<ParticleMoonState, string> = {
  idle: '空闲',
  listening: '聆听',
  thinking: '思考',
  speaking: '说话',
}

const LINES: Record<ParticleMoonState, { who: string; text: string }> = {
  idle: { who: '月汐', text: '我在。星尘还绕着我转。' },
  listening: { who: '你', text: '换成粒子月亮看看。' },
  thinking: { who: '月汐', text: '收紧之后，形态接着变。' },
  speaking: { who: '月汐', text: '这是定稿对照。产品里用玉盘 / 星尘切换。' },
}

export function ParticleMoonPreview(): React.JSX.Element {
  const [state, setState] = useState<ParticleMoonState>('idle')
  const [gain, setGain] = useState(0.48)
  const [level, setLevel] = useState(0.4)
  const [cycle, setCycle] = useState(false)
  const [burstToken, setBurstToken] = useState(0)
  const [high, setHigh] = useState(true)
  const [fps, setFps] = useState(0)
  const [voice, setVoice] = useState('')
  const [voiceHit, setVoiceHit] = useState('尚未拦截（仿真）')
  const lowFps = useRef(0)
  const webgl = canUseCompanionWebgl() && !prefersCompanionStillVisual()
  const levels = useMemo(
    () => Array.from({ length: 12 }, (_, i) => Math.max(0, level * (0.45 + ((i * 13) % 11) / 16))),
    [level],
  )

  useEffect(() => {
    if (!cycle) return
    const timer = window.setInterval(() => {
      setState(current => STATES[(STATES.indexOf(current) + 1) % STATES.length])
    }, 3200)
    return () => window.clearInterval(timer)
  }, [cycle])

  useEffect(() => {
    if (state !== 'listening') return
    const timer = window.setInterval(() => setLevel(0.22 + Math.random() * 0.72), 140)
    return () => window.clearInterval(timer)
  }, [state])

  useEffect(() => {
    if (state !== 'speaking') return
    const timer = window.setInterval(() => setGain(0.3 + Math.random() * 0.64), 160)
    return () => window.clearInterval(timer)
  }, [state])

  useEffect(() => {
    if (!fps) return
    if (fps < 40 && high) {
      lowFps.current += 1
      if (lowFps.current >= 8) setHigh(false)
      return
    }
    lowFps.current = 0
  }, [fps, high])

  const pick = (next: ParticleMoonState) => {
    setCycle(false)
    setState(next)
  }

  const tryVoice = () => {
    const hit = parseCompanionSkinCommand(voice)
    if (hit === 'particle') setVoiceHit('拦截：切到星尘（进系统后才换皮肤）')
    else if (hit === 'classic') setVoiceHit('拦截：换回玉盘（进系统后才换皮肤）')
    else if (hit === 'toggle') setVoiceHit('拦截：切换风格')
    else setVoiceHit('未命中，会当普通用户句')
  }

  return (
    <div className="stardust-sim" data-state={state}>
      {webgl ? (
        <ParticleMoonScene state={state} gain={state === 'speaking' ? gain : 0.08} levels={levels} burstToken={burstToken} high={high} onFps={setFps} />
      ) : (
        <div className="stardust-sim-fallback" aria-hidden="true">
          <span className="stardust-sim-disc" />
        </div>
      )}
      <div className="stardust-sim-vignette" aria-hidden="true" />
      <header className="stardust-sim-brand">
        <p className="stardust-sim-kicker">月汐 · 独立仿真</p>
        <h1>星尘</h1>
        <p>定稿对照；产品舞台用顶栏或设置切换。</p>
      </header>
      <div className="stardust-sim-captions" aria-label="仿真字幕">
        <p>
          <span>{LINES[state].who}</span>
          {LINES[state].text}
        </p>
      </div>
      <div className="stardust-sim-status">
        <i className={`is-${state}`} />
        {STATE_LABEL[state]}
        <em>{webgl ? `${high ? '月20k·云22k' : '月9k·云9k'} · ${fps ? `${Math.round(fps)}fps` : '…'}` : 'CSS 降级'}</em>
      </div>
      <div className="stardust-sim-dock" role="toolbar" aria-label="星尘仿真控制">
        <div className="stardust-sim-row">
          {STATES.map(item => (
            <button key={item} type="button" className={state === item ? 'is-on' : ''} onClick={() => pick(item)}>
              {STATE_LABEL[item]}
            </button>
          ))}
          <button type="button" className={cycle ? 'is-on' : ''} onClick={() => setCycle(on => !on)}>
            {cycle ? '轮播中' : '自动轮播'}
          </button>
        </div>
        <div className="stardust-sim-row">
          <button
            type="button"
            onClick={() => {
              setCycle(false)
              setState('speaking')
            }}
          >
            张开
          </button>
          <button
            type="button"
            onClick={() => {
              setCycle(false)
              setState('listening')
            }}
          >
            收紧
          </button>
          <button type="button" className={high ? 'is-on' : ''} onClick={() => setHigh(on => !on)}>
            {high ? '粒子 高' : '粒子 低'}
          </button>
          <label>
            增益
            <input type="range" min={0} max={1} step={0.01} value={gain} onChange={e => setGain(Number(e.target.value))} />
          </label>
        </div>
        <div className="stardust-sim-row">
          <input
            value={voice}
            onChange={e => setVoice(e.target.value)}
            placeholder="试：贾维斯风格 / 换回普通月亮"
            aria-label="仿真语音口令"
            onKeyDown={e => {
              if (e.key === 'Enter') tryVoice()
            }}
          />
          <button type="button" onClick={tryVoice}>
            解析口令
          </button>
          <span className="stardust-sim-voice">{voiceHit}</span>
        </div>
        <p className="stardust-sim-hint">空闲：散开慢转的球。聆听/思考：同一条联动，先缩球再莲花再切形态。说话：展开，跟着声音一张一缩。</p>
      </div>
    </div>
  )
}
