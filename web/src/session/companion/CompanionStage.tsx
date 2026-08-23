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
import type { CompanionEngine, CompanionSettings } from './companionSettings'
import {
  companionEngineProbeOrder,
  defaultCompanionSettings,
  formatInterruptHotkey,
  loadCompanionSettings,
  matchesInterruptHotkey,
  saveCompanionSettings,
  voiceIdForEngineSwitch,
} from './companionSettings'
import { cleanForSpeech, cleanUserTranscript, companionInstantAck, compactSpeech, companionReplyStallMs, looksLikePlaybackEcho, prepareSpeech, takeSpeakableChunk } from './companionText'
import { MOON_RING_BINS, MoonSphere } from './MoonSphere'
import { ECHO_GUARD_MS, INTERRUPT_ECHO_MS, startCompanionSpeech, type CompanionSpeechHandle } from './speech'
import { TtsPlayer, getTtsAudioState, unlockTtsAudio } from './ttsPlayer'
import { useAutomationBroadcast } from './useAutomationBroadcast'
import { useCompanionMachine, type CompanionState } from './useCompanionMachine'

export interface CompanionStageProps {
  chatStatus: 'idle' | 'streaming' | 'done' | 'failed' | 'cancelled'
  assistantText: string
  /** Short status while tools run before the first spoken token (e.g. "浏览文件中…"). */
  activityStatus?: string
  error?: BridgeClientError
  chatReady: boolean
  onSend: (text: string) => void
  /** Cancel the in-flight LLM stream (interrupt during thinking). */
  onCancel?: () => void
  onExit: () => void
}

interface SubtitleRound {
  role: 'user' | 'assistant'
  text: string
  segments?: string[]
  activeIndex?: number
}

const STATE_LABELS: Record<CompanionState, string> = { idle: '待机', listening: '聆听中', thinking: '对答中', speaking: '说话中' }
const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)
const speakingGain = (value: number) => Math.max(0.18, Math.min(1, value))
/** Re-listen guard: stop the hands-free loop after this many silent auto-restarts in a row. */
const MAX_SILENT_RESTARTS = 3

