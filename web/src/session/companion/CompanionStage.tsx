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
// the subtitle strip shows only the current round until the next
// utterance starts. Focus returns to the entry element on unmount.
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
import { cleanForSpeech, cleanUserTranscript, compactSpeech, companionCannotExecuteSpeech, companionCaptionFromStream, companionExecutingSpeech, companionReplyStallMs, companionTaskCompleteSpeech, companionToolsExecuting, handsFreeRetryDelayMs, looksLikeOmniPersonaCaption, looksLikePlaybackEcho, prepareSpeech, shouldAcceptUserTranscript, shouldKeepHandsFreeLoop, stripTaskDonePhrases, takeSpeakableChunk } from './companionText'
import { localAsrStatus, LOCAL_ASR_DECISION_MS, readyWithin } from './localAsr'
import { startLocalCompanionSpeech } from './localSpeech'
import { MOON_RING_BINS, MoonSphere } from './MoonSphere'
import {
  ECHO_GUARD_MS,
  FORCE_COMMIT_MS,
  INTERRUPT_ECHO_MS,
  shouldShowSpeechSetupHint,
  startCompanionSpeech,
  type CompanionSpeechHandle,
  type CompanionSpeechOptions,
} from './speech'
import { getTtsAudioState, TtsPlayer, unlockTtsAudio } from './ttsPlayer'
import { prepareCompanionEntry } from './prepareCompanionEntry'
import { useWindowsDefaultMicrophone } from '../../settings/microphone'
import { useAutomationBroadcast } from './useAutomationBroadcast'
import { useCompanionMachine, companionSurfaceState, companionStatusLabel } from './useCompanionMachine'

const RECOGNIZER_DEAF_MS = 2500

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

const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)
const speakingGain = (value: number) => Math.max(0.18, Math.min(1, value))

