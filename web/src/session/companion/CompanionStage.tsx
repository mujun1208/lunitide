// CompanionStage.tsx is the M9.5 full-screen moon stage (T-9.5.2.3 +
// MC3 integration): it composes MoonSphere, SubtitleBar and
// CompanionControls, owns the companion state machine, drives the
// TtsPlayer speech pipeline with segment-highlighted subtitles, wires
// final transcripts straight into ChatBridge, and implements the
// degradation chain (M95-001 one-shot banner, 3-failure circuit
// breaker with retry, cancel-receipt tolerance). Esc interrupts
// (speaking) or exits; Space/Enter toggles the microphone; focus
// returns to the entry button on unmount.
import { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, getTtsBridge, type TtsVoice } from '../../bridge/client'
import { defaultCompanionSettings, loadCompanionSettings, saveCompanionSettings, type CompanionSettings } from './companionSettings'
import { prepareSpeech } from './companionText'
import { MOON_RING_BINS, MoonSphere } from './MoonSphere'
import { startCompanionSpeech, type CompanionSpeechHandle } from './speech'
import { TtsPlayer } from './ttsPlayer'
import { useCompanionMachine, type CompanionState } from './useCompanionMachine'

export interface CompanionStageProps {
  chatStatus: 'idle' | 'streaming' | 'done' | 'failed' | 'cancelled'
  assistantText: string
  error?: BridgeClientError
  chatReady: boolean
  onSend: (text: string) => void
  onExit: () => void
}

interface SubtitleRound {
  role: 'user' | 'assistant'
  text: string
  segments?: string[]
  activeIndex?: number
}

const STATE_LABELS: Record<CompanionState, string> = { idle: '待机', listening: '聆听中', thinking: '思考中', speaking: '说话中' }
const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)

