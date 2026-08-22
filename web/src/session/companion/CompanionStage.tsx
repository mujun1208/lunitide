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
import { cleanForSpeech, cleanUserTranscript, looksLikePlaybackEcho, prepareSpeech, takeSpeakableChunk } from './companionText'
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

const STATE_LABELS: Record<CompanionState, string> = { idle: '待机', listening: '聆听中', thinking: '回应中', speaking: '说话中' }
const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)
const speakingGain = (value: number) => Math.max(0.18, Math.min(1, value))
/** Re-listen guard: stop the hands-free loop after this many silent auto-restarts in a row. */
const MAX_SILENT_RESTARTS = 3

export function CompanionStage({ chatStatus, assistantText, activityStatus, error, chatReady, onSend, onCancel, onExit }: CompanionStageProps): React.JSX.Element {
  const machine = useCompanionMachine()
  const [settings, setSettings] = useState<CompanionSettings>(defaultCompanionSettings)
  const [ttsAvailable, setTtsAvailable] = useState<boolean | undefined>(undefined)
  const [voices, setVoices] = useState<TtsVoice[]>([])
  const [degraded, setDegraded] = useState(false)
  const [circuitBroken, setCircuitBroken] = useState(false)
  const [gain, setGain] = useState(0)
  const [levels, setLevels] = useState<number[]>(idleLevels)
  const [rounds, setRounds] = useState<SubtitleRound[]>([])
  const [interimText, setInterimText] = useState('')
  const [listenSeconds, setListenSeconds] = useState(0)
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
  /** Idle re-listen waits longer after playback so room echo dies. */
  const justSpokeRef = useRef(false)
  /** P0-1: Incremented when a streaming chunk finishes, forcing the streaming
   *  useEffect to re-evaluate even if assistantText hasn't changed. */
  const [streamTick, setStreamTick] = useState(0)

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
    const probeEngines: CompanionSettings['engine'][] =
      stored.engine === 'ref' ? ['ref', 'edge', 'natural', 'sapi'] : stored.engine === 'edge' ? ['edge', 'natural', 'sapi'] : [stored.engine, 'edge', 'natural', 'sapi']
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
    // P0-3: Warm up GPT-SoVITS when using ref engine. First ask the
    // backend to auto-host the model server (non-blocking spawn when
    // 9880 is down), then send a short silent synthesis so the model
    // finishes loading before the first real segment. Retries cover the
    // 30-90s cold-load window; the result is discarded (not played).
    if (stored.engine === 'ref' && stored.autoSpeak) {
      const warmupVoiceId = stored.voiceId || ''
      const warmup = async (attempt: number) => {
        try {
          await getTtsBridge().synthesize({
            text: '嗯',
            voiceId: warmupVoiceId || undefined,
            rate: stored.rate,
            volume: 0, // volume 0 so even if it somehow plays, it's silent
            engine: 'ref',
            refEndpoint: stored.refEndpoint || undefined,
          })
        } catch (error) {
          // "语音引擎启动中" → the hosted server is still loading:
          // keep waiting instead of giving up on the warm-up.
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
    return () => {
      cancelled = true
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

  // Streaming reply → live subtitle text. Update on every delta so the
  // user sees characters as they land; first token leaves "thinking".
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    lastDeltaAtRef.current = performance.now()
    const text = assistantText
    if (text.trim() && stateRef.current === 'thinking') {
      machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
    }
    setRounds(current => {
      const last = current[current.length - 1]
      if (last?.role === 'assistant' && last.segments === undefined) {
        if (last.text === text) return current
        return [...current.slice(0, -1), { ...last, text }]
      }
      return [...current, { role: 'assistant', text }]
    })
  }, [assistantText, chatStatus, machine.dispatch])

  // Streaming TTS: speak complete sentences as they land (Doubao-style),
  // never a short comma clause. Chunks are enqueued as whole utterances
  // so the player can prefetch and join them on one timeline.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const batch: string[] = []
    while (true) {
      const pending = assistantText.slice(spokenUpToRef.current)
      const chunk = takeSpeakableChunk(pending, spokenUpToRef.current === 0)
      if (!chunk) break
      spokenUpToRef.current += chunk.consumed
      const cleaned = cleanForSpeech(chunk.text)
      if (cleaned) batch.push(cleaned)
    }
    if (!batch.length) return
    lastSpokenRef.current = `${lastSpokenRef.current}${batch.join('')}`.slice(-1200)
    const player = ensurePlayer()
    const voiceId = activeVoiceId()
    player.configure(voiceId, settings.rate, settings.volume, settings)
    player.enqueue(batch, { ...settings, voiceId }, {
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
        if (reason === 'engine-unavailable') {
          setTtsAvailable(false)
          setDegraded(true)
        } else if (reason === 'circuit-broken') {
          setCircuitBroken(true)
        }
        setStreamTick(t => t + 1)
        if (reason !== 'completed' || playerRef.current?.isBusy()) return
        if (stateRef.current !== 'speaking') return
        if (chatStatusRef.current === 'streaming') machine.dispatch({ type: 'AWAIT_MORE' })
        else {
          speakingRef.current = false
          machine.dispatch({ type: 'PLAYBACK_ENDED' })
        }
      },
      onSegmentFailed: (index, consecutive) => {
        if (consecutive >= 3) setCircuitBroken(true)
      },
    })
  }, [assistantText, chatStatus, ttsAvailable, settings.autoSpeak, streamTick])

  // If the model stalls mid-sentence, force the leftover into TTS so the
  // later turns do not sit on “说话中” with a silent player.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const timer = window.setInterval(() => {
      const pending = assistantText.slice(spokenUpToRef.current)
      if (!pending.trim()) return
      if (performance.now() - lastDeltaAtRef.current < 420) return
      const chunk = takeSpeakableChunk(pending, spokenUpToRef.current === 0, true)
      if (!chunk) return
      spokenUpToRef.current += chunk.consumed
      const cleaned = cleanForSpeech(chunk.text)
      if (!cleaned) return
      lastSpokenRef.current = `${lastSpokenRef.current}${cleaned}`.slice(-1200)
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      player.enqueue([cleaned], { ...settings, voiceId }, {
        onFinished: reason => {
          setGain(0)
          setStreamTick(t => t + 1)
          if (reason !== 'completed' || playerRef.current?.isBusy()) return
          if (stateRef.current !== 'speaking') return
          if (chatStatusRef.current === 'streaming') machine.dispatch({ type: 'AWAIT_MORE' })
          else {
            speakingRef.current = false
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          }
        },
      })
    }, 180)
    return () => window.clearInterval(timer)
  }, [assistantText, chatStatus, ttsAvailable, settings.autoSpeak, streamTick])

  // Terminal chat states drive the machine (TTS on → speaking).
  useEffect(() => {
    if (chatStatus === 'streaming') {
      handledReplyRef.current = false
      return
    }
    if (chatStatus === 'done' && !handledReplyRef.current) {
      handledReplyRef.current = true
      const speakable = ttsAvailable !== false && settings.autoSpeak
      const completionLine = assistantText.trim() || activityStatus?.trim() || ''
      if (!completionLine.trim()) {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
        return
      }
      // P0-1: Enqueue any remaining text that wasn't picked up during streaming,
      // then flush the queue and transition the machine.
      const remaining = assistantText.trim() ? assistantText.slice(spokenUpToRef.current) : completionLine
      if (remaining.trim() && speakable) {
        if (stateRef.current === 'thinking') {
          machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
        }
        const player = ensurePlayer()
        const voiceId = activeVoiceId()
        player.configure(voiceId, settings.rate, settings.volume, settings)
        const segments = prepareSpeech(remaining)
        lastSpokenRef.current = `${lastSpokenRef.current}${segments.join('')}`.slice(-1200)
        player.enqueue(segments, { ...settings, voiceId }, {
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
            speakingRef.current = false
            if (stateRef.current === 'speaking') machine.dispatch({ type: 'PLAYBACK_ENDED' })
            else if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_TERMINAL' })
          },
        })
      } else if (!remaining.trim() && speakable) {
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
  }, [chatStatus, assistantText, activityStatus, ttsAvailable, settings.autoSpeak, settings.rate, settings.volume])

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
  useEffect(() => {
    if (machine.state !== 'thinking') return
    const ms = chatStatus === 'streaming' ? 45000 : 12000
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'thinking') return
      if (assistantText.trim()) return
      onCancel?.()
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
    [activeVoiceId, ensurePlayer, machine, settings],
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
    [activeVoiceId, ensurePlayer, machine, settings],
  )

  const interrupt = useCallback(() => {
    playerRef.current?.interrupt()
    setGain(0)
    speakingRef.current = false
    setCircuitBroken(false)
    setStreamTick(0)
    if (stateRef.current === 'speaking') machine.dispatch({ type: 'INTERRUPT' })
  }, [machine])

  const cancelReply = useCallback(() => {
    onCancel?.()
    spokenUpToRef.current = 0
    speakingRef.current = false
    setStreamTick(0)
    playerRef.current?.interrupt()
    setGain(0)
    if (stateRef.current === 'thinking') machine.dispatch({ type: 'INTERRUPT' })
    else if (stateRef.current === 'speaking') machine.dispatch({ type: 'INTERRUPT' })
  }, [machine, onCancel])

  const syncSpeechModes = useCallback(() => {
    const handle = speechHandleRef.current
    if (!handle || !settingsRef.current.fullDuplex) return
    const speaking = stateRef.current === 'speaking'
    // Mute + ignore SR for the whole speaking state, not only while the
    // player reports busy — TTS fetch delay is when speaker echo starts.
    handle.setCommitPaused(speaking)
    handle.setAssistantPlayback(speaking, stateRef.current === 'listening' ? INTERRUPT_ECHO_MS : ECHO_GUARD_MS)
    handle.setBargeInActive(settingsRef.current.bargeIn && (stateRef.current === 'thinking' || speaking))
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
  }, [machine.state, streamTick, settings.fullDuplex, settings.bargeIn, syncSpeechModes])

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

  const beginUserTurn = useCallback(
    (transcript: string, viaBargeIn: boolean) => {
      const text = cleanUserTranscript(transcript)
      const barge = viaBargeIn || stateRef.current === 'thinking' || stateRef.current === 'speaking'
      if (!text && barge) {
        silentRestartsRef.current = 0
        spokenUpToRef.current = 0
        speakingRef.current = false
        setStreamTick(0)
        setInterimText('')
        playerRef.current?.interrupt()
        setGain(0)
        onCancel?.()
        if (stateRef.current === 'speaking' || stateRef.current === 'thinking') machine.dispatch({ type: 'BARGE_IN' })
        syncSpeechModes()
        return
      }
      if (!text) return
      if (looksLikePlaybackEcho(text, lastSpokenRef.current)) {
        setInterimText('')
        return
      }
      silentRestartsRef.current = 0
      spokenUpToRef.current = 0
      speakingRef.current = false
      lastSpokenRef.current = ''
      setStreamTick(0)
      setInterimText('')
      playerRef.current?.interrupt()
      setRounds([{ role: 'user', text: text }])
      if (barge) {
        onCancel?.()
        if (stateRef.current === 'speaking' || stateRef.current === 'thinking') machine.dispatch({ type: 'BARGE_IN' })
      } else if (stateRef.current === 'idle') {
        machine.dispatch({ type: 'MIC_ACTIVATE' })
      }
      machine.dispatch({ type: 'RECOGNIZED_FINAL' })
      onSend(text)
      syncSpeechModes()
    },
    [machine, onCancel, onSend, syncSpeechModes],
  )

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
    setInterimText('')
    unlockTtsAudio()
    const duplex = settingsRef.current.fullDuplex
    if (duplex && speechHandleRef.current) {
      syncSpeechModes()
      if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
      autoLoopRef.current = true
      setHintVisible(false)
      return
    }
    void startCompanionSpeech({
      duplex,
      bargeIn: settingsRef.current.bargeIn,
      environment: settingsRef.current.speechEnvironment,
      onInterim: transcript => setInterimText(transcript),
      onFinal: transcript => {
        if (!duplex) speechHandleRef.current = undefined
        beginUserTurn(transcript, false)
      },
      onBargeIn: transcript => beginUserTurn(transcript, true),
      onError: issue => {
        speechHandleRef.current = undefined
        autoLoopRef.current = false
        setLocalError(issue)
        if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
      },
      onLevels: setLevels,
      onEndWithoutFinal: () => {
        if (duplex && speechHandleRef.current) {
          if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
          return
        }
        speechHandleRef.current = undefined
        if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
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
      if (settingsRef.current.fullDuplex && speechHandleRef.current) {
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
    }, 150)
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
    if (state === 'thinking') {
      cancelReply()
      if (!settingsRef.current.fullDuplex || !speechHandleRef.current) startListening(false)
      else syncSpeechModes()
      return
    }
    if (state === 'speaking') {
      playerRef.current?.interrupt()
      setGain(0)
      machine.dispatch({ type: 'MIC_CLICK_WHILE_SPEAKING' })
      if (!settingsRef.current.fullDuplex || !speechHandleRef.current) startListening(false)
      else syncSpeechModes()
      return
    }
    startListening(false)
  }, [chatReady, cancelReply, machine, startListening])

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
      {(localError ?? error) && (
        <div className="companion-banner error" role="alert">
          {(localError ?? error)!.message}
          <span>代码 {(localError ?? error)!.code}</span>
        </div>
      )}
      <MoonSphere
        state={machine.state}
        gain={gain}
        levels={levels}
        interruptible={machine.state !== 'listening'}
        onInterrupt={machine.state === 'speaking' ? interrupt : machine.state === 'thinking' ? cancelReply : toggleMic}
      />
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${machine.state}`} aria-hidden="true" />
        {STATE_LABELS[machine.state]}
        {machine.state === 'listening' && <time>{`${Math.floor(listenSeconds / 60)}:${String(listenSeconds % 60).padStart(2, '0')}`}</time>}
        {machine.state === 'thinking' && (
          <span className="companion-status-sub">{activityStatus?.trim() || (assistantText.trim() ? '正在说…' : '马上开口…')}</span>
        )}
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
          {interimText && (machine.state === 'listening' || (settings.fullDuplex && (machine.state === 'thinking' || machine.state === 'speaking'))) && (
            <p className="companion-line interim">
              <span className="who" aria-hidden="true">我</span>
              <span className="interim-text">{interimText}</span>
              <span className="interim-cursor" aria-hidden="true">|</span>
            </p>
          )}
          {rounds.map((round, index) => (
            <SubtitleRow key={index} round={round} />
          ))}
          {!rounds.length && !interimText && <p className="companion-subtitle-hint">开启麦克风后，你说的话和月汐的回答都会在这里播报。</p>}
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
        <span key={index} className="sand">
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