/** Subtitle strip keeps only this round: the live user line plus 月汐's reply. */
function withCurrentAssistant(current: SubtitleRound[], assistant: SubtitleRound): SubtitleRound[] {
  const user = current.find(round => round.role === 'user')
  const last = current[current.length - 1]
  if (last?.role === 'assistant') {
    const merged = { ...last, ...assistant }
    return user ? [user, merged] : [merged]
  }
  return user ? [user, assistant] : [assistant]
}

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
  const [captionFading, setCaptionFading] = useState(false)
  const fadeTimersRef = useRef<{ fade: number; clear: number } | null>(null)
  const pendingCaptionFadeRef = useRef(false)
  const roundsRef = useRef<SubtitleRound[]>([])
  const [voiceHeard, setVoiceHeard] = useState(false)
  /** When the microphone last carried speech, whatever the recognizer did. */
  const voiceEnergyAtRef = useRef(0)
  const interimTextRef = useRef('')
  const [deafRecognizer, setDeafRecognizer] = useState(false)
  const deafRecoveriesRef = useRef(0)
  const [heardThisVisit, setHeardThisVisit] = useState(false)
  const [listenSeconds, setListenSeconds] = useState(0)
  const [engineHint, setEngineHint] = useState('')
  const [hintVisible, setHintVisible] = useState(false)
  const [localError, setLocalError] = useState<BridgeClientError>()
  const [audioLocked, setAudioLocked] = useState(() => getTtsAudioState() !== 'running')
  const rootRef = useRef<HTMLDivElement>(null)
  const subtitleListRef = useRef<HTMLDivElement>(null)
  const entryFocusRef = useRef<Element | null>(null)
  const speechHandleRef = useRef<CompanionSpeechHandle | undefined>(undefined)
  /** Probed once: whether the local model is installed and the sidecar starts. */
  const localAsrReadyRef = useRef(false)
  /** The probe itself, so a turn starting before it answers can wait. */
  const localAsrProbeRef = useRef<Promise<boolean> | undefined>(undefined)
  const activeRecognizerRef = useRef<'cloud' | 'local'>('cloud')
  const playerRef = useRef<TtsPlayer | undefined>(undefined)
  const streamCaptionRef = useRef('')
  const [assistantAloud, setAssistantAloud] = useState(false)
  const [playerSounding, setPlayerSounding] = useState(false)
  const startListeningRef = useRef<(auto: boolean) => void>(() => {})
  const openingListenRef = useRef(false)
  const captionHandleRef = useRef<CompanionSpeechHandle | undefined>(undefined)
  const handledReplyRef = useRef(chatStatus === 'done')
  const stateRef = useRef(machine.state)
  stateRef.current = machine.state
  roundsRef.current = rounds
  const cancelCaptionFade = () => {
    if (fadeTimersRef.current) {
      window.clearTimeout(fadeTimersRef.current.fade)
      window.clearTimeout(fadeTimersRef.current.clear)
      fadeTimersRef.current = null
    }
    setCaptionFading(false)
  }
  const chatStatusRef = useRef(chatStatus)
  chatStatusRef.current = chatStatus
  const activityStatusRef = useRef(activityStatus)
  activityStatusRef.current = activityStatus
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
  /** Forwards syncSpeechModes to effects declared above it. */
  const syncSpeechModesRef = useRef<() => void>(() => {})
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

  const assistantTurnBusy = useCallback(() => {
    const state = stateRef.current
    if (userInterruptedRef.current && (state === 'listening' || state === 'idle')) {
      return speakingRef.current
    }
    return (
      state === 'thinking' ||
      state === 'speaking' ||
      speakingRef.current ||
      playerRef.current?.isBusy() === true ||
      companionToolsExecuting(chatStatusRef.current, activityStatusRef.current)
    )
  }, [])

  const transcriptAcceptance = useCallback(
    (text: string) => {
      const lastAssistant = [...roundsRef.current].reverse().find(round => round.role === 'assistant')?.text ?? ''
      return shouldAcceptUserTranscript({
        state: stateRef.current,
        text,
        lastSpoken: lastSpokenRef.current,
        lastAssistant,
        assistantBusy: assistantTurnBusy(),
      })
    },
    [assistantTurnBusy],
  )

  const discardEchoCaption = useCallback((text: string) => {
    setInterimText('')
    const cleaned = cleanUserTranscript(text)
    if (!cleaned) return
    setRounds(current => {
      const user = current.find(round => round.role === 'user')
      if (!user || cleanUserTranscript(user.text) !== cleaned) return current
      return current.filter(round => round.role !== 'user')
    })
  }, [])

  const markAssistantAloud = useCallback((aloud: boolean) => {
    speakingRef.current = aloud
    setAssistantAloud(aloud)
    if (!aloud) {
      syncSpeechModesRef.current()
      return
    }
    if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
    if (stateRef.current === 'listening') machine.dispatch({ type: 'RECOGNIZED_FINAL' })
    if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
    syncSpeechModesRef.current()
  }, [machine])

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
    useWindowsDefaultMicrophone()
    setSettings(stored)
    settingsRef.current = stored
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
      syncSpeechModesRef.current()
      speechHandleRef.current?.resumeCapture()
    }
    window.addEventListener('pointerdown', unlock)
    window.addEventListener('keydown', unlock)
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
            engine: stored.engine === 'sapi' || stored.engine === 'natural' ? 'edge' : stored.engine,
          }),
        ).catch(() => {})
      }
    }, 0)
    return () => {
      cancelled = true
      window.removeEventListener('pointerdown', unlock)
      window.removeEventListener('keydown', unlock)
      window.clearTimeout(deferTtsWarmup)
      window.clearInterval(audioPoll)
      exitedRef.current = true
      autoLoopRef.current = false
      document.body.style.overflow = previousOverflow
      document.documentElement.classList.remove('companion-active')
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      captionHandleRef.current?.stop()
      captionHandleRef.current = undefined
      openingListenRef.current = false
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
    if (text.trim()) {
      lastDeltaAtRef.current = performance.now()
      void unlockTtsAudio()
    }
    if (text.trim() && stateRef.current === 'thinking') {
      machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
    }
    const shown = companionCaptionFromStream(text)
    if (!shown.trim()) return
    streamCaptionRef.current = shown
    setRounds(current => {
      const last = current[current.length - 1]
      if (last?.role === 'assistant' && last.segments === undefined && last.text === shown) return current
      if (last?.role === 'assistant' && last.segments === undefined) {
        return withCurrentAssistant(current, { ...last, text: shown })
      }
      return withCurrentAssistant(current, { role: 'assistant', text: shown })
    })
  }, [assistantText, chatStatus, machine.dispatch])

  // Do not clear userInterruptedRef when the stream ends. A leftover
  // done/failed event after 打断 used to restart TTS and trap 说话中.

  // Ensure the final reply lands in the subtitle strip even if the stream
  // flips to "done" between React renders.
  useEffect(() => {
    if (chatStatus !== 'done') return
    const shown = companionCaptionFromStream(assistantText)
    if (!shown) return
    streamCaptionRef.current = shown
    setRounds(current => {
      const last = current[current.length - 1]
      if (last?.role === 'assistant' && last.text === shown) return current
      if (last?.role === 'assistant' && last.segments === undefined) {
        return withCurrentAssistant(current, { ...last, text: shown })
      }
      return withCurrentAssistant(current, { role: 'assistant', text: shown })
    })
  }, [chatStatus, assistantText])

  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (assistantText.trim()) void unlockTtsAudio()
  }, [assistantText, chatStatus])

  // Head start: a sentence goes to the engine the moment it is complete,
  // instead of waiting for the whole turn to finish generating. Later
  // sentences do not become separate clips — TtsPlayer holds them in its
  // tail and joins them onto the still-running timeline, so a reply is
  // one continuous reading that simply begins ~1s after the first token.
  // Unpunctuated leftovers keep the old 800ms stall flush.
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (userInterruptedRef.current) return
    if (stateRef.current !== 'speaking' && stateRef.current !== 'thinking') return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const flush = () => {
      const streamText = companionCaptionFromStream(assistantText)
      const pending = streamText.slice(spokenUpToRef.current)
      if (!pending.trim()) return
      const stalled = performance.now() - lastDeltaAtRef.current >= 800
      const chunk = takeSpeakableChunk(pending, spokenUpToRef.current === 0, stalled)
      if (!chunk) return
      spokenUpToRef.current += chunk.consumed
      const cleaned = stripTaskDonePhrases(cleanForSpeech(chunk.text))
      if (!cleaned) return
      if (looksLikePlaybackEcho(cleaned, lastSpokenRef.current) && compactSpeech(cleaned).length <= compactSpeech(lastSpokenRef.current).length + 4) {
        return
      }
      lastSpokenRef.current = `${lastSpokenRef.current}${cleaned}`.slice(-1200)
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      // Close the microphone now, before the first sample reaches the
      // speaker. The state machine will not say 'speaking' until the model
      // has finished writing, which is far too late on speakerphone.
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
      player.enqueue([cleaned], { ...settings, voiceId }, {
        onEngineFallback: handleEngineFallback,
        onGain: value => setGain(speakingGain(value)),
        onFinished: reason => {
          setGain(0)
          echoUntilRef.current = performance.now() + ECHO_GUARD_MS
          setStreamTick(t => t + 1)
          const busy = playerRef.current?.isBusy() === true
          if (reason !== 'completed' || !busy) {
            speakingRef.current = false
            setAssistantAloud(false)
            syncSpeechModesRef.current()
          }
          if (reason === 'interrupted' || reason === 'circuit-broken' || reason === 'engine-unavailable') {
            if (reason === 'engine-unavailable') {
              setTtsAvailable(false)
              setDegraded(true)
            }
            if (reason === 'circuit-broken') setCircuitBroken(true)
            if (stateRef.current === 'speaking' && !userInterruptedRef.current) {
              machine.dispatch({ type: 'PLAYBACK_ENDED' })
            }
            return
          }
          if (reason !== 'completed' || busy) return
          if (stateRef.current !== 'speaking') return
          if (shouldAwaitMoreReply()) machine.dispatch({ type: 'AWAIT_MORE' })
          else if (chatStatusRef.current !== 'streaming') {
            speakingRef.current = false
            setAssistantAloud(false)
            machine.dispatch({ type: 'PLAYBACK_ENDED' })
          }
        },
      })
    }
    flush()
    const timer = window.setInterval(flush, 120)
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
      const reply = stripTaskDonePhrases(assistantText.trim())
      const completionLine = reply || stripTaskDonePhrases(activityStatus?.trim() || '')
      if (!completionLine.trim()) {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
        return
      }
      if (!reply) {
        const spoken = companionTaskCompleteSpeech(completionLine)
        setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: spoken }))
        if (speakable) {
          if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
          speak(spoken)
          return
        }
        machine.dispatch({ type: 'REPLY_TERMINAL' })
        return
      }
      // P0-1: Enqueue any remaining text that wasn't picked up during streaming,
      // then flush the queue and transition the machine.
      const remaining = companionCaptionFromStream(assistantText).slice(spokenUpToRef.current)
      const segments = prepareSpeech(remaining).filter(seg => {
        const cleaned = stripTaskDonePhrases(seg)
        if (!cleaned.trim()) return false
        if (looksLikePlaybackEcho(cleaned, lastSpokenRef.current) && compactSpeech(cleaned).length <= compactSpeech(lastSpokenRef.current).length + 4) {
          return false
        }
        return true
      }).map(seg => stripTaskDonePhrases(seg))
      if (segments.length && speakable) {
        if (stateRef.current === 'thinking') {
          machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
        }
        const player = ensurePlayer()
        const voiceId = activeVoiceId()
        player.configure(voiceId, settings.rate, settings.volume, settings)
        const spoken = segments.join('')
        lastSpokenRef.current = `${lastSpokenRef.current}${spoken}`.slice(-1200)
        const callbacks = {
          onEngineFallback: handleEngineFallback,
          onSegmentStart: (index: number) => {
            setRounds(current => {
              const last = current[current.length - 1]
              if (last?.role !== 'assistant') return current
              return [...current.slice(0, -1), { ...last, activeIndex: index }]
            })
          },
          onGain: (value: number) => setGain(speakingGain(value)),
          onSegmentFailed: (index: number, consecutive: number) => {
            if (consecutive >= 3) setCircuitBroken(true)
          },
        }
        // A turn that never streamed (fast provider, tool-only reply) would
        // otherwise synthesize as one long clip. Lead with the first
        // sentence so sound starts while the rest is still rendering; the
        // player joins them onto one timeline.
        const head = spokenUpToRef.current === 0 ? takeSpeakableChunk(spoken, true, false) : null
        const opening = head && head.consumed < spoken.length ? head.text : ''
        speakingRef.current = true
        setAssistantAloud(true)
        syncSpeechModesRef.current()
        if (opening) player.enqueue([opening], { ...settings, voiceId }, callbacks)
        player.enqueue([opening ? spoken.slice(opening.length) : spoken], { ...settings, voiceId }, callbacks)
        void player.flush({
          onFinished: () => {
            setGain(0)
            echoUntilRef.current = performance.now() + ECHO_GUARD_MS
            speakingRef.current = false
            setAssistantAloud(false)
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
            setAssistantAloud(false)
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
      // Read, not heard. A failure is already on screen as a banner and a
      // caption; saying it out loud spends a second and a half of the user's
      // time telling them something they can see, in a voice meant for
      // conversation.
      if (!handledReplyRef.current && chatStatus === 'failed') {
        handledReplyRef.current = true
        const spoken = companionCannotExecuteSpeech((error ?? localError)?.message || assistantText.trim())
        setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: spoken }))
        if (ttsAvailable !== false && settings.autoSpeak && !userInterruptedRef.current) {
          if (stateRef.current === 'thinking') machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
          speak(spoken)
          return
        }
      }
      if (stateRef.current === 'thinking') {
        machine.dispatch({ type: 'REPLY_TERMINAL' })
      } else if (stateRef.current === 'speaking') {
        playerRef.current?.interrupt()
        speakingRef.current = false
        setAssistantAloud(false)
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

  // Tool-status: speak 正在执行 once, show the live caption, and drop
  // 说话中 so a desktop task is never silent idle-chat.
  useEffect(() => {
    if (!companionToolsExecuting(chatStatus, activityStatus)) return
    if (userInterruptedRef.current) return
    const line = activityStatus?.trim()
    if (!line) return
    // Once tokens land, the stream owns the subtitle — a tool-status prefix
    // used to poison accumulateSpeakableCaption and replay the whole reply.
    if (assistantText.trim()) return
    setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: line }))
    const spoken = companionExecutingSpeech()
    if (spoken === lastActivitySpokenRef.current) return
    lastActivitySpokenRef.current = spoken
    if (stateRef.current === 'speaking') machine.dispatch({ type: 'AWAIT_MORE' })
    playerRef.current?.interrupt()
    speakingRef.current = false
    setAssistantAloud(false)
    if (ttsAvailable === false || !settings.autoSpeak) {
      syncSpeechModesRef.current()
      return
    }
    const player = ensurePlayer()
    const voiceId = activeVoiceId()
    player.configure(voiceId, settings.rate, settings.volume, settings)
    speakingRef.current = true
    setAssistantAloud(true)
    syncSpeechModesRef.current()
    player.enqueue([spoken], { ...settings, voiceId }, {
      onEngineFallback: handleEngineFallback,
      onGain: value => setGain(speakingGain(value)),
      onFinished: () => {
        setGain(0)
        speakingRef.current = false
        setAssistantAloud(false)
        syncSpeechModesRef.current()
      },
    })
  }, [activityStatus, assistantText, chatStatus, ttsAvailable, settings.autoSpeak, settings.rate, settings.volume, handleEngineFallback])

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
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
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
          setAssistantAloud(false)
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
      setRounds(current => withCurrentAssistant(current, { role: 'assistant', text, segments, activeIndex: undefined }))
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
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
          setAssistantAloud(false)
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
    setAssistantAloud(false)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    if (stateRef.current === 'speaking' || stateRef.current === 'thinking') {
      machine.dispatch({ type: 'INTERRUPT' })
    }
    // INTERRUPT is batched; keep syncSpeechModes from remuting the mic
    // while stateRef still says speaking.
    stateRef.current = 'idle'
    captionHandleRef.current?.resumeCapture()
    speechHandleRef.current?.resumeCapture()
    syncSpeechModesRef.current()
    setCircuitBroken(false)
    setStreamTick(0)
  }, [machine])

  const cancelReply = useCallback(() => {
    onCancel?.()
    spokenUpToRef.current = 0
    playerRef.current?.interrupt()
    setGain(0)
    speakingRef.current = false
    setAssistantAloud(false)
    setStreamTick(0)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    if (stateRef.current === 'thinking' || stateRef.current === 'speaking') {
      machine.dispatch({ type: 'INTERRUPT' })
    }
    stateRef.current = 'idle'
    captionHandleRef.current?.resumeCapture()
    speechHandleRef.current?.resumeCapture()
    syncSpeechModesRef.current()
  }, [machine, onCancel])

  const syncSpeechModes = useCallback(() => {
    const handle = speechHandleRef.current
    if (!handle) return
    const state = stateRef.current
    // Her turn is hers, start to end.
    //
    // There are exactly two ways back to the user's turn: the 打断 button
    // (or its hotkey), and her finishing. The microphone is not one of them,
    // so it stays shut for the whole of her turn — while she is thinking,
    // while she is talking, and across the gaps in between where the engine
    // has run dry and the model is still writing.
    //
    // Narrower versions of this rule kept failing on speakerphone, and each
    // time for the same reason: whatever window was left open was a window
    // her own voice could come back through, and a transcript cannot be
    // told apart from the user's by any test worth trusting. A closed
    // microphone needs no such test.
    // 'listening' and 'idle' are the machine saying her turn is over, and it
    // outranks the flag.
    //
    // speakingRef is set on every path that hands audio to the engine and
    // cleared on every path that finishes one, which is more places than stay
    // in agreement — and a single missed reset used to be permanent. It kept
    // assistantBusy true, which shut the microphone, stopped the recognizer,
    // and disabled both of the repairs that would have noticed, since each of
    // them declines to act during her turn. The stage said 聆听中 and nothing
    // underneath it was listening.
    if ((state === 'listening' || state === 'idle') && playerRef.current?.isBusy() !== true) {
      speakingRef.current = false
    }
    const speakingAloud = state === 'speaking' || speakingRef.current || playerRef.current?.isBusy() === true
    const assistantBusy = state === 'thinking' || speakingAloud
    const next = {
      commitPaused: assistantBusy,
      playback: assistantBusy,
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
  }, [])
  syncSpeechModesRef.current = syncSpeechModes

  useEffect(() => {
    const tick = () => {
      const busy = playerRef.current?.isBusy() === true
      setPlayerSounding(current => (current === busy ? current : busy))
      if (busy) speakingRef.current = true
    }
    tick()
    const timer = window.setInterval(tick, 80)
    return () => window.clearInterval(timer)
  }, [])

  const releaseSpeakingTurn = useCallback(() => {
    setGain(0)
    speakingRef.current = false
    setAssistantAloud(false)
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
    }
  }, [error, localError, machine, syncSpeechModes])

  const beginUserTurn = useCallback(
    (transcript: string) => {
      const text = cleanUserTranscript(transcript)
      if (!transcriptAcceptance(text)) {
        discardEchoCaption(transcript)
        return
      }
      cancelCaptionFade()
      userInterruptedRef.current = false
      silentRestartsRef.current = 0
      spokenUpToRef.current = 0
      speakingRef.current = false
      setAssistantAloud(false)
      streamCaptionRef.current = ''
      lastActivitySpokenRef.current = ''
      setStreamTick(0)
      setInterimText('')
      setLocalError(undefined)
      playerRef.current?.interrupt()
      setRounds([{ role: 'user', text }])
      if (stateRef.current === 'idle') {
        machine.dispatch({ type: 'MIC_ACTIVATE' })
      }
      if (!chatReadyRef.current) {
        pendingSendRef.current = text
        syncSpeechModes()
        return
      }
      machine.dispatch({ type: 'RECOGNIZED_FINAL' })
      void unlockTtsAudio()
      onSendRef.current(text)
      syncSpeechModes()
    },
    [machine, syncSpeechModes, transcriptAcceptance, discardEchoCaption],
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
      machine.dispatch({ type: 'MIC_ACTIVATE' })
      machine.dispatch({ type: 'RECOGNIZED_FINAL' })
      machine.dispatch({ type: 'REPLY_COMPLETED', speakable: true })
      speak(text)
    },
  })

  // Asked once per mount, not per turn: the answer only changes when the user
  // installs the model, and that happens in settings, behind a remount.
  //
  // Kept as the promise, not just its result. The stage starts listening the
  // moment it mounts, which is before a bridge round trip can answer — so a
  // caller reading the result alone always read the initial false and chose
  // the system recognizer. Under 'auto' that made the local model
  // unreachable by construction, however plainly the settings screen said it
  // was installed, and the choice was never revisited afterwards.
  useEffect(() => {
    const probe = localAsrStatus().then(status => status?.supported === true && status.ready === true)
    localAsrProbeRef.current = probe
    let alive = true
    void probe.then(ready => {
      if (alive) localAsrReadyRef.current = ready
    })
    return () => {
      alive = false
    }
  }, [])

  const startListening = useCallback((auto: boolean) => {
    if (exitedRef.current) return
    setLocalError(undefined)
    setEngineHint('')
    unlockTtsAudio()
    if (speechHandleRef.current) {
      syncSpeechModes()
      speechHandleRef.current.resumeCapture()
      if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
      autoLoopRef.current = true
      setHintVisible(false)
      return
    }
    if (openingListenRef.current) return
    openingListenRef.current = true
    setInterimText('')
    const speechOptions: CompanionSpeechOptions = {
      duplex: true,
      environment: settingsRef.current.speechEnvironment,
      onInterim: transcript => {
        const next = cleanUserTranscript(transcript)
        if (!transcriptAcceptance(next)) {
          discardEchoCaption(transcript)
          return
        }
        cancelCaptionFade()
        setInterimText(next)
        setVoiceHeard(true)
        setHeardThisVisit(true)
        setEngineHint('')
        if (stateRef.current === 'listening' || stateRef.current === 'idle') {
          setRounds([{ role: 'user', text: next }])
        }
      },
      onSpeechStart: () => {
        setVoiceHeard(true)
        setHeardThisVisit(true)
      },
      onVoiceEnergy: () => {
        // Hearing the user is the microphone's business, not the
        // recognizer's.
        //
        // This used to do nothing, so 「正在听…」 waited on a transcript.
        // That ties the one piece of feedback saying "your voice is
        // arriving" to the one thing most likely to be broken, and when the
        // recognizer returned nothing the stage looked deaf even though
        // audio was coming in the whole time. The two are separate
        // questions and the user can only debug them if we answer them
        // separately.
        setVoiceHeard(true)
        voiceEnergyAtRef.current = performance.now()
      },
      onEngineHint: message => setEngineHint(message),
      onFinal: transcript => {
        setHeardThisVisit(true)
        beginUserTurn(transcript)
      },
      spokenText: () => lastSpokenRef.current,
      onError: issue => {
        speechHandleRef.current = undefined
        // A local recognizer that dies mid-session under 'auto' is a fallback,
        // not something to make the user read and act on. An explicit choice
        // is never overridden: quietly switching to the system recognizer
        // would ship audio off the machine for someone who asked it not to be.
        if (activeRecognizerRef.current === 'local' && settingsRef.current.recognizer === 'auto') {
          localAsrReadyRef.current = false
          activeRecognizerRef.current = 'cloud'
          void startCompanionSpeech(speechOptions).then(adoptHandle).catch(abandon)
          return
        }
        if (!shouldKeepHandsFreeLoop({ exited: exitedRef.current, userPausedMic: false, errorCode: issue.code })) {
          autoLoopRef.current = false
          setLocalError(issue)
          if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
          return
        }
        autoLoopRef.current = true
        setLocalError(issue)
        if (stateRef.current === 'listening') machine.dispatch({ type: 'MIC_CANCEL' })
      },
      onLevels: next => {
        setLevels(next)
      },
      onEndWithoutFinal: () => {
        if (exitedRef.current) return
        autoLoopRef.current = true
        speechHandleRef.current?.resumeCapture()
        if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
      },
    }

    const adoptHandle = (handle: CompanionSpeechHandle) => {
      openingListenRef.current = false
      if (stateRef.current === 'idle') machine.dispatch({ type: 'MIC_ACTIVATE' })
      speechHandleRef.current = handle
      syncSpeechModes()
      handle.resumeCapture()
      autoLoopRef.current = true
      setHintVisible(false)
      void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
    }

    const abandon = (issue: BridgeClientError) => {
      openingListenRef.current = false
      if (!shouldKeepHandsFreeLoop({ exited: exitedRef.current, userPausedMic: false, errorCode: issue.code })) {
        autoLoopRef.current = false
        setLocalError(issue)
        return
      }
      autoLoopRef.current = true
      if (auto) setHintVisible(true)
      else setLocalError(issue)
    }

    const begin = async () => {
      // 'auto' prefers local when it is there, but a hung voice.status
      // used to stall this forever — the moon said 聆听中 and ASR never
      // opened. Bound the wait so the system recognizer starts talking.
      const installed =
        settingsRef.current.recognizer === 'auto'
          ? await readyWithin(localAsrProbeRef.current, LOCAL_ASR_DECISION_MS)
          : false
      if (exitedRef.current) {
        openingListenRef.current = false
        return
      }
      const preferLocal =
        settingsRef.current.recognizer === 'local' ||
        (settingsRef.current.recognizer === 'auto' && installed)
      activeRecognizerRef.current = preferLocal ? 'local' : 'cloud'
      const open = preferLocal ? startLocalCompanionSpeech : startCompanionSpeech
      try {
        adoptHandle(await open(speechOptions))
      } catch (issue) {
        if (preferLocal && settingsRef.current.recognizer === 'auto') {
          localAsrReadyRef.current = false
          activeRecognizerRef.current = 'cloud'
          void startCompanionSpeech(speechOptions).then(adoptHandle).catch(abandon)
          return
        }
        abandon(issue as BridgeClientError)
      }
    }
    void begin()
  }, [beginUserTurn, machine, markAssistantAloud, syncSpeechModes, transcriptAcceptance, discardEchoCaption])

  useEffect(() => {
    if (machine.state === 'speaking') justSpokeRef.current = true
  }, [machine.state])

  // Hands-free loop: once armed, every return to idle automatically re-opens
  // the mic. After TTS, wait out speaker ring-out so the next listen is clean.
  useEffect(() => {
    if (machine.state !== 'idle' || !autoLoopRef.current || exitedRef.current) return
    const hasHandle = !!(speechHandleRef.current || captionHandleRef.current)
    const delay = hasHandle ? 40 : handsFreeRetryDelayMs(silentRestartsRef.current)
    justSpokeRef.current = false
    let nested = 0
    const tryListen = () => {
      if (stateRef.current !== 'idle' || !autoLoopRef.current || exitedRef.current) return
      if (playerRef.current?.isBusy()) {
        nested = window.setTimeout(tryListen, 80)
        return
      }
      if (speechHandleRef.current) {
        silentRestartsRef.current = 0
        syncSpeechModes()
        speechHandleRef.current.resumeCapture()
        machine.dispatch({ type: 'MIC_ACTIVATE' })
        return
      }
      if (captionHandleRef.current) {
        silentRestartsRef.current = 0
        captionHandleRef.current.resumeCapture()
        machine.dispatch({ type: 'MIC_ACTIVATE' })
        return
      }
      silentRestartsRef.current += 1
      startListening(true)
    }
    const timer = window.setTimeout(tryListen, delay)
    return () => {
      window.clearTimeout(timer)
      window.clearTimeout(nested)
    }
  }, [machine.state, startListening, syncSpeechModes])

  // Audio arriving with no transcript behind it is a specific, nameable
  // failure, and one the user has no way to tell apart from a dead
  // microphone unless it is said. Reported rather than repaired here: the
  // repairs live in the speech layer, and this is what tells the user their
  // voice is getting in while one of them runs.
  useEffect(() => {
    if (machine.state !== 'listening') {
      setDeafRecognizer(false)
      return
    }
    const timer = window.setInterval(() => {
      const heardRecently = performance.now() - voiceEnergyAtRef.current < RECOGNIZER_DEAF_MS
      const deaf = heardRecently && !interimTextRef.current.trim()
      setDeafRecognizer(deaf)
      if (!deaf) {
        deafRecoveriesRef.current = 0
        return
      }
      deafRecoveriesRef.current += 1
      if (deafRecoveriesRef.current % 5 === 0) {
        speechHandleRef.current?.pulseRecognition()
        captionHandleRef.current?.pulseRecognition()
      }
      if (deafRecoveriesRef.current < 10) return
      deafRecoveriesRef.current = 0
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      captionHandleRef.current?.stop()
      captionHandleRef.current = undefined
      openingListenRef.current = false
      startListeningRef.current(true)
    }, 500)
    return () => window.clearInterval(timer)
  }, [machine.state])

  useEffect(() => {
    if (machine.state !== 'listening') {
      setVoiceHeard(false)
      return
    }
    syncSpeechModes()
    speechHandleRef.current?.resumeCapture()
    captionHandleRef.current?.resumeCapture()
    const wake = window.setTimeout(() => {
      if (stateRef.current !== 'listening' || exitedRef.current) return
      syncSpeechModes()
      speechHandleRef.current?.resumeCapture()
      captionHandleRef.current?.resumeCapture()
    }, 40)
    return () => window.clearTimeout(wake)
  }, [machine.state, syncSpeechModes])

  // Auto-start the microphone as soon as the stage mounts. Listening does
  // not depend on chat/model readiness — only the LLM send is gated.
  startListeningRef.current = startListening

  useEffect(() => {
    if (machine.state !== 'listening' || !interimText.trim()) return
    let interval = 0
    const kick = () => {
      if (stateRef.current !== 'listening' || exitedRef.current) return
      if (!interimTextRef.current.trim()) return
      speechHandleRef.current?.forceCommit()
      captionHandleRef.current?.forceCommit()
    }
    const timer = window.setTimeout(() => {
      kick()
      interval = window.setInterval(kick, 400)
    }, FORCE_COMMIT_MS)
    return () => {
      window.clearTimeout(timer)
      window.clearInterval(interval)
    }
  }, [machine.state, interimText])

  useEffect(() => {
    if (machine.state !== 'listening' || interimText.trim()) return
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'listening' || exitedRef.current || interimText.trim()) return
      speechHandleRef.current?.pulseRecognition()
      captionHandleRef.current?.pulseRecognition()
    }, heardThisVisit ? 8000 : 2000)
    return () => window.clearTimeout(timer)
  }, [machine.state, interimText, heardThisVisit])

  useEffect(() => {
    if (machine.state === 'thinking') {
      pendingCaptionFadeRef.current = false
      cancelCaptionFade()
      return
    }
    if (machine.state === 'speaking') {
      pendingCaptionFadeRef.current = true
      cancelCaptionFade()
      return
    }
    if (!pendingCaptionFadeRef.current) return
    if (machine.state !== 'idle' && machine.state !== 'listening') return
    pendingCaptionFadeRef.current = false
    if (fadeTimersRef.current) return
    if (!roundsRef.current.some(round => round.role === 'assistant')) return
    const fade = window.setTimeout(() => setCaptionFading(true), 700)
    const clear = window.setTimeout(() => {
      fadeTimersRef.current = null
      setRounds([])
      setInterimText('')
      setCaptionFading(false)
    }, 1550)
    fadeTimersRef.current = { fade, clear }
  }, [machine.state])

  useEffect(() => () => {
    if (fadeTimersRef.current) {
      window.clearTimeout(fadeTimersRef.current.fade)
      window.clearTimeout(fadeTimersRef.current.clear)
    }
  }, [])

  useEffect(() => {
    if (autoStartTriedRef.current) return
    autoStartTriedRef.current = true
    const timer = window.setTimeout(() => {
      if (!exitedRef.current && stateRef.current === 'idle') startListeningRef.current(true)
    }, 0)
    void prepareCompanionEntry(settingsRef.current).then(prepared => {
      if (exitedRef.current) return
      setSettings(prepared.settings)
      settingsRef.current = prepared.settings
    })
    return () => window.clearTimeout(timer)
  }, [])

  const stopAssistantAndListen = useCallback(() => {
    const state = stateRef.current
    if (state !== 'thinking' && state !== 'speaking') return
    userInterruptedRef.current = true
    handledReplyRef.current = true
    cancelReply()
    autoLoopRef.current = true
    stateRef.current = 'idle'
    speakingRef.current = false
    setAssistantAloud(false)
    speechHandleRef.current?.resumeCapture()
    captionHandleRef.current?.resumeCapture()
    syncSpeechModes()
    machine.dispatch({ type: 'MIC_ACTIVATE' })
    if (!speechHandleRef.current && !captionHandleRef.current) {
      startListening(false)
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
    openingListenRef.current = false
    speechHandleRef.current?.stop()
    speechHandleRef.current = undefined
    captionHandleRef.current?.stop()
    captionHandleRef.current = undefined
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

  /** A caption is live only during the turn it belongs to. */
  const executing = companionToolsExecuting(chatStatus, activityStatus)
  const surfaceState = companionSurfaceState(machine.state, (assistantAloud || playerSounding) && !executing, executing)
  const liveCaption = surfaceState === 'listening' && !!interimText.trim()
  interimTextRef.current = interimText

  return (
    <div
      className="companion-stage"
      ref={rootRef}
      tabIndex={-1}
      data-state={surfaceState}
      data-started={autoLoopRef.current || machine.state !== 'idle'}
      role="dialog"
      aria-modal="true"
      aria-label="月伴对话舞台"
      onPointerDown={() => {
        void unlockTtsAudio()
        syncSpeechModes()
        speechHandleRef.current?.resumeCapture()
      }}
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
      {(localError ?? error) &&
        (localError ?? error)!.code !== 'CHAT_CONFIG_MISSING' && (
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
        state={surfaceState}
        gain={gain}
        levels={levels}
        interruptible={surfaceState !== 'listening'}
        onInterrupt={surfaceState === 'speaking' || surfaceState === 'thinking' || machine.state === 'speaking' || machine.state === 'thinking' ? interruptAssistant : toggleMic}
      />
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${surfaceState}`} aria-hidden="true" />
        {companionStatusLabel(surfaceState, executing)}
        {surfaceState === 'listening' && (
          <span className="companion-status-sub">
            {interimText.trim()
              ? '正在听你说…'
              : voiceHeard
                ? '正在听…'
                : heardThisVisit || rounds.some(round => round.role === 'user')
                  ? '我在听'
                  : '我在听，请说话'}
          </span>
        )}
        {surfaceState === 'thinking' && (
          <span className="companion-status-sub">
            {activityStatus?.trim() ||
              (executing
                ? '正在执行…'
                : assistantText.trim()
                  ? '边写边读…'
                  : chatReady
                    ? '正在回你…'
                    : '连接模型…')}
          </span>
        )}
        {surfaceState === 'speaking' && (
          <span className="companion-status-sub">正在回答…</span>
        )}
      </div>
      {hintVisible && machine.state === 'idle' && (
        <p className="companion-hint" aria-live="polite">
          轻点月亮或按空格，开始和月汐说话；她说话时点「打断」或按 {formatInterruptHotkey(settings.interruptHotkey)} 停止
        </p>
      )}
      {/* Current round only: previous Q&A clears as soon as the next
          utterance is heard so the strip always matches this turn. */}
      <div className={`companion-subtitles${captionFading ? ' retiring' : ''}`} aria-label="对话记录">
        <div className="companion-subtitle-list" aria-live="polite" role="log" ref={subtitleListRef}>
          {rounds.map((round, index) => (
            <SubtitleRow
              key={round.role}
              // Only while listening. A caption belongs to the turn being
              // spoken, and anything arriving outside one is late audio from
              // the turn before — overwriting the question already on screen
              // with it is what made the subtitles jump.
              round={round.role === 'user' && liveCaption ? { ...round, text: interimText } : round}
              live={round.role === 'user' && liveCaption}
            />
          ))}
          {liveCaption && !rounds.some(round => round.role === 'user') && (
            <SubtitleRow round={{ role: 'user', text: interimText }} live />
          )}
          {!rounds.length && !interimText && machine.state !== 'listening' && (
            <p className="companion-subtitle-hint">
              进入后即可说话；你说的话和月汐的回答都会在这里显示。
            </p>
          )}
          {machine.state === 'listening' && engineHint && (
            <p className="companion-subtitle-hint warn">{engineHint}</p>
          )}
          {deafRecognizer && (
            <p className="companion-subtitle-hint warn">
              {activeRecognizerRef.current === 'local'
                ? '听到你的声音了，本机识别还没有出字…'
                : '听到你的声音了，系统识别还没有出字…'}
            </p>
          )}
          {!deafRecognizer && shouldShowSpeechSetupHint({
            listening: machine.state === 'listening',
            hasInterim: !!interimText.trim(),
            listenSeconds,
            heardThisVisit,
            hasUserRound: rounds.some(round => round.role === 'user'),
          }) && (
            <p className="companion-subtitle-hint warn">
              还是没有出字。请检查 Windows「设置 → 时间和语言 → 语音」里在线语音识别是否已开启，并确认系统默认麦克风正确。
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
          disabled={surfaceState !== 'thinking' && surfaceState !== 'speaking'}
          onClick={interruptAssistant}
        >
          打断
          <span className="companion-interrupt-key">{formatInterruptHotkey(settings.interruptHotkey)}</span>
        </button>
      </div>
    </div>
  )
}

function SubtitleRow({ round, live }: { round: SubtitleRound; live?: boolean }): React.JSX.Element {
  if (round.role === 'user') {
    return (
      <p className={`companion-line user${live ? ' live' : ''}`}>
        <span className="who" aria-hidden="true">我</span>
        <span className="plain-text">{round.text}</span>
        {live ? <span className="interim-cursor" aria-hidden="true">|</span> : null}
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