export function CompanionStage({ chatStatus, assistantText, error, chatReady, onSend, onExit }: CompanionStageProps): React.JSX.Element {
  const machine = useCompanionMachine()
  const [settings, setSettings] = useState<CompanionSettings>(defaultCompanionSettings)
  const [ttsAvailable, setTtsAvailable] = useState<boolean | undefined>(undefined)
  const [voices, setVoices] = useState<TtsVoice[]>([])
  const [degraded, setDegraded] = useState(false)
  const [circuitBroken, setCircuitBroken] = useState(false)
  const [gain, setGain] = useState(0)
  const [levels, setLevels] = useState<number[]>(idleLevels)
  const [rounds, setRounds] = useState<SubtitleRound[]>([])
  const [listenSeconds, setListenSeconds] = useState(0)
  const [typed, setTyped] = useState('')
  const [typing, setTyping] = useState(false)
  const [localError, setLocalError] = useState<BridgeClientError>()
  const rootRef = useRef<HTMLDivElement>(null)
  const micRef = useRef<HTMLButtonElement>(null)
  const entryFocusRef = useRef<Element | null>(null)
  const speechHandleRef = useRef<CompanionSpeechHandle | undefined>(undefined)
  const playerRef = useRef<TtsPlayer | undefined>(undefined)
  const handledReplyRef = useRef(chatStatus === 'done')
  const lastAssistantRef = useRef('')
  const followPauseRef = useRef(0)
  const subtitleRef = useRef<HTMLDivElement>(null)
  const stateRef = useRef(machine.state)
  stateRef.current = machine.state

  // Mount: load settings, probe the TTS engine, lock body scroll,
  // remember the entry element for focus return, focus the mic.
  useEffect(() => {
    const stored = loadCompanionSettings()
    setSettings(stored)
    entryFocusRef.current = document.activeElement
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    micRef.current?.focus()
    let cancelled = false
    getTtsBridge()
      .voices()
      .then(result => {
        if (cancelled) return
        setVoices(result.voices)
        setTtsAvailable(result.voices.length > 0)
        if (!result.voices.length) setDegraded(true)
      })
      .catch(() => {
        if (cancelled) return
        setTtsAvailable(false)
        setDegraded(true)
      })
    return () => {
      cancelled = true
      document.body.style.overflow = previousOverflow
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      playerRef.current?.dispose()
      playerRef.current = undefined
      const entry = entryFocusRef.current
      if (entry instanceof HTMLElement) entry.focus()
    }
  }, [])

  // Streaming reply → live subtitle text for the assistant round.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    const text = assistantText
    setRounds(current => {
      const last = current[current.length - 1]
      if (last?.role === 'assistant' && last.segments === undefined) {
        return [...current.slice(0, -1), { ...last, text }]
      }
      return [...current, { role: 'assistant', text }]
    })
  }, [assistantText, chatStatus])

  // Terminal chat states drive the machine (TTS on → speaking).
  useEffect(() => {
    if (chatStatus === 'streaming') {
      handledReplyRef.current = false
      return
    }
    if (chatStatus === 'done' && !handledReplyRef.current && assistantText.trim()) {
      handledReplyRef.current = true
      const speakable = Boolean(ttsAvailable) && settings.autoSpeak
      if (speakable) {
        machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
        speak(assistantText)
      } else {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
      }
      return
    }
    if ((chatStatus === 'failed' || chatStatus === 'cancelled') && stateRef.current === 'thinking') {
      machine.dispatch({ type: 'REPLY_TERMINAL' })
    }
  }, [chatStatus, assistantText, ttsAvailable, settings.autoSpeak])

  // Listening timer for the status label.
  useEffect(() => {
    if (machine.state !== 'listening') {
      setListenSeconds(0)
      return
    }
    const timer = window.setInterval(() => setListenSeconds(value => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [machine.state])

  const ensurePlayer = useCallback(() => {
    playerRef.current ??= new TtsPlayer()
    return playerRef.current
  }, [])

  const activeVoiceId = useCallback(() => {
    if (settings.voiceId && voices.some(voice => voice.voice_id === settings.voiceId)) return settings.voiceId
    return (voices.find(voice => voice.lang.toLowerCase().startsWith('zh')) ?? voices[0])?.voice_id ?? ''
  }, [settings.voiceId, voices])

  const speak = useCallback(
    (text: string) => {
      const segments = prepareSpeech(text)
      setRounds(current => {
        const last = current[current.length - 1]
        if (last?.role === 'assistant') {
          return [...current.slice(0, -1), { role: 'assistant', text: last.text, segments, activeIndex: undefined }]
        }
        return [...current, { role: 'assistant', text, segments, activeIndex: undefined }]
      })
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume)
      setCircuitBroken(false)
      void player.speak(segments, { ...settings, voiceId }, {
        onSegmentStart: index => {
          setRounds(current => {
            const last = current[current.length - 1]
            if (last?.role !== 'assistant') return current
            return [...current.slice(0, -1), { ...last, activeIndex: index }]
          })
        },
        onGain: value => setGain(value),
        onFinished: reason => {
          setGain(0)
          if (reason === 'engine-unavailable') {
            setTtsAvailable(false)
            setDegraded(true)
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          } else if (reason === 'circuit-broken') {
            setCircuitBroken(true)
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          } else if (reason === 'completed') {
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          }
          // 'interrupted' is dispatched synchronously by interrupt().
        },
      })
    },
    [activeVoiceId, ensurePlayer, machine, settings],
  )

  const interrupt = useCallback(() => {
    playerRef.current?.interrupt()
    setGain(0)
    setCircuitBroken(false)
    if (stateRef.current === 'speaking') machine.dispatch({ type: 'INTERRUPT' })
  }, [machine])

  const toggleMic = useCallback(() => {
    if (!chatReady) {
      setLocalError(new BridgeClientError('请先配置并选择可用的供应商和模型', 'CHAT_CONFIG_MISSING', false, 'renderer'))
      return
    }
    const state = stateRef.current
    if (state === 'listening') {
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      machine.dispatch({ type: 'MIC_CANCEL' })
      return
    }
    if (state === 'thinking') return
    if (state === 'speaking') {
      playerRef.current?.interrupt()
      setGain(0)
      machine.dispatch({ type: 'MIC_CLICK_WHILE_SPEAKING' })
      startListening()
      return
    }
    startListening()
  }, [chatReady, machine])

  const startListening = useCallback(() => {
    setLocalError(undefined)
    void startCompanionSpeech({
      onFinal: transcript => {
        speechHandleRef.current = undefined
        setRounds(current => [...current, { role: 'user', text: transcript }])
        if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
        machine.dispatch({ type: 'RECOGNIZED_FINAL' })
        onSend(transcript)
      },
      onError: issue => {
        speechHandleRef.current = undefined
        setLocalError(issue)
        if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
      },
      onLevels: setLevels,
      onEndWithoutFinal: () => {
        speechHandleRef.current = undefined
        if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
      },
    })
      .then(handle => {
        if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
        speechHandleRef.current = handle
      })
      .catch(issue => {
        setLocalError(issue)
      })
  }, [machine, onSend])

  const exit = useCallback(() => {
    speechHandleRef.current?.stop()
    speechHandleRef.current = undefined
    if (stateRef.current === 'speaking') interrupt()
    onExit()
  }, [interrupt, onExit])

  const sendTyped = () => {
    const text = typed.trim()
    if (!text || stateRef.current === 'thinking' || stateRef.current === 'speaking') return
    setTyped('')
    setTyping(false)
    setRounds(current => [...current, { role: 'user', text }])
    // Typed send follows the frozen matrix: idle→listening→thinking.
    if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
    machine.dispatch({ type: 'RECOGNIZED_FINAL' })
    onSend(text)
  }

  const toggleAutoSpeak = () => {
    setSettings(current => {
      const next = { ...current, autoSpeak: !current.autoSpeak }
      saveCompanionSettings(next)
      return next
    })
  }

  const retrySegment = () => {
    setCircuitBroken(false)
    const last = rounds[rounds.length - 1]
    if (last?.role === 'assistant' && last.text) {
      machine.dispatch({ type: 'MIC_ACTIVATE' })
      machine.dispatch({ type: 'RECOGNIZED_FINAL' })
      machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
      speak(last.text)
    }
  }

  // Keyboard contract: Esc = interrupt (speaking) or exit; Space/Enter
  // = microphone (unless typing in the folded composer).
  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      if (typing) {
        setTyping(false)
        return
      }
      if (stateRef.current === 'speaking') interrupt()
      else exit()
      return
    }
    if (event.key === ' ' || event.key === 'Enter') {
      const target = event.target as HTMLElement
      // Only interactive elements swallow Space/Enter here; a bare
      // `[role]` selector would match the dialog root itself for every
      // descendant and kill the stage-level mic shortcut (MC-06).
      if (target.closest('input,textarea,button,select,a')) return
      event.preventDefault()
      toggleMic()
    }
  }

  const followSubtitle = (force = false) => {
    const box = subtitleRef.current
    if (!box) return
    const now = performance.now()
    if (!force && now < followPauseRef.current) return
    box.scrollTop = box.scrollHeight
  }

  const onSubtitleScroll = () => {
    const box = subtitleRef.current
    if (!box) return
    const nearBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40
    if (nearBottom) followPauseRef.current = 0
    else if (!followPauseRef.current) followPauseRef.current = performance.now() + 3000
  }

  useEffect(() => {
    followSubtitle()
  }, [rounds])

  const activeIndex = (() => {
    const last = rounds[rounds.length - 1]
    return last?.role === 'assistant' && last.segments ? last.activeIndex : undefined
  })()
  const lastAssistant = [...rounds].reverse().find(round => round.role === 'assistant')

  return (
    <div className="companion-stage" ref={rootRef} data-state={machine.state} role="dialog" aria-modal="true" aria-label="月伴对话舞台" onKeyDown={onKeyDown} style={{ '--moon-gain': gain } as React.CSSProperties}>
      <div className="companion-stars" aria-hidden="true">
        {COMPANION_STARS.map((star, index) => (
          <i key={index} style={{ left: star.x, top: star.y, width: star.s, height: star.s, animationDelay: star.d, animationDuration: star.t }} className={star.bright ? 'bright' : undefined} />
        ))}
      </div>
      {degraded && (
        <div className="companion-banner" role="status">
          本机无语音合成引擎，已切换字幕模式
          <button type="button" onClick={() => setDegraded(false)}>
            知道了
          </button>
        </div>
      )}
      {circuitBroken && (
        <div className="companion-banner warn" role="status">
          语音朗读暂时不可用，字幕不受影响
          <button type="button" onClick={retrySegment}>
            重试本段朗读
          </button>
          <button type="button" onClick={() => setCircuitBroken(false)}>
            忽略
          </button>
        </div>
      )}
      {(localError ?? error) && (
        <div className="companion-banner error" role="alert">
          {(localError ?? error)!.message}
          <span>代码 {(localError ?? error)!.code}</span>
        </div>
      )}
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${machine.state}`} aria-hidden="true" />
        {STATE_LABELS[machine.state]}
        {machine.state === 'listening' && <time>{`${Math.floor(listenSeconds / 60)}:${String(listenSeconds % 60).padStart(2, '0')}`}</time>}
        {machine.state === 'thinking' && <span className="companion-status-sub">月汐思考中…</span>}
      </div>
      <MoonSphere state={machine.state} gain={gain} levels={levels} interruptible={machine.state === 'speaking'} onInterrupt={interrupt} />
      <div className="companion-subtitles" ref={subtitleRef} tabIndex={0} aria-label="对话字幕" onScroll={onSubtitleScroll}>
        <div className="companion-subtitle-list" aria-live="polite" role="log">
          {rounds.map((round, index) => (
            <SubtitleRow key={index} round={round} />
          ))}
          {!rounds.length && <p className="companion-subtitle-hint">点击话筒或按空格开始说话，也可以展开输入框打字。</p>}
        </div>
      </div>
      {machine.state === 'speaking' && lastAssistant?.segments && activeIndex !== undefined && (
        <p className="companion-active-line" aria-hidden="true">
          {lastAssistant.segments[activeIndex]}
        </p>
      )}
      {typing && (
        <form
          className="companion-typing"
          onSubmit={event => {
            event.preventDefault()
            sendTyped()
          }}
        >
          <input
            value={typed}
            autoFocus
            aria-label="文字输入"
            placeholder="输入文字，Enter 发送，Esc 收起"
            onChange={event => setTyped(event.target.value)}
            onKeyDown={event => {
              if (event.key === 'Escape') {
                event.stopPropagation()
                setTyping(false)
              }
            }}
          />
          <button type="submit" className="primary" disabled={!typed.trim()}>
            发送
          </button>
        </form>
      )}
      <div className="companion-controls" role="toolbar" aria-label="月伴控制">
        <button
          type="button"
          ref={micRef}
          className={`companion-mic state-${machine.state}`}
          aria-label={machine.state === 'listening' ? '取消语音输入' : '语音输入（空格）'}
          aria-pressed={machine.state === 'listening'}
          disabled={!chatReady && machine.state === 'idle'}
          onClick={toggleMic}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
            <path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8" />
          </svg>
        </button>
        <button
          type="button"
          className="companion-tts-toggle"
          aria-label={settings.autoSpeak ? '关闭自动朗读' : '开启自动朗读'}
          aria-pressed={settings.autoSpeak}
          disabled={!ttsAvailable}
          title={ttsAvailable === undefined ? '正在检测语音引擎…' : ttsAvailable ? '自动朗读开关' : '本机无语音合成引擎'}
          onClick={toggleAutoSpeak}
        >
          {settings.autoSpeak && ttsAvailable ? '🔊' : '🔇'} 朗读
        </button>
        {!typing && machine.state !== 'listening' && (
          <button type="button" className="companion-type-toggle" aria-label="展开文字输入" onClick={() => setTyping(true)}>
            ⌨ 输入
          </button>
        )}
        <button type="button" className="companion-exit" aria-label="退出月伴舞台（Esc）" onClick={exit}>
          退出
        </button>
      </div>
    </div>
  )
}

function SubtitleRow({ round }: { round: SubtitleRound }): React.JSX.Element {
  if (round.role === 'user') {
    return (
      <p className="companion-line user">
        <span className="who" aria-hidden="true">我</span>
        {round.text}
      </p>
    )
  }
  if (round.segments) {
    return (
      <p className="companion-line assistant">
        <span className="who" aria-hidden="true">月汐</span>
        {round.segments.map((segment, index) => (
          <span key={index} className={`segment${round.activeIndex === index ? ' active' : ''}${round.activeIndex !== undefined && index < round.activeIndex ? ' spoken' : ''}`}>
            {segment}
          </span>
        ))}
      </p>
    )
  }
  return (
    <p className="companion-line assistant">
      <span className="who" aria-hidden="true">月汐</span>
      {round.text || '…'}
    </p>
  )
}

const COMPANION_STARS = Array.from({ length: 46 }, (_, index) => {
  const seed = (index * 2654435761) % 1000
  const x = `${(seed % 100).toFixed(1)}%`
  const y = `${((seed / 7) % 100).toFixed(1)}%`
  const s = `${1 + (seed % 3) * 0.6}px`
  return { x, y, s, d: `${(seed % 40) / 10}s`, t: `${3 + (seed % 30) / 10}s`, bright: seed % 7 === 0 }
})
