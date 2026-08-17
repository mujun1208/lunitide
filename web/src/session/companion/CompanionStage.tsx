// CompanionStage.tsx is the full-screen pure-moon voice stage: no
// subtitle bar, no control buttons — just the moon, a faint status
// pill and a ghost exit. The conversation is fully automatic: the
// first activation (auto attempt when the microphone permission is
// already granted, otherwise Space / moon click) arms a hands-free
// loop — listen → final transcript → ChatBridge → TTS reply →
// auto re-listen — so talking to 月汐 feels like a chat. Replies are
// announced through a visually-hidden aria-live log; the degradation
// chain (M95-001 banner, 3-failure circuit breaker with retry,
// cancel-receipt tolerance) is preserved. Esc exits at any moment
// (window-level, even mid-speech); Space/Enter toggles the microphone;
// finished turns fade away after a short linger; focus returns to the
// entry element on unmount.
import { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, getTtsBridge, type TtsVoice } from '../../bridge/client'
import { defaultCompanionSettings, loadCompanionSettings, saveCompanionSettings, type CompanionSettings } from './companionSettings'
import { prepareSpeech } from './companionText'
import { MOON_RING_BINS, MoonSphere } from './MoonSphere'
import { startCompanionSpeech, type CompanionSpeechHandle } from './speech'
import { TtsPlayer, unlockTtsAudio } from './ttsPlayer'
import { useAutomationBroadcast } from './useAutomationBroadcast'
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
/** Re-listen guard: stop the hands-free loop after this many silent auto-restarts in a row. */
const MAX_SILENT_RESTARTS = 3

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
  const [hintVisible, setHintVisible] = useState(false)
  const [retiring, setRetiring] = useState(false)
  const [localError, setLocalError] = useState<BridgeClientError>()
  const rootRef = useRef<HTMLDivElement>(null)
  const subtitleListRef = useRef<HTMLDivElement>(null)
  const entryFocusRef = useRef<Element | null>(null)
  const speechHandleRef = useRef<CompanionSpeechHandle | undefined>(undefined)
  const playerRef = useRef<TtsPlayer | undefined>(undefined)
  const handledReplyRef = useRef(chatStatus === 'done')
  const stateRef = useRef(machine.state)
  stateRef.current = machine.state
  /** Hands-free loop armed after the first successful microphone activation. */
  const autoLoopRef = useRef(false)
  const autoStartTriedRef = useRef(false)
  const exitedRef = useRef(false)
  const silentRestartsRef = useRef(0)

  // Mount: load settings, probe the TTS engine, unlock audio playback,
  // lock body scroll, remember the entry element for focus return.
  useEffect(() => {
    const stored = loadCompanionSettings()
    setSettings(stored)
    entryFocusRef.current = document.activeElement
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    // The stage renders inside .launch-content, whose stacking level sits
    // below .launch-sidebar — so the sidebar must be retired via a root-level
    // flag (html.companion-active) instead of relying on the overlay's z-index.
    document.documentElement.classList.add('companion-active')
    rootRef.current?.focus()
    unlockTtsAudio()
    const unlock = () => unlockTtsAudio()
    window.addEventListener('pointerdown', unlock, { once: true })
    window.addEventListener('keydown', unlock, { once: true })
    let cancelled = false
    getTtsBridge()
      .voices({ engine: stored.engine })
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
      exitedRef.current = true
      autoLoopRef.current = false
      document.body.style.overflow = previousOverflow
      document.documentElement.classList.remove('companion-active')
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

  // Default to a gentle female voice: explicit choice → zh female →
  // any female → zh → first available.
  const activeVoiceId = useCallback(() => {
    if (settings.voiceId && voices.some(voice => voice.voice_id === settings.voiceId)) return settings.voiceId
    const zh = voices.filter(voice => voice.lang.toLowerCase().startsWith('zh'))
    return (zh.find(voice => voice.gender === 'female') ?? voices.find(voice => voice.gender === 'female') ?? zh[0] ?? voices[0])?.voice_id ?? ''
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
      player.configure(voiceId, settings.rate, settings.volume, settings)
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

  // P3-4 automation→TTS linkage: a run that finishes while the stage
  // sits idle is spoken like a proactive reply — re-using the
  // retrySegment dispatch chain so the frozen machine matrix holds.
  // Busy stages never broadcast (the scheduler toast still notifies).
  useAutomationBroadcast({
    enabled: settings.enabled && settings.autoSpeak && ttsAvailable === true,
    idle: () => stateRef.current === 'idle',
    onBroadcast: text => {
      if (stateRef.current !== 'idle' || exitedRef.current) return
      setRounds([]) // a proactive announcement opens its own turn
      machine.dispatch({ type: 'MIC_ACTIVATE' })
      machine.dispatch({ type: 'RECOGNIZED_FINAL' })
      machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
      speak(text)
    },
  })

  const startListening = useCallback((auto: boolean) => {
    setLocalError(undefined)
    unlockTtsAudio()
    void startCompanionSpeech({
      onFinal: transcript => {
        speechHandleRef.current = undefined
        silentRestartsRef.current = 0
        // A new turn retires the previous one: only this user line stays,
        // the assistant reply streams in below it and both fade away with
        // the next question.
        setRounds([{ role: 'user', text: transcript }])
        if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
        machine.dispatch({ type: 'RECOGNIZED_FINAL' })
        onSend(transcript)
      },
      onError: issue => {
        speechHandleRef.current = undefined
        autoLoopRef.current = false
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
        autoLoopRef.current = true
        setHintVisible(false)
      })
      .catch(issue => {
        // Failed start disarms the loop (no infinite auto-retry);
        // silent auto attempt keeps the stage clean and just invites
        // the user to activate with Space / moon click — a gesture
        // also unlocks mic permission prompts and audio playback.
        autoLoopRef.current = false
        if (auto) setHintVisible(true)
        else setLocalError(issue)
      })
  }, [machine, onSend])

  // Hands-free loop: once armed, every return to idle (reply played,
  // interrupted, silence timeout) automatically re-opens the mic so
  // the user just keeps talking.
  useEffect(() => {
    if (machine.state !== 'idle' || !autoLoopRef.current || exitedRef.current) return
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'idle' || !autoLoopRef.current || exitedRef.current) return
      if (++silentRestartsRef.current > MAX_SILENT_RESTARTS) {
        autoLoopRef.current = false
        silentRestartsRef.current = 0
        setHintVisible(true)
        return
      }
      startListening(true)
    }, 800)
    return () => window.clearTimeout(timer)
  }, [machine.state, startListening])

  // Auto-start attempt when the chat is ready: with microphone
  // permission already granted this opens the listening loop without
  // any click; otherwise the catch shows the faint hint. The callback
  // lives in a ref so the effect (and its timer) survive unrelated
  // re-renders — e.g. the voices() probe resolving right after mount.
  const startListeningRef = useRef(startListening)
  startListeningRef.current = startListening
  useEffect(() => {
    if (autoStartTriedRef.current || !chatReady) return
    autoStartTriedRef.current = true
    const timer = window.setTimeout(() => {
      if (!exitedRef.current && stateRef.current === 'idle') startListeningRef.current(true)
    }, 400)
    return () => window.clearTimeout(timer)
  }, [chatReady])

  const toggleMic = useCallback(() => {
    unlockTtsAudio()
    if (!chatReady) {
      setLocalError(new BridgeClientError('请先配置并选择可用的供应商和模型', 'CHAT_CONFIG_MISSING', false, 'renderer'))
      return
    }
    const state = stateRef.current
    if (state === 'listening') {
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      // Manual stop pauses the hands-free loop; Space re-arms it.
      autoLoopRef.current = false
      silentRestartsRef.current = 0
      machine.dispatch({ type: 'MIC_CANCEL' })
      return
    }
    if (state === 'thinking') return
    if (state === 'speaking') {
      playerRef.current?.interrupt()
      setGain(0)
      machine.dispatch({ type: 'MIC_CLICK_WHILE_SPEAKING' })
      startListening(false)
      return
    }
    startListening(false)
  }, [chatReady, machine, startListening])

  const exit = useCallback(() => {
    exitedRef.current = true
    autoLoopRef.current = false
    speechHandleRef.current?.stop()
    speechHandleRef.current = undefined
    if (stateRef.current === 'speaking') interrupt()
    onExit()
  }, [interrupt, onExit])

  // Window-level Esc: exit works no matter where focus sits (a clicked
  // button, the document body…) and even mid-speech — the user asked for
  // an unconditional escape hatch.
  const exitRef = useRef(exit)
  exitRef.current = exit
  useEffect(() => {
    const onWindowKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      exitRef.current()
    }
    window.addEventListener('keydown', onWindowKeyDown)
    return () => window.removeEventListener('keydown', onWindowKeyDown)
  }, [])

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

  // Keep the fixed-height subtitle pill pinned to the newest text unless the
  // user scrolled up to re-read earlier turns.
  useEffect(() => {
    const box = subtitleListRef.current
    if (!box) return
    if (box.scrollHeight - box.scrollTop - box.clientHeight < 48) box.scrollTop = box.scrollHeight
  }, [rounds])

  // A finished turn lingers briefly, then fades away so the stage
  // returns to its clean moon vista (the aria-live log already carries
  // the history). Any new activity (streaming, thinking, speaking)
  // cancels the fade.
  useEffect(() => {
    if (!rounds.length) {
      setRetiring(false)
      return
    }
    if (chatStatus === 'streaming' || machine.state === 'thinking' || machine.state === 'speaking') {
      setRetiring(false)
      return
    }
    const linger = window.setTimeout(() => setRetiring(true), 2400)
    const clear = window.setTimeout(() => {
      setRounds([])
      setRetiring(false)
    }, 2400 + 800)
    return () => {
      window.clearTimeout(linger)
      window.clearTimeout(clear)
    }
  }, [chatStatus, machine.state, rounds.length])

  // Keyboard contract: Esc = exit (handled solely by the window-level
  // listener — a stage-level branch would double-fire because the same
  // keydown bubbles to window); Space/Enter = microphone (unless
  // focused on an interactive element).
  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === ' ' || event.key === 'Enter') {
      const target = event.target as HTMLElement
      if (target.closest('button,input,textarea,select,a')) return
      event.preventDefault()
      toggleMic()
    }
  }

  return (
    <div
      className="companion-stage"
      ref={rootRef}
      tabIndex={-1}
      data-state={machine.state}
      data-started={autoLoopRef.current || machine.state !== 'idle'}
      role="dialog"
      aria-modal="true"
      aria-label="月伴对话舞台"
      onKeyDown={onKeyDown}
      style={{ '--moon-gain': gain } as React.CSSProperties}
    >
      {/* 移除背景水印，星空由 CSS 处理 */}
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
          语音朗读暂时不可用
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
      <MoonSphere state={machine.state} gain={gain} levels={levels} interruptible={machine.state !== 'thinking'} onInterrupt={machine.state === 'speaking' ? interrupt : toggleMic} />
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${machine.state}`} aria-hidden="true" />
        {STATE_LABELS[machine.state]}
        {machine.state === 'listening' && <time>{`${Math.floor(listenSeconds / 60)}:${String(listenSeconds % 60).padStart(2, '0')}`}</time>}
        {machine.state === 'thinking' && <span className="companion-status-sub">月汐思考中…</span>}
      </div>
      {hintVisible && machine.state === 'idle' && (
        <p className="companion-hint" aria-live="polite">
          轻点月亮或按空格，开始和月汐说话
        </p>
      )}
      {/* Live subtitle strip: the current turn streams here character by
          character (wind-blown sand feel), lingers briefly once the turn
          ends and then fades away; the aria-live log still announces
          every round. */}
      <div className={`companion-subtitles${retiring ? ' retiring' : ''}`} aria-label="对话记录">
        <div className="companion-subtitle-list" aria-live="polite" role="log" ref={subtitleListRef}>
          {rounds.map((round, index) => (
            <SubtitleRow key={index} round={round} />
          ))}
          {!rounds.length && <p className="companion-subtitle-hint">开启麦克风后，你说的话和月汐的回答都会在这里播报。</p>}
        </div>
      </div>
      <button type="button" className="companion-exit" aria-label="退出月伴对话（Esc）" onClick={exit}>
        退出
      </button>
    </div>
  )
}

function SubtitleRow({ round }: { round: SubtitleRound }): React.JSX.Element {
  if (round.role === 'user') {
    return (
      <p className="companion-line user">
        <span className="who" aria-hidden="true">我</span>
        <StreamChars text={round.text} />
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
      <StreamChars text={round.text || '…'} />
    </p>
  )
}

/** Stream text character by character: each new character drifts in like
 *  wind-blown sand (blur + slide + fade), so streamed replies materialize
 *  continuously instead of popping in chunk by chunk. Stable per-index keys
 *  keep already-shown characters from re-animating on every stream chunk. */
function StreamChars({ text }: { text: string }): React.JSX.Element {
  return (
    <span className="stream-chars">
      {Array.from(text).map((char, index) => (
        <span key={index} className="sand" style={{ animationDelay: `${Math.min(index * 10, 260)}ms` }}>
          {char === ' ' ? '\u00A0' : char}
        </span>
      ))}
    </span>
  )
}

const COMPANION_STARS = Array.from({ length: 140 }, (_, index) => {
  const seed = (index * 2654435761) % 1000
  const x = `${(seed % 100).toFixed(1)}%`
  const y = `${((seed / 7) % 100).toFixed(1)}%`
  const s = `${1 + (seed % 3) * 0.6}px`
  return { x, y, s, d: `${(seed % 40) / 10}s`, t: `${2.4 + (seed % 30) / 10}s`, bright: seed % 5 === 0 }
})