export function CompanionStage({ chatStatus, assistantText, activityStatus, error, chatReady, onSend, onCancel, onExit }: CompanionStageProps): React.JSX.Element {
  const machine = useCompanionMachine()
  const [settings, setSettings] = useState<CompanionSettings>(defaultCompanionSettings())
  const [ttsAvailable, setTtsAvailable] = useState<boolean | undefined>(undefined)
  const [voices, setVoices] = useState<TtsVoice[]>([])
  const [degraded, setDegraded] = useState(false)
  const [circuitBroken, setCircuitBroken] = useState(false)
  const [gain, setGain] = useState(0)
  const [levels, setLevels] = useState<number[]>(idleLevels)
  const [rounds, setRounds] = useState<SubtitleRound[]>([])
  const [interimText, setInterimText] = useState('')
  const [voiceHeard, setVoiceHeard] = useState(false)
  const [listenSeconds, setListenSeconds] = useState(0)
  const [engineHint, setEngineHint] = useState('')
  const [hintVisible, setHintVisible] = useState(false)
  const [retiring, setRetiring] = useState(false)
  const [localError, setLocalError] = useState<BridgeClientError>()
  const [audioLocked, setAudioLocked] = useState(() => getTtsAudioState() !== 'running')
  const rootRef = useRef<HTMLDivElement>(null)
  const subtitleListRef = useRef<HTMLDivElement>(null)
  const entryFocusRef = useRef<Element | null>(null)
  const speechHandleRef = useRef<CompanionSpeechHandle | undefined>(undefined)
  const playerRef = useRef<TtsPlayer | undefined>(undefined)
  const handledReplyRef = useRef(chatStatus === 'done')
  const stateRef = useRef(machine.state)
  stateRef.current = machine.state
  const chatStatusRef = useRef(chatStatus)
  chatStatusRef.current = chatStatus
  const lastDeltaAtRef = useRef(0)
  const assistantTextRef = useRef(assistantText)
  assistantTextRef.current = assistantText
  const settingsRef = useRef(settings)
  settingsRef.current = settings
  /** Hands-free loop armed after the first successful microphone activation. */
  const autoLoopRef = useRef(false)
  const autoStartTriedRef = useRef(false)
  const exitedRef = useRef(false)
  const silentRestartsRef = useRef(0)
  /** Streaming TTS: track how much of assistantText has been spoken. */
  const spokenUpToRef = useRef(0)
  /** Streaming TTS: whether we are currently speaking (streaming or final). */
  const speakingRef = useRef(false)
  /** Last TTS text — used to drop speaker→mic echo as a fake user turn. */
  const lastSpokenRef = useRef('')
  /** Ignore recognizer output until this time — covers TTS + speaker ring-out. */
  const echoUntilRef = useRef(0)
  const interruptEchoRef = useRef(false)
  /** Idle re-listen waits longer after playback so room echo dies. */
  const justSpokeRef = useRef(false)
  /** User clicked 打断 / hotkey — block streaming TTS until the next user turn. */
  const userInterruptedRef = useRef(false)
  /** P0-1: Incremented when a streaming chunk finishes, forcing the streaming
   *  useEffect to re-evaluate even if assistantText hasn't changed. */
  const [streamTick, setStreamTick] = useState(0)
  const lastActivitySpokenRef = useRef('')
  const chatReadyRef = useRef(chatReady)
  chatReadyRef.current = chatReady
  const ttsAvailableRef = useRef(ttsAvailable)
  ttsAvailableRef.current = ttsAvailable
  const pendingSendRef = useRef<string | null>(null)
  const speechSyncRef = useRef<{ commitPaused: boolean; playback: boolean; echoGuardMs: number } | undefined>(undefined)
  const onSendRef = useRef(onSend)
  onSendRef.current = onSend

  const shouldAwaitMoreReply = useCallback(
    () => chatStatusRef.current === 'streaming' && !!assistantTextRef.current.trim(),
    [],
  )

  const handleEngineFallback = useCallback((engine: CompanionEngine) => {
    setSettings(current => {
      const voiceId = voiceIdForEngineSwitch(current.engine, engine, current.voiceId)
      const next = { ...current, engine, voiceId }
      saveCompanionSettings(next)
      return next
    })
    setTtsAvailable(true)
    setDegraded(false)
    setCircuitBroken(false)
  }, [])

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
    unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
    const unlock = () => {
      void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
    }
    window.addEventListener('pointerdown', unlock, { once: true })
    window.addEventListener('keydown', unlock, { once: true })
    const audioPoll = window.setInterval(() => setAudioLocked(getTtsAudioState() !== 'running'), 1200)
    let cancelled = false
    setTtsAvailable(true)
    const deferTtsWarmup = window.setTimeout(() => {
      if (cancelled) return
      const probeEngines = companionEngineProbeOrder(stored.engine)
      void (async () => {
        for (const engine of [...new Set(probeEngines)]) {
          if (cancelled) return
          try {
            const result = await getTtsBridge().voices({ engine })
            if (cancelled) return
            if (result.voices.length) {
              setVoices(result.voices)
              setTtsAvailable(true)
              if (engine !== stored.engine) setSettings(current => ({ ...current, engine }))
              return
            }
          } catch {
            /* try the next engine so a cloud-voice outage still speaks */
          }
        }
        if (!cancelled) {
          setTtsAvailable(false)
          setDegraded(true)
        }
      })()
      if (stored.engine === 'ref' && stored.autoSpeak) {
        const warmupVoiceId = stored.voiceId || ''
        const warmup = async (attempt: number) => {
          try {
            await getTtsBridge().synthesize({
              text: '嗯',
              voiceId: warmupVoiceId || undefined,
              rate: stored.rate,
              volume: 0,
              engine: 'ref',
              refEndpoint: stored.refEndpoint || undefined,
            })
          } catch (error) {
            const starting =
              error instanceof Error && (error as { code?: unknown }).code === 'M95-001' && /启动中/.test(error.message)
            if (starting && attempt < 10 && !cancelled) setTimeout(() => void warmup(attempt + 1), 8000)
          }
        }
        void getTtsBridge()
          .ensureRefEngine({ refEndpoint: stored.refEndpoint || undefined })
          .catch(() => {})
        void warmup(0)
      } else if (stored.autoSpeak) {
        void Promise.resolve(
          getTtsBridge().synthesize({
            text: '嗯',
            voiceId: stored.voiceId || undefined,
            rate: stored.rate,
            volume: 0,
            engine: stored.engine,
          }),
        ).catch(() => {})
      }
    }, 0)
    return () => {
      cancelled = true
      window.clearTimeout(deferTtsWarmup)
      window.clearInterval(audioPoll)
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

  // Pick up companion settings saved while the stage is open (same window).
  useEffect(() => {
    const refresh = () => setSettings(loadCompanionSettings())
    window.addEventListener('lunitide:companion-settings', refresh)
    window.addEventListener('storage', event => {
      if (event.key === 'lunitide:companion') refresh()
    })
    return () => {
      window.removeEventListener('lunitide:companion-settings', refresh)
    }
  }, [])

  // Streaming reply → live subtitle text. Update on every delta so the
  // user sees characters as they land; first token leaves "thinking".
  // Never paint an empty assistant row: that used to steal the user's
  // greeting onto 月汐 while the stream was still waiting for TTFT.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    const text = assistantText
    if (text.trim()) lastDeltaAtRef.current = performance.now()
    if (text.trim() && stateRef.current === 'thinking') {
      machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
    }
    setRounds(current => {
      if (!text.trim()) return current
      const last = current[current.length - 1]
      if (last?.role === 'assistant' && last.segments === undefined) {
        if (last.text === text) return current
        return [...current.slice(0, -1), { ...last, text }]
      }
      return [...current, { role: 'assistant', text }]
    })
  }, [assistantText, chatStatus, machine.dispatch])

  useEffect(() => {
    if (chatStatus === 'done' || chatStatus === 'cancelled' || chatStatus === 'failed') {
      userInterruptedRef.current = false
    }
  }, [chatStatus])

  // Ensure the final reply lands in the subtitle strip even if the stream
  // flips to "done" between React renders.
  useEffect(() => {
    if (chatStatus !== 'done') return
    const text = assistantText.trim()
    if (!text) return
    setRounds(current => {
      const last = current[current.length - 1]
      if (last?.role === 'assistant' && last.text === text) return current
      if (last?.role === 'assistant' && last.segments === undefined) {
        return [...current.slice(0, -1), { ...last, text }]
      }
      return [...current, { role: 'assistant', text }]
    })
  }, [chatStatus, assistantText])

  // Streaming TTS: speak complete sentences as they land (Doubao-style),
  // never a short comma clause. Chunks are enqueued as whole utterances
  // so the player can prefetch and join them on one timeline.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (userInterruptedRef.current) return
    if (stateRef.current !== 'speaking' && stateRef.current !== 'thinking') return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const batch: string[] = []
    while (true) {
      const pending = assistantText.slice(spokenUpToRef.current)
      const chunk = takeSpeakableChunk(pending, spokenUpToRef.current === 0)
      if (!chunk) break
      spokenUpToRef.current += chunk.consumed
      const cleaned = cleanForSpeech(chunk.text)
      if (!cleaned) continue
      if (looksLikePlaybackEcho(cleaned, lastSpokenRef.current) && compactSpeech(cleaned).length <= compactSpeech(lastSpokenRef.current).length + 4) {
        continue
      }
      batch.push(cleaned)
    }
    if (!batch.length) return
    lastSpokenRef.current = `${lastSpokenRef.current}${batch.join('')}`.slice(-1200)
    const player = ensurePlayer()
    const voiceId = activeVoiceId()
    player.configure(voiceId, settings.rate, settings.volume, settings)
    player.enqueue(batch, { ...settings, voiceId }, {
      onEngineFallback: handleEngineFallback,
      onSegmentStart: index => {
        setRounds(current => {
          const last = current[current.length - 1]
          if (last?.role !== 'assistant') return current
          return [...current.slice(0, -1), { ...last, activeIndex: index }]
        })
      },
      onGain: value => setGain(speakingGain(value)),
        onFinished: reason => {
          setGain(0)
          echoUntilRef.current = performance.now() + ECHO_GUARD_MS
          if (reason === 'engine-unavailable') {
            setTtsAvailable(false)
            setDegraded(true)
          } else if (reason === 'circuit-broken') {
            setCircuitBroken(true)
          }
          setStreamTick(t => t + 1)
          if (reason !== 'completed' || playerRef.current?.isBusy()) return
          if (stateRef.current !== 'speaking') return
          if (shouldAwaitMoreReply()) machine.dispatch({ type: 'AWAIT_MORE' })
          else if (chatStatusRef.current !== 'streaming') {
            speakingRef.current = false
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          }
        },
      onSegmentFailed: (index, consecutive) => {
        if (consecutive >= 3) setCircuitBroken(true)
      },
    })
  }, [assistantText, chatStatus, ttsAvailable, settings.autoSpeak, streamTick, handleEngineFallback])

  // If the model stalls mid-sentence, force the leftover into TTS so the
  // later turns do not sit on “说话中” with a silent player.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (userInterruptedRef.current) return
    if (stateRef.current !== 'speaking' && stateRef.current !== 'thinking') return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const timer = window.setInterval(() => {
      const pending = assistantText.slice(spokenUpToRef.current)
      if (!pending.trim()) return
      if (performance.now() - lastDeltaAtRef.current < 280) return
      const chunk = takeSpeakableChunk(pending, spokenUpToRef.current === 0, true)
      if (!chunk) return
      spokenUpToRef.current += chunk.consumed
      const cleaned = cleanForSpeech(chunk.text)
      if (!cleaned) return
      if (looksLikePlaybackEcho(cleaned, lastSpokenRef.current) && compactSpeech(cleaned).length <= compactSpeech(lastSpokenRef.current).length + 4) {
        return
      }
      lastSpokenRef.current = `${lastSpokenRef.current}${cleaned}`.slice(-1200)
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      player.enqueue([cleaned], { ...settings, voiceId }, {
        onEngineFallback: handleEngineFallback,
        onFinished: reason => {
          setGain(0)
          echoUntilRef.current = performance.now() + ECHO_GUARD_MS
          setStreamTick(t => t + 1)
          if (reason !== 'completed' || playerRef.current?.isBusy()) return
          if (stateRef.current !== 'speaking') return
          if (shouldAwaitMoreReply()) machine.dispatch({ type: 'AWAIT_MORE' })
          else if (chatStatusRef.current !== 'streaming') {
            speakingRef.current = false
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          }
        },
      })
    }, 120)
    return () => window.clearInterval(timer)
  }, [assistantText, chatStatus, ttsAvailable, settings.autoSpeak, streamTick, handleEngineFallback])

  // Terminal chat states drive the machine (TTS on → speaking).
  useEffect(() => {
    if (chatStatus === 'streaming') {
      handledReplyRef.current = false
      return
    }
    if (chatStatus === 'done' && !handledReplyRef.current) {
      handledReplyRef.current = true
      if (userInterruptedRef.current) {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
        return
      }
      const speakable = ttsAvailable !== false && settings.autoSpeak
      const completionLine = assistantText.trim() || activityStatus?.trim() || ''
      if (!completionLine.trim()) {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
        return
      }
      // P0-1: Enqueue any remaining text that wasn't picked up during streaming,
      // then flush the queue and transition the machine.
      const remaining = assistantText.trim() ? assistantText.slice(spokenUpToRef.current) : completionLine
      const segments = prepareSpeech(remaining).filter(seg => {
        if (!seg.trim()) return false
        if (looksLikePlaybackEcho(seg, lastSpokenRef.current) && compactSpeech(seg).length <= compactSpeech(lastSpokenRef.current).length + 4) {
          return false
        }
        return true
      })
      if (segments.length && speakable) {
        if (stateRef.current === 'thinking') {
          machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
        }
        const player = ensurePlayer()
        const voiceId = activeVoiceId()
        player.configure(voiceId, settings.rate, settings.volume, settings)
        lastSpokenRef.current = `${lastSpokenRef.current}${segments.join('')}`.slice(-1200)
        player.enqueue(segments, { ...settings, voiceId }, {
          onEngineFallback: handleEngineFallback,
          onSegmentStart: index => {
            setRounds(current => {
              const last = current[current.length - 1]
              if (last?.role !== 'assistant') return current
              return [...current.slice(0, -1), { ...last, activeIndex: index }]
            })
          },
          onGain: value => setGain(speakingGain(value)),
          onSegmentFailed: (index, consecutive) => {
            if (consecutive >= 3) setCircuitBroken(true)
          },
        })
        void player.flush({
          onFinished: () => {
            setGain(0)
            echoUntilRef.current = performance.now() + ECHO_GUARD_MS
            speakingRef.current = false
            if (stateRef.current === 'speaking') machine.dispatch({ type: 'PLAYBACK_ENDED' })
            else if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_TERMINAL' })
          },
        })
      } else if (speakable) {
        // All text was already consumed during streaming — wait for queue to finish
        const player = ensurePlayer()
        void player.flush({
          onFinished: () => {
            setGain(0)
            speakingRef.current = false
            if (stateRef.current === 'speaking') machine.dispatch({ type: 'PLAYBACK_ENDED' })
            else if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_TERMINAL' })
          },
        })
      } else if (!speakable) {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
      }
      return
    }
    if (chatStatus === 'failed' || chatStatus === 'cancelled') {
      if (!handledReplyRef.current && chatStatus === 'failed' && ttsAvailable !== false && settings.autoSpeak) {
        handledReplyRef.current = true
        const player = ensurePlayer()
        const voiceId = activeVoiceId()
        player.configure(voiceId, settings.rate, settings.volume, settings)
        player.enqueue(['抱歉，这次任务没有完成。'], { ...settings, voiceId }, {
          onEngineFallback: handleEngineFallback,
          onGain: value => setGain(speakingGain(value)),
          onFinished: () => {
            setGain(0)
            speakingRef.current = false
            if (stateRef.current === 'speaking') machine.dispatch({ type: 'PLAYBACK_ENDED' })
            else if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_TERMINAL' })
          },
        })
        void player.flush({ onFinished: () => machine.dispatch({ type: 'REPLY_TERMINAL' }) })
        return
      }
      if (stateRef.current === 'thinking') {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
      } else if (stateRef.current === 'speaking') {
        playerRef.current?.interrupt()
        speakingRef.current = false
        setGain(0)
        machine.dispatch({ type: 'INTERRUPT' })
      }
    }
  }, [chatStatus, assistantText, activityStatus, ttsAvailable, settings.autoSpeak, settings.rate, settings.volume, handleEngineFallback])

  // Listening timer for the status label.
  useEffect(() => {
    if (machine.state !== 'listening') {
      setListenSeconds(0)
      return
    }
    const timer = window.setInterval(() => setListenSeconds(value => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [machine.state])

  // If a turn never streams (send dropped, provider hang), don't sit on
  // “回应中” forever — drop back to listening like a missed phone sentence.
  // sendAndChat sets chatStatus to streaming before chat.start returns, so
  // a 5–6s cap cancelled live DeepSeek V4 turns during TTFT.
  useEffect(() => {
    if (machine.state !== 'thinking') return
    const waitingForFirstToken = !assistantText.trim()
    const ms = companionReplyStallMs(chatStatus === 'streaming', !waitingForFirstToken)
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'thinking') return
      if (assistantTextRef.current.trim()) return
      onCancel?.()
      setLocalError(new BridgeClientError('月汐没有及时回应，请再说一次', 'COMPANION_REPLY_STALL', true, 'renderer'))
      machine.dispatch({ type: 'REPLY_TERMINAL' })
    }, ms)
    return () => window.clearTimeout(timer)
  }, [machine.state, chatStatus, assistantText, machine, onCancel])

  // TTS drained but the stream is still open: leave “说话中” so the mic
  // hears the next utterance instead of looking frozen.
  useEffect(() => {
    if (machine.state !== 'speaking' || chatStatus !== 'streaming') return
    const timer = window.setInterval(() => {
      if (stateRef.current !== 'speaking' || chatStatusRef.current !== 'streaming') return
      if (playerRef.current?.isBusy()) return
      if (!assistantTextRef.current.trim()) return
      machine.dispatch({ type: 'AWAIT_MORE' })
    }, 500)
    return () => window.clearInterval(timer)
  }, [machine.state, chatStatus, machine])

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

  // Tool gaps before the first token: speak a short status so voice never feels dead.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (userInterruptedRef.current) return
    if (stateRef.current !== 'thinking') return
    if (assistantText.trim()) return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const line = activityStatus?.trim()
    if (!line || line === '等你确认…' || line === lastActivitySpokenRef.current) return
    lastActivitySpokenRef.current = line
    const cleaned = cleanForSpeech(line)
    if (!cleaned) return
    const player = ensurePlayer()
    const voiceId = activeVoiceId()
    player.configure(voiceId, settings.rate, settings.volume, settings)
    player.enqueue([cleaned], { ...settings, voiceId }, {
      onEngineFallback: handleEngineFallback,
      onGain: value => setGain(speakingGain(value)),
      onFinished: () => setGain(0),
    })
  }, [activityStatus, assistantText, chatStatus, ttsAvailable, settings, handleEngineFallback, ensurePlayer, activeVoiceId])

  const speakChunk = useCallback(
    (text: string) => {
      const segments = prepareSpeech(text)
      if (!segments.length) {
        speakingRef.current = false
        return
      }
      lastSpokenRef.current = `${lastSpokenRef.current}${segments.join('')}`.slice(-1200)
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      setCircuitBroken(false)
      void player.speak(segments, { ...settings, voiceId }, {
        onEngineFallback: handleEngineFallback,
        onSegmentStart: index => {
          setRounds(current => {
            const last = current[current.length - 1]
            if (last?.role !== 'assistant') return current
            return [...current.slice(0, -1), { ...last, activeIndex: index }]
          })
        },
        onGain: value => setGain(speakingGain(value)),
        onFinished: reason => {
          setGain(0)
          speakingRef.current = false
          if (reason === 'engine-unavailable') {
            setTtsAvailable(false)
            setDegraded(true)
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          } else if (reason === 'circuit-broken') {
            setCircuitBroken(true)
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          }
          // 'completed' does NOT dispatch PLAYBACK_ENDED during streaming —
          // we wait for more text or the final 'done' event.
        },
      })
    },
    [activeVoiceId, ensurePlayer, handleEngineFallback, machine, settings],
  )

  const speak = useCallback(
    (text: string) => {
      const segments = prepareSpeech(text)
      lastSpokenRef.current = `${lastSpokenRef.current}${segments.join('')}`.slice(-1200)
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
        onEngineFallback: handleEngineFallback,
        onSegmentStart: index => {
          setRounds(current => {
            const last = current[current.length - 1]
            if (last?.role !== 'assistant') return current
            return [...current.slice(0, -1), { ...last, activeIndex: index }]
          })
        },
        onGain: value => setGain(speakingGain(value)),
        onFinished: reason => {
          setGain(0)
          speakingRef.current = false
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
    [activeVoiceId, ensurePlayer, handleEngineFallback, machine, settings],
  )

  const interrupt = useCallback(() => {
    playerRef.current?.interrupt()
    setGain(0)
    speakingRef.current = false
    setCircuitBroken(false)
    setStreamTick(0)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    if (stateRef.current === 'speaking') machine.dispatch({ type: 'INTERRUPT' })
  }, [machine])

  const cancelReply = useCallback(() => {
    onCancel?.()
    spokenUpToRef.current = 0
    speakingRef.current = false
    setStreamTick(0)
    playerRef.current?.interrupt()
    setGain(0)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    if (stateRef.current === 'thinking') machine.dispatch({ type: 'INTERRUPT' })
    else if (stateRef.current === 'speaking') machine.dispatch({ type: 'INTERRUPT' })
  }, [machine, onCancel])

  const syncSpeechModes = useCallback(() => {
    const handle = speechHandleRef.current
    if (!handle) return
    const state = stateRef.current
    const ttsBusy = playerRef.current?.isBusy() === true
    const speakingAloud = state === 'speaking' || ttsBusy
    const next = {
      commitPaused: speakingAloud,
      playback: speakingAloud,
      echoGuardMs: interruptEchoRef.current ? INTERRUPT_ECHO_MS : ECHO_GUARD_MS,
    }
    interruptEchoRef.current = false
    const prev = speechSyncRef.current
    if (prev && prev.commitPaused === next.commitPaused && prev.playback === next.playback && prev.echoGuardMs === next.echoGuardMs) {
      return
    }
    speechSyncRef.current = next
    handle.setCommitPaused(next.commitPaused)
    handle.setAssistantPlayback(next.playback, next.echoGuardMs)
    handle.setBargeInActive(false)
  }, [])

  const releaseSpeakingTurn = useCallback(() => {
    setGain(0)
    speakingRef.current = false
    if (stateRef.current === 'speaking') machine.dispatch({ type: 'PLAYBACK_ENDED' })
    else if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_TERMINAL' })
    syncSpeechModes()
  }, [machine, syncSpeechModes])

  useEffect(() => {
    syncSpeechModes()
  }, [machine.state, streamTick, settings.fullDuplex, syncSpeechModes])

  // Safety net: streaming TTS can finish after chatStatus is already "done".
  // If playback drained but the stage stayed on "说话中", drop back to idle.
  useEffect(() => {
    if (machine.state !== 'speaking') return
    if (chatStatus === 'streaming') return
    const timer = window.setInterval(() => {
      if (stateRef.current !== 'speaking') return
      if (chatStatusRef.current === 'streaming') return
      if (playerRef.current?.isBusy()) return
      releaseSpeakingTurn()
    }, 350)
    return () => window.clearInterval(timer)
  }, [machine.state, chatStatus, releaseSpeakingTurn])

  useEffect(() => {
    if (machine.state !== 'thinking') return
    const issue = localError ?? error
    if (issue?.code === 'CHAT_CONFIG_MISSING') {
      machine.dispatch({ type: 'REPLY_TERMINAL' })
      syncSpeechModes()
      return
    }
    if (chatStatus === 'failed' || chatStatus === 'cancelled') {
      machine.dispatch({ type: 'REPLY_TERMINAL' })
      syncSpeechModes()
    }
  }, [chatStatus, error, localError, machine, syncSpeechModes])

  const beginUserTurn = useCallback(
    (transcript: string, viaBargeIn: boolean) => {
      // Voice never cuts TTS. Use 打断 / Tab / moon click.
      if (stateRef.current === 'speaking') {
        setInterimText('')
        return
      }
      const text = cleanUserTranscript(transcript)
      const replacing = viaBargeIn || stateRef.current === 'thinking'
      if (!text && replacing) {
        if (looksLikePlaybackEcho(transcript, lastSpokenRef.current)) {
          setInterimText('')
          return
        }
        silentRestartsRef.current = 0
        spokenUpToRef.current = 0
        speakingRef.current = false
        setStreamTick(0)
        setInterimText('')
        playerRef.current?.interrupt()
        setGain(0)
        onCancel?.()
        if (stateRef.current === 'thinking') machine.dispatch({ type: 'BARGE_IN' })
        syncSpeechModes()
        return
      }
      if (!text) return
      if (looksLikePlaybackEcho(text, lastSpokenRef.current)) {
        setInterimText('')
        return
      }
      userInterruptedRef.current = false
      silentRestartsRef.current = 0
      spokenUpToRef.current = 0
      speakingRef.current = false
      lastActivitySpokenRef.current = ''
      setStreamTick(0)
      setInterimText('')
      setLocalError(undefined)
      playerRef.current?.interrupt()
      const ack = companionInstantAck(text)
      lastSpokenRef.current = ack
      setRounds([{ role: 'user', text }, { role: 'assistant', text: ack }])
      if (ttsAvailable !== false && settings.autoSpeak) {
        const spoken = cleanForSpeech(ack)
        if (spoken) {
          const player = ensurePlayer()
          const voiceId = activeVoiceId()
          player.configure(voiceId, settings.rate, settings.volume, settings)
          player.enqueue([spoken], { ...settings, voiceId }, {
            onEngineFallback: handleEngineFallback,
            onGain: value => setGain(speakingGain(value)),
            onFinished: () => setGain(0),
          })
        }
      }
      if (replacing) {
        onCancel?.()
        if (stateRef.current === 'thinking') machine.dispatch({ type: 'BARGE_IN' })
      } else if (stateRef.current === 'idle') {
        machine.dispatch({ type: 'MIC_ACTIVATE' })
      }
      if (!chatReadyRef.current) {
        pendingSendRef.current = text
        syncSpeechModes()
        return
      }
      machine.dispatch({ type: 'RECOGNIZED_FINAL' })
      onSendRef.current(text)
      syncSpeechModes()
    },
    [activeVoiceId, ensurePlayer, handleEngineFallback, machine, onCancel, settings, syncSpeechModes, ttsAvailable],
  )

  useEffect(() => {
    if (!chatReady || !pendingSendRef.current) return
    const text = pendingSendRef.current
    pendingSendRef.current = null
    if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
    machine.dispatch({ type: 'RECOGNIZED_FINAL' })
    onSendRef.current(text)
  }, [chatReady, machine])

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
    setEngineHint('')
    setInterimText('')
    unlockTtsAudio()
    if (speechHandleRef.current) {
      syncSpeechModes()
      if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
      autoLoopRef.current = true
      setHintVisible(false)
      return
    }
    void startCompanionSpeech({
      duplex: true,
      bargeIn: false,
      environment: settingsRef.current.speechEnvironment,
      onInterim: transcript => {
        setInterimText(transcript)
        if (transcript.trim()) {
          setVoiceHeard(true)
          setEngineHint('')
          // A new utterance starts: drop the previous turn so the strip
          // only shows this round (live caption, then the committed line).
          if (stateRef.current === 'listening' || stateRef.current === 'idle') {
            setRounds([])
            setRetiring(false)
          }
        }
      },
      onSpeechStart: () => {
        setVoiceHeard(true)
        if (stateRef.current === 'listening' || stateRef.current === 'idle') {
          setRounds([])
          setRetiring(false)
        }
      },
      onVoiceEnergy: () => setVoiceHeard(true),
      onEngineHint: message => setEngineHint(message),
      onFinal: transcript => {
        beginUserTurn(transcript, false)
      },
      onError: issue => {
        speechHandleRef.current = undefined
        autoLoopRef.current = false
        setLocalError(issue)
        if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
      },
      onLevels: next => {
        setLevels(next)
      },
      onEndWithoutFinal: () => {
        if (speechHandleRef.current && stateRef.current === 'listening') {
          machine.dispatch({ type: 'MIC_CANCEL' })
        }
      },
    })
      .then(handle => {
        if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
        speechHandleRef.current = handle
        syncSpeechModes()
        autoLoopRef.current = true
        setHintVisible(false)
        void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
      })
      .catch(issue => {
        autoLoopRef.current = false
        if (auto) setHintVisible(true)
        else setLocalError(issue)
      })
  }, [beginUserTurn, machine, syncSpeechModes])

  useEffect(() => {
    if (machine.state === 'speaking') justSpokeRef.current = true
  }, [machine.state])

  // Hands-free loop: once armed, every return to idle automatically re-opens
  // the mic. After TTS, wait out speaker ring-out so the next listen is clean.
  useEffect(() => {
    if (machine.state !== 'idle' || !autoLoopRef.current || exitedRef.current) return
    const delay = justSpokeRef.current ? ECHO_GUARD_MS : 40
    justSpokeRef.current = false
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'idle' || !autoLoopRef.current || exitedRef.current) return
      if (speechHandleRef.current) {
        silentRestartsRef.current = 0
        syncSpeechModes()
        machine.dispatch({ type: 'MIC_ACTIVATE' })
        return
      }
      if (++silentRestartsRef.current > MAX_SILENT_RESTARTS) {
        autoLoopRef.current = false
        silentRestartsRef.current = 0
        setHintVisible(true)
        return
      }
      startListening(true)
    }, delay)
    return () => window.clearTimeout(timer)
  }, [machine.state, startListening, syncSpeechModes])

  useEffect(() => {
    if (machine.state !== 'listening') {
      setVoiceHeard(false)
    }
  }, [machine.state])

  // Auto-start the microphone as soon as the stage mounts. Listening does
  // not depend on chat/model readiness — only the LLM send is gated.
  const startListeningRef = useRef(startListening)
  startListeningRef.current = startListening

  useEffect(() => {
    if (machine.state !== 'listening' || !interimText.trim()) return
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'listening' || exitedRef.current) return
      if (!interimText.trim()) return
      speechHandleRef.current?.forceCommit()
    }, 500)
    return () => window.clearTimeout(timer)
  }, [machine.state, interimText])

  useEffect(() => {
    if (machine.state !== 'listening' || interimText.trim()) return
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'listening' || exitedRef.current || interimText.trim()) return
      speechHandleRef.current?.pulseRecognition()
    }, 2000)
    return () => window.clearTimeout(timer)
  }, [machine.state, interimText])

  useEffect(() => {
    if (autoStartTriedRef.current) return
    autoStartTriedRef.current = true
    const timer = window.setTimeout(() => {
      if (!exitedRef.current && stateRef.current === 'idle') startListeningRef.current(true)
    }, 0)
    return () => window.clearTimeout(timer)
  }, [])

  const stopAssistantAndListen = useCallback(() => {
    const state = stateRef.current
    if (state !== 'thinking' && state !== 'speaking') return
    userInterruptedRef.current = true
    cancelReply()
    autoLoopRef.current = true
    if (!settingsRef.current.fullDuplex || !speechHandleRef.current) startListening(false)
    else {
      syncSpeechModes()
      if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
    }
  }, [cancelReply, machine, startListening, syncSpeechModes])

  const toggleMic = useCallback(() => {
    unlockTtsAudio()
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
    if (state === 'thinking' || state === 'speaking') {
      stopAssistantAndListen()
      return
    }
    startListening(false)
  }, [machine, startListening, stopAssistantAndListen])

  const interruptAssistant = stopAssistantAndListen

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
  const interruptAssistantRef = useRef(interruptAssistant)
  interruptAssistantRef.current = interruptAssistant
  useEffect(() => {
    const onWindowKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        exitRef.current()
        return
      }
      if (!matchesInterruptHotkey(event, settingsRef.current.interruptHotkey)) return
      if (stateRef.current !== 'speaking' && stateRef.current !== 'thinking') return
      event.preventDefault()
      event.stopPropagation()
      interruptAssistantRef.current()
    }
    window.addEventListener('keydown', onWindowKeyDown, true)
    return () => window.removeEventListener('keydown', onWindowKeyDown, true)
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
    if (chatStatus === 'streaming' || machine.state === 'thinking' || machine.state === 'speaking' || machine.state === 'listening') {
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
      {audioLocked && settings.autoSpeak && ttsAvailable !== false && (
        <div className="companion-banner warn" role="status">
          轻点月亮或按空格，开启朗读声音
          <button type="button" onClick={() => void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))}>
            开启声音
          </button>
        </div>
      )}
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
      {(localError ?? error) && (localError ?? error)!.code !== 'CHAT_CONFIG_MISSING' && (
        <div className="companion-banner error" role="alert">
          {(localError ?? error)!.message}
          <span>代码 {(localError ?? error)!.code}</span>
        </div>
      )}
      {!chatReady && machine.state === 'listening' && (
        <div className="companion-banner warn" role="status">
          正在连接模型…请稍候，说完后会自动发送
        </div>
      )}
      <MoonSphere
        state={machine.state}
        gain={gain}
        levels={levels}
        interruptible={machine.state !== 'listening'}
        onInterrupt={machine.state === 'speaking' || machine.state === 'thinking' ? interruptAssistant : toggleMic}
      />
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${machine.state}`} aria-hidden="true" />
        {STATE_LABELS[machine.state]}
        {machine.state === 'listening' && (
          <span className="companion-status-sub">
            {interimText.trim() ? '正在听你说…' : voiceHeard ? '正在听…' : '我在听，请说话'}
          </span>
        )}
        {machine.state === 'thinking' && (
          <span className="companion-status-sub">
            {activityStatus?.trim() ||
              (assistantText.trim()
                ? '边写边读…'
                : chatReady
                  ? '正在回你…'
                  : '连接模型…')}
          </span>
        )}
      </div>
      {hintVisible && machine.state === 'idle' && (
        <p className="companion-hint" aria-live="polite">
          轻点月亮或按空格，开始和月汐说话；她说话时点「打断」或按 {formatInterruptHotkey(settings.interruptHotkey)} 停止
        </p>
      )}
      {/* Live subtitle strip: the current turn streams here character by
          character (wind-blown sand feel), lingers briefly once the turn
          ends and then fades away; the aria-live log still announces
          every round. */}
      <div className={`companion-subtitles${retiring ? ' retiring' : ''}`} aria-label="对话记录">
        <div className="companion-subtitle-list" aria-live="polite" role="log" ref={subtitleListRef}>
          {machine.state === 'listening' && !interimText.trim() && (
            <p className="companion-line interim listening">
              <span className="who" aria-hidden="true">我</span>
              <span className="companion-mic-live" aria-hidden="true">
                {(levels.length ? levels : idleLevels).slice(0, 8).map((level, index) => (
                  <i key={index} style={{ '--speech-level': level } as React.CSSProperties} />
                ))}
              </span>
              <span className="interim-listening">正在听…</span>
              <span className="interim-cursor" aria-hidden="true">|</span>
            </p>
          )}
          {interimText.trim() && (
            <p className="companion-line interim">
              <span className="who" aria-hidden="true">我</span>
              <span className="interim-text">{interimText}</span>
              <span className="interim-cursor" aria-hidden="true">|</span>
            </p>
          )}
          {rounds.map((round, index) => (
            <SubtitleRow key={index} round={round} />
          ))}
          {!rounds.length && !interimText && machine.state !== 'listening' && (
            <p className="companion-subtitle-hint">
              进入后即可说话；你说的话和月汐的回答都会在这里显示。
            </p>
          )}
          {machine.state === 'listening' && !interimText.trim() && listenSeconds >= 20 && (
            <p className="companion-subtitle-hint warn">
              {engineHint || '还是没有出字。请检查 Windows「设置 → 时间和语言 → 语音」里在线语音识别是否已开启，并确认系统默认麦克风正确。'}
            </p>
          )}
        </div>
      </div>
      <div className="companion-chrome">
        <button type="button" className="companion-exit" aria-label="退出月伴对话（Esc）" onClick={exit}>
          退出
        </button>
        <button
          type="button"
          className="companion-interrupt"
          aria-label={`打断月汐（${formatInterruptHotkey(settings.interruptHotkey)}）`}
          disabled={machine.state !== 'thinking' && machine.state !== 'speaking'}
          onClick={interruptAssistant}
        >
          打断
          <span className="companion-interrupt-key">{formatInterruptHotkey(settings.interruptHotkey)}</span>
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
        <span className="plain-text">{round.text}</span>
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
      <span className="plain-text">{round.text || '…'}</span>
    </p>
  )
}

const COMPANION_STARS = Array.from({ length: 140 }, (_, index) => {
  const seed = (index * 2654435761) % 1000
  const x = `${(seed % 100).toFixed(1)}%`
  const y = `${((seed / 7) % 100).toFixed(1)}%`
  const s = `${1 + (seed % 3) * 0.6}px`
  return { x, y, s, d: `${(seed % 40) / 10}s`, t: `${2.4 + (seed % 30) / 10}s`, bright: seed % 5 === 0 }
})
