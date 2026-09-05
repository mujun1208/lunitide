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
import { BridgeClientError, getProviderBridge, getTtsBridge, type TtsVoice } from '../../bridge/client'
import type { CompanionEngine, CompanionSettings } from './companionSettings'
import {
  companionEngineProbeOrder,
  companionPlaybackSettings,
  defaultCompanionSettings,
  formatInterruptHotkey,
  companionVoiceBargeInEnabled,
  loadCompanionSettings,
  matchesInterruptHotkey,
  saveCompanionSettings,
  voiceIdForEngineSwitch,
} from './companionSettings'
import { alreadySpokenCloseout, cleanForSpeech, cleanUserTranscript, clipAssistantToSpoken, clipCompanionPrompt, compactSpeech, companionCannotExecuteSpeech, companionCaptionFromStream, companionExecutingSpeech, companionHasFreshAssistantText, companionPadSpeech, companionReplyStallMs, companionTaskCompleteSpeech, companionToolCloseoutSpeech, companionToolsExecuting, FIRST_SPEAK_STALL_MS, handsFreeRetryDelayMs, isCompanionLeadInOnly, looksLikeOmniPersonaCaption, looksLikePlaybackEcho, prepareSpeech, shouldAcceptUserTranscript, shouldKeepHandsFreeLoop, shouldQueueBusyUserTranscript, stripTaskDonePhrases, takeSpeakableChunk } from './companionText'
import { companionAsrPathLabel, companionListenFailover, companionListenKind, companionListenLightLabel, companionVolcDeafGiveUp, withDeadline, type AsrRoute } from './asrPath'
import { isCompanionInfraBusy } from './companionBusy'
import { localAsrStatus, LOCAL_ASR_DECISION_MS, readyWithin } from './localAsr'
import { startLocalCompanionSpeech } from './localSpeech'
import { startVolcCompanionSpeech } from './volc/volcSpeech'
import { VOLC_ASR_DECISION_MS } from './volc/volcAsr'
import { pickDefaultVoice } from '../../provider/modelKind'
import { UserAskWizard } from '../UserAskWizard'
import type { UserAskPack } from '../userAsk'
import { MOON_RING_BINS, MoonSphere } from './MoonSphere'
import { CompanionSkinSwitch } from './CompanionSkinSwitch'
import { ParticleMoonScene } from './particle/ParticleMoonScene'
import { consumeCompanionSkinCommand, type CompanionSkin } from './particle/particleMoon'
import { Aurora } from './visual/Aurora'
import { AURORA_STOPS, auroraForEnter } from './visual/moonVisual'
import { useCompanionEnter } from './visual/useCompanionEnter'
import { canUseCompanionWebgl } from './visual/webglSupport'
import {
  ECHO_GUARD_MS,
  FORCE_COMMIT_MS,
  INTERRUPT_ECHO_MS,
  shouldShowSpeechSetupHint,
  stageForceCommitMayBeginTurn,
  startCompanionSpeech,
  type CompanionSpeechHandle,
  type CompanionSpeechOptions,
} from './speech'
import { getTtsAudioState, playCompanionAckPcm, TtsPlayer, unlockTtsAudio } from './ttsPlayer'
import { currentVoiceTiming, finishVoiceTurn, markVoiceTiming, startVoiceTurn } from './voiceTiming'
import { prepareCompanionEntry, pendingCompanionLights } from './prepareCompanionEntry'
import { CompanionEntryLights } from './CompanionEntryLights'
import { companionTalkLiveLights, type CompanionEntryReport, type EntryLight } from './companionLights'
import {
  companionCascadeSpeechBlocked,
  isCompanionIdleChat,
  newTalkRetryState,
  noteTalkFailure,
  shouldOfferCompanionTalk,
  startCompanionTalk,
  TALK_FALLBACK_BANNER,
  TALK_FIRST_AUDIO_MS,
  TALK_MAX_FAILURES,
  talkRetryBlocked,
  type CompanionTalkHandle,
} from './companionTalk'
import { prewarmVoice } from './voicePrewarm'
import { useWindowsDefaultMicrophone } from '../../settings/microphone'
import { useAutomationBroadcast } from './useAutomationBroadcast'
import { useCompanionMachine, companionEventForDispatch, companionSurfaceState, companionStatusLabel, type CompanionEvent } from './useCompanionMachine'
import { ToolTrajectory, type ToolTrajectoryItem } from '../ToolTrajectory'
import { useZh } from '../../i18n/language'

const RECOGNIZER_DEAF_MS = 2500
/** Room echo after she finishes a clip. Separate from ECHO_GUARD_MS (300–600). */
const POST_SPEAK_ECHO_MS = 900

export interface CompanionStageProps {
  sessionId?: string
  chatStatus: 'idle' | 'streaming' | 'done' | 'failed' | 'cancelled'
  assistantText: string
  /** Short status while tools run before the first spoken token (e.g. "浏览文件中…"). */
  activityStatus?: string
  /** Append-only tool list for this turn (same source as SessionPage). */
  toolActivities?: readonly ToolTrajectoryItem[]
  error?: BridgeClientError
  chatReady: boolean
  /** First user turn when entering from home wake (“你好月汐，查天气”). */
  seedPrompt?: string
  userAsk?: UserAskPack
  onUserAsk?: (followUp: string) => void
  onSend: (text: string) => void | boolean | 'error' | Promise<void | boolean | 'error'>
  /** Cancel the in-flight LLM stream. Spoken prefix is what the stage already read aloud. */
  onCancel?: (spokenText?: string) => void
  onExit: () => void
  pendingApproval?: { callId: string; name: string; summary?: string }
  onApproveTool?: () => void
  onRejectTool?: () => void
  persistFailed?: boolean
  onRetryPersist?: () => void
  resumeAvailable?: boolean
  onResume?: () => void
  memorySummary?: string
  onOpenMemory?: () => void
  /** D9: settings switch is off — show on the stage, never enable it. */
  computerControlOff?: boolean
  /** Home/session LLM so the 想 light matches the model that will actually run. */
  thinkProviderId?: string
  thinkModelId?: string
  /** Last user + assistant from history. Current-turn highlight stays in the bar. */
  historySeed?: ReadonlyArray<{ role: string; text: string }>
  onEngaged?: () => void
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

export function CompanionStage({ sessionId, chatStatus, assistantText, activityStatus, toolActivities, error, chatReady, seedPrompt, userAsk, onUserAsk, onSend, onCancel, onExit, pendingApproval, onApproveTool, onRejectTool, persistFailed, onRetryPersist, resumeAvailable, onResume, memorySummary, onOpenMemory, computerControlOff, thinkProviderId, thinkModelId, onEngaged }: CompanionStageProps): React.JSX.Element {
  const zh = useZh()
  const enter = useCompanionEnter()
  const machine = useCompanionMachine()
  const [settings, setSettings] = useState<CompanionSettings>(defaultCompanionSettings())
  const [dustHigh, setDustHigh] = useState(true)
  const lowFps = useRef(0)
  const visualSkin: CompanionSkin = settings.visualSkin === 'particle' ? 'particle' : 'classic'
  const applyVisualSkin = (next: CompanionSkin) => {
    const current = loadCompanionSettings()
    saveCompanionSettings({ ...current, visualSkin: next })
    setSettings(loadCompanionSettings())
  }
  const [ttsAvailable, setTtsAvailable] = useState<boolean | undefined>(undefined)
  const [entryLights, setEntryLights] = useState<CompanionEntryReport['lights']>(pendingCompanionLights)
  const [entryBlock, setEntryBlock] = useState('')
  const allowListenRef = useRef(false)
  const entryBlockRef = useRef('')
  const sessionIdRef = useRef(sessionId)
  sessionIdRef.current = sessionId
  const hasTalkModelRef = useRef(false)
  const talkHandleRef = useRef<CompanionTalkHandle | undefined>(undefined)
  const talkPendingRef = useRef(false)
  const talkRetryRef = useRef(newTalkRetryState())
  const talkHandoffRef = useRef(false)
  const talkSuppressPlayRef = useRef(false)
  const preparedLightsRef = useRef<CompanionEntryReport['lights']>(pendingCompanionLights())
  const [talkLive, setTalkLive] = useState(false)
  const talkLiveRef = useRef(false)
  talkLiveRef.current = talkLive
  const onEngagedRef = useRef(onEngaged)
  onEngagedRef.current = onEngaged
  const cascadeSpeechBlocked = () => companionCascadeSpeechBlocked({
    // talkHandleRef is set synchronously in adoptTalk, before setTalkLive's
    // render commits talkLiveRef. Reading the handle here closes the window
    // where cascade seed-tts would speak the same reply the talk PCM is
    // already streaming (the "two mouths, one reply" doubling).
    talkLive: talkLiveRef.current || talkHandleRef.current != null,
    talkPending: talkPendingRef.current,
    talkSuppressPlay: talkSuppressPlayRef.current,
  })
  const [entryReady, setEntryReady] = useState(false)
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
  const volcDeafRestartsRef = useRef(0)
  const [heardThisVisit, setHeardThisVisit] = useState(false)
  const [listenSeconds, setListenSeconds] = useState(0)
  const [engineHint, setEngineHint] = useState('')
  const [asrRoute, setAsrRoute] = useState<AsrRoute | ''>('')
  const [handsFree, setHandsFree] = useState(false)
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
  const activeRecognizerRef = useRef<'cloud' | 'local' | 'volc'>('cloud')
  const listenOverrideRef = useRef<AsrRoute | undefined>(undefined)
  const playerRef = useRef<TtsPlayer | undefined>(undefined)
  const streamCaptionRef = useRef('')
  const [assistantAloud, setAssistantAloud] = useState(false)
  const [playerSounding, setPlayerSounding] = useState(false)
  const startListeningRef = useRef<(auto: boolean) => void>(() => {})
  const openingListenRef = useRef(false)
  const captionHandleRef = useRef<CompanionSpeechHandle | undefined>(undefined)
  const handledReplyRef = useRef(chatStatus === 'done')
  const stateRef = useRef(machine.state)
  const applyEvent = useCallback((event: CompanionEvent): boolean => {
    const mapped = companionEventForDispatch(stateRef.current, event)
    if (!mapped) return true
    const next = machine.dispatch(mapped)
    if (!next) return false
    stateRef.current = next
    return true
  }, [machine])
  useEffect(() => {
    stateRef.current = machine.state
  }, [machine.state])
  roundsRef.current = rounds
  useEffect(() => {
    setRounds([])
  }, [sessionId])
  useEffect(() => {
    if (settings.voicePath !== 'local') return
    const launching = entryLights.some(light => light.key === 'speak' && light.label.includes('启动中'))
    if (!launching) return
    const timer = window.setTimeout(() => {
      speakReadyRef.current = false
      setEngineHint('引擎未就绪，这轮先用晓晓读。')
      setEntryLights(current => current.map(light => (
        light.key === 'speak' ? { ...light, label: '晓晓（克隆未就绪）', state: 'warn' as const } : light
      )) as typeof current)
    }, 10_000)
    return () => window.clearTimeout(timer)
  }, [entryLights, settings.voicePath])
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
  const toolsRanThisTurnRef = useRef(false)
  const lastDeltaAtRef = useRef(0)
  const assistantTextRef = useRef(assistantText)
  assistantTextRef.current = assistantText
  const settingsRef = useRef(settings)
  settingsRef.current = settings
  const speakReadyRef = useRef(true)
  const localSlowRoundsRef = useRef(0)
  const localEdgeLaneRef = useRef(false)
  const playbackExtras = (voiceId = '') => {
    const extras = companionPlaybackSettings(settingsRef.current, speakReadyRef.current, localEdgeLaneRef.current)
    const id = extras.voicePath === 'local' && extras.engine === 'edge' ? '' : (voiceId || extras.voiceId)
    return { ...extras, voiceId: id }
  }

  const dropCompanionPad = () => {
    padActiveRef.current = false
    padGenRef.current += 1
  }

  const yieldCompanionPad = () => {
    if (!padActiveRef.current) return
    dropCompanionPad()
    playerRef.current?.interrupt({ cancelEngine: false })
  }

  const speakCompanionPad = () => {
    if (cascadeSpeechBlocked()) return
    const stored = settingsRef.current
    if (!stored.instantAck || !stored.autoSpeak) return
    if (ttsAvailableRef.current === false) return
    const line = companionPadSpeech()
    padGenRef.current += 1
    padActiveRef.current = true
    lastSpokenRef.current = `${lastSpokenRef.current}${line}`.slice(-1200)
    playCompanionAckPcm()
    padActiveRef.current = false
  }
  /** Hands-free loop armed after the first successful microphone activation. */
  const autoLoopRef = useRef(false)
  const setListenLoop = useCallback((armed: boolean) => {
    autoLoopRef.current = armed
    setHandsFree(armed)
  }, [])
  const armListenLoop = useCallback(() => {
    setListenLoop(settingsRef.current.fullDuplex)
  }, [setListenLoop])
  const autoStartTriedRef = useRef(false)
  const entryRetryTimerRef = useRef(0)
  const listenQueuedRef = useRef(false)
  const [recognizerLive, setRecognizerLive] = useState(false)
  const exitedRef = useRef(false)
  const stageAliveRef = useRef(true)
  const silentRestartsRef = useRef(0)
  /** Streaming TTS: track how much of assistantText has been spoken. */
  const spokenUpToRef = useRef(0)
  /** Streaming TTS: whether we are currently speaking (streaming or final). */
  const speakingRef = useRef(false)
  /** Forwards syncSpeechModes to effects declared above it. */
  const syncSpeechModesRef = useRef<() => void>(() => {})
  /** Last TTS text — used to drop speaker→mic echo as a fake user turn. */
  const lastSpokenRef = useRef('')
  const padGenRef = useRef(0)
  const padActiveRef = useRef(false)
  /** Assistant text at barge-in, so a leftover stream cannot re-speak it. */
  const staleReplyRef = useRef('')
  /** Ignore recognizer output until this time — covers TTS + speaker ring-out. */
  const echoUntilRef = useRef(0)
  const interruptEchoRef = useRef(false)
  /** Idle re-listen waits longer after playback so room echo dies. */
  const justSpokeRef = useRef(false)
  /** User clicked 打断 / hotkey — block streaming TTS until the next user turn. */
  const userInterruptedRef = useRef(false)
  /** After clip-to-spoken, ignore leftover stream captions until assistantText resets. */
  const holdSpokenCaptionRef = useRef(false)
  /** P0-1: Incremented when a streaming chunk finishes, forcing the streaming
   *  useEffect to re-evaluate even if assistantText hasn't changed. */
  const [streamTick, setStreamTick] = useState(0)
  const [connectingHint, setConnectingHint] = useState(false)
  const lastActivitySpokenRef = useRef('')
  const chatReadyRef = useRef(chatReady)
  chatReadyRef.current = chatReady
  const ttsAvailableRef = useRef(ttsAvailable)
  ttsAvailableRef.current = ttsAvailable
  const pendingSendRef = useRef<string | null>(null)
  const sentThisUtteranceRef = useRef('')
  const speechSyncRef = useRef<{ commitPaused: boolean; playback: boolean; echoGuardMs: number; listenThrough: boolean } | undefined>(undefined)
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
    (text: string, opts?: { commit?: boolean }) => {
      const lastAssistant = [...roundsRef.current].reverse().find(round => round.role === 'assistant')?.text ?? ''
      return shouldAcceptUserTranscript({
        state: stateRef.current,
        text,
        lastSpoken: lastSpokenRef.current,
        lastAssistant,
        assistantBusy: assistantTurnBusy(),
        echoGuardActive: opts?.commit === true && performance.now() < echoUntilRef.current,
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
    if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
    if (stateRef.current === 'listening') applyEvent({ type: 'RECOGNIZED_FINAL' })
    if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
    syncSpeechModesRef.current()
  }, [applyEvent])

  const handleEngineFallback = useCallback((engine: CompanionEngine) => {
    if (settingsRef.current.voicePath === 'local') {
      setEntryLights(lights => lights.map(light => (
        light.key === 'speak'
          ? { ...light, label: '晓晓（本机朗读未就绪）', state: 'warn' as const }
          : light
      )) as typeof lights)
      setEngineHint('本机朗读未就绪，这轮先用晓晓读给你听。')
      setTtsAvailable(true)
      setDegraded(false)
      setCircuitBroken(false)
      return
    }
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
      const probeEngines =
        stored.voicePath === 'local'
          ? ([stored.engine === 'ref' ? 'ref' : 'onnx'] as const)
          : companionEngineProbeOrder(stored.engine)
      void (async () => {
        for (const engine of [...new Set(probeEngines)]) {
          if (cancelled) return
          try {
            const result = await getTtsBridge().voices({ engine })
            if (cancelled) return
            if (result.voices.length) {
              setVoices(result.voices)
              setTtsAvailable(true)
              if (engine !== stored.engine && stored.voicePath !== 'local') setSettings(current => ({ ...current, engine }))
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
              text: '啊',
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
      stageAliveRef.current = false
      exitedRef.current = true
      window.clearInterval(audioPoll)
      window.clearTimeout(deferTtsWarmup)
      window.removeEventListener('pointerdown', unlock)
      window.removeEventListener('keydown', unlock)
      autoLoopRef.current = false
      document.body.style.overflow = previousOverflow
      document.documentElement.classList.remove('companion-active')
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      captionHandleRef.current?.stop()
      captionHandleRef.current = undefined
      // A live 火山 talk.* session outlives an unmount that skips exit()
      // (route change / conditional unmount / StrictMode remount): the mic
      // append loop and realtime socket would keep running and stream audio
      // into a disposed player. Stop it here so unmount == full teardown.
      const leakedTalk = talkHandleRef.current
      talkHandleRef.current = undefined
      talkPendingRef.current = false
      void leakedTalk?.stop()
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
    const onStorage = (event: StorageEvent) => {
      if (event.key === 'lunitide:companion') refresh()
    }
    window.addEventListener('lunitide:companion-settings', refresh)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener('lunitide:companion-settings', refresh)
      window.removeEventListener('storage', onStorage)
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
    if (holdSpokenCaptionRef.current) {
      if (!text.trim()) holdSpokenCaptionRef.current = false
      return
    }
    if (companionHasFreshAssistantText(text, staleReplyRef.current) && stateRef.current === 'thinking') {
      applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
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
    if (holdSpokenCaptionRef.current) return
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
  // Unpunctuated leftovers flush after FIRST_SPEAK_STALL_MS (350).
  useEffect(() => {
    if (chatStatus !== 'streaming') return
    if (userInterruptedRef.current) return
    if (stateRef.current !== 'speaking' && stateRef.current !== 'thinking') return
    if (ttsAvailable === false || !settings.autoSpeak) return
    const flush = () => {
      if (staleReplyRef.current && assistantText === staleReplyRef.current) return
      if (staleReplyRef.current) staleReplyRef.current = ''
      const streamText = companionCaptionFromStream(assistantText)
      const pending = streamText.slice(spokenUpToRef.current)
      if (!pending.trim()) return
      const stalled = performance.now() - lastDeltaAtRef.current >= FIRST_SPEAK_STALL_MS
      const chunk = takeSpeakableChunk(pending, spokenUpToRef.current === 0, stalled)
      if (!chunk) return
      spokenUpToRef.current += chunk.consumed
      markVoiceTiming('firstSynth')
      const cleaned = stripTaskDonePhrases(cleanForSpeech(chunk.text))
      if (!cleaned) return
      if (cascadeSpeechBlocked()) return
      if (/无法执行/.test(cleaned) && companionToolsExecuting(chatStatus, activityStatus)) return
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
      yieldCompanionPad()
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
      player.enqueue([cleaned], playbackExtras(voiceId), {
        onEngineFallback: handleEngineFallback,
        onGain: value => {
          markVoiceTiming('firstAudio')
          if (settingsRef.current.voicePath === 'local' && !localEdgeLaneRef.current) {
            const audioMs = currentVoiceTiming('firstAudio')
            if (audioMs != null && audioMs > 800) {
              localSlowRoundsRef.current += 1
              if (localSlowRoundsRef.current >= 3) {
                localEdgeLaneRef.current = true
                setEngineHint('本机听 · 晓晓读（克隆首包偏慢）')
              }
            } else if (audioMs != null && audioMs <= 800) {
              localSlowRoundsRef.current = 0
            }
          }
          setGain(speakingGain(value))
        },
        onFinished: reason => {
          setGain(0)
          echoUntilRef.current = performance.now() + POST_SPEAK_ECHO_MS
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
              setEngineHint('朗读断了，看字幕')
            }
            if (reason === 'circuit-broken') {
              setCircuitBroken(true)
              setEngineHint('朗读断了，看字幕')
            }
            if (stateRef.current === 'speaking' && !userInterruptedRef.current) {
              applyEvent({ type: 'PLAYBACK_ENDED' })
            }
            return
          }
          if (reason !== 'completed' || busy) return
          if (stateRef.current !== 'speaking') return
          if (shouldAwaitMoreReply()) applyEvent({ type: 'AWAIT_MORE' })
          else if (chatStatusRef.current !== 'streaming') {
            speakingRef.current = false
            setAssistantAloud(false)
            applyEvent({ type: 'PLAYBACK_ENDED' })
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
      if (staleReplyRef.current.trim() && !companionHasFreshAssistantText(assistantText, staleReplyRef.current)) {
        return
      }
      handledReplyRef.current = true
      if (userInterruptedRef.current) {
        applyEvent({ type: 'REPLY_TERMINAL' })
        return
      }
      const speakable = ttsAvailable !== false && settings.autoSpeak
      const reply = stripTaskDonePhrases(assistantText.trim())
      const leadInOnly = isCompanionLeadInOnly(assistantText.trim())
      const toolsRan = toolsRanThisTurnRef.current
      const activityLine = activityStatus?.trim() || ''
      const activityResult = /中[….…]+$/.test(activityLine) ? '' : stripTaskDonePhrases(activityLine)
      const completionLine = (!leadInOnly ? reply : '') || activityResult
      const speakCloseout = () => {
        const spoken = toolsRan ? companionToolCloseoutSpeech(completionLine) : companionTaskCompleteSpeech(completionLine)
        setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: spoken }))
        if (speakable) {
          if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
          const caption = companionCaptionFromStream(assistantText)
          if (alreadySpokenCloseout(spoken, caption, spokenUpToRef.current)) {
            const player = ensurePlayer()
            void player.flush({
              onFinished: () => {
                if (!stageAliveRef.current) return
                setGain(0)
                speakingRef.current = false
                setAssistantAloud(false)
                if (stateRef.current === 'speaking') applyEvent({ type: 'PLAYBACK_ENDED' })
                else if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
              },
            })
            return
          }
          speak(spoken)
          return
        }
        applyEvent({ type: 'REPLY_TERMINAL' })
      }
      if (leadInOnly && toolsRan) {
        speakCloseout()
        return
      }
      if (!completionLine.trim()) {
        if (toolsRan) {
          speakCloseout()
          return
        }
        applyEvent({ type: 'REPLY_TERMINAL' })
        return
      }
      if (!reply) {
        speakCloseout()
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
          applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
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
        yieldCompanionPad()
        speakingRef.current = true
        setAssistantAloud(true)
        syncSpeechModesRef.current()
        // Talk PCM already owns this player. Enqueueing cascade here is the
        // leftover stacked-voice path (same reply, two mouths).
        if (!cascadeSpeechBlocked()) {
          if (opening) player.enqueue([opening], playbackExtras(voiceId), callbacks)
          player.enqueue([opening ? spoken.slice(opening.length) : spoken], playbackExtras(voiceId), callbacks)
        } else if (stateRef.current === 'thinking') {
          applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
        }
        void player.flush({
          onFinished: () => {
            if (!stageAliveRef.current) return
            setGain(0)
            echoUntilRef.current = performance.now() + POST_SPEAK_ECHO_MS
            speakingRef.current = false
            setAssistantAloud(false)
            if (stateRef.current === 'speaking') applyEvent({ type: 'PLAYBACK_ENDED' })
            else if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
          },
        })
      } else if (speakable) {
        // All text was already consumed during streaming — wait for queue to finish
        const player = ensurePlayer()
        void player.flush({
          onFinished: () => {
            if (!stageAliveRef.current) return
            setGain(0)
            speakingRef.current = false
            setAssistantAloud(false)
            if (stateRef.current === 'speaking') applyEvent({ type: 'PLAYBACK_ENDED' })
            else if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
          },
        })
      } else if (!speakable) {
        applyEvent({ type: 'REPLY_TERMINAL' })
      }
      return
    }
    if (chatStatus === 'failed' || chatStatus === 'cancelled') {
      const issue = error ?? localError
      const infraBusy = isCompanionInfraBusy(issue?.code ?? '')
      // Admission/retry noise is not a task failure. Drop back to listening
      // instead of reading 「桌面主机正忙」 as if the model refused the turn.
      if (infraBusy && chatStatus === 'failed') {
        handledReplyRef.current = true
        const busyLine = '桌面正忙，我稍后再试。'
        setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: busyLine }))
        if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
        else if (stateRef.current === 'speaking') {
          playerRef.current?.interrupt()
          speakingRef.current = false
          setAssistantAloud(false)
          setGain(0)
          applyEvent({ type: 'INTERRUPT' })
        }
        if (ttsAvailable !== false && settings.autoSpeak && !userInterruptedRef.current) {
          speak(busyLine)
        }
        return
      }
      // Read, not heard. A failure is already on screen as a banner and a
      // caption; saying it out loud spends a second and a half of the user's
      // time telling them something they can see, in a voice meant for
      // conversation.
      if (!handledReplyRef.current && chatStatus === 'failed') {
        handledReplyRef.current = true
        const spoken = companionCannotExecuteSpeech(issue?.message || assistantText.trim())
        setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: spoken }))
        if (ttsAvailable !== false && settings.autoSpeak && !userInterruptedRef.current) {
          if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
          speak(spoken)
          return
        }
      }
      if (stateRef.current === 'thinking') {
        applyEvent({ type: 'REPLY_TERMINAL' })
      } else if (stateRef.current === 'speaking') {
        playerRef.current?.interrupt()
        speakingRef.current = false
        setAssistantAloud(false)
        setGain(0)
        applyEvent({ type: 'INTERRUPT' })
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
    if (userAsk) return
    if (companionToolsExecuting(chatStatus, activityStatus)) return
    const waitingForFirstToken = !companionHasFreshAssistantText(assistantText, staleReplyRef.current)
    const ms = companionReplyStallMs(chatStatus === 'streaming', !waitingForFirstToken)
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'thinking') return
      if (companionHasFreshAssistantText(assistantTextRef.current, staleReplyRef.current)) return
      if (companionToolsExecuting(chatStatusRef.current, activityStatusRef.current)) return
      onCancel?.()
      setLocalError(new BridgeClientError('月汐没有及时回应，请再说一次', 'COMPANION_REPLY_STALL', true, 'renderer'))
      applyEvent({ type: 'REPLY_TERMINAL' })
    }, ms)
    return () => window.clearTimeout(timer)
  }, [machine.state, chatStatus, assistantText, activityStatus, userAsk, applyEvent, onCancel])

  // TTS drained but the stream is still open: leave “说话中” so the mic
  // hears the next utterance instead of looking frozen.
  useEffect(() => {
    if (machine.state !== 'speaking' || chatStatus !== 'streaming') return
    const timer = window.setInterval(() => {
      if (stateRef.current !== 'speaking' || chatStatusRef.current !== 'streaming') return
      if (playerRef.current?.isBusy()) return
      if (!assistantTextRef.current.trim()) return
      applyEvent({ type: 'AWAIT_MORE' })
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
    toolsRanThisTurnRef.current = true
    if (userInterruptedRef.current) return
    const line = activityStatus?.trim()
    if (!line) return
    // A host lead-in is not the result. Keep 执行中 audible on top of it.
    if (assistantText.trim() && !isCompanionLeadInOnly(assistantText)) return
    setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: line }))
    const spoken = companionExecutingSpeech(line)
    if (!spoken) return
    if (cascadeSpeechBlocked()) return
    if (spoken === lastActivitySpokenRef.current) return
    lastActivitySpokenRef.current = spoken
    if (stateRef.current === 'speaking') applyEvent({ type: 'AWAIT_MORE' })
    dropCompanionPad()
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
    player.enqueue([spoken], playbackExtras(voiceId), {
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

  useEffect(() => {
    if (!companionToolsExecuting(chatStatus, activityStatus)) return
    if (userInterruptedRef.current) return
    const timer = window.setTimeout(() => {
      if (userInterruptedRef.current) return
      if (!companionToolsExecuting(chatStatusRef.current, activityStatusRef.current)) return
      const spoken = companionExecutingSpeech(activityStatusRef.current)
      if (!spoken || cascadeSpeechBlocked()) return
      lastActivitySpokenRef.current = spoken
      if (ttsAvailable === false || !settings.autoSpeak) return
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
      player.enqueue([spoken], playbackExtras(voiceId), {
        onEngineFallback: handleEngineFallback,
        onGain: value => setGain(speakingGain(value)),
        onFinished: () => {
          setGain(0)
          speakingRef.current = false
          setAssistantAloud(false)
          syncSpeechModesRef.current()
        },
      })
    }, 20_000)
    return () => window.clearTimeout(timer)
  }, [activityStatus, chatStatus, ttsAvailable, settings.autoSpeak, settings.rate, settings.volume, handleEngineFallback, ensurePlayer, activeVoiceId])

  const speakChunk = useCallback(
    (text: string) => {
      if (cascadeSpeechBlocked()) return
      const segments = prepareSpeech(text)
      if (!segments.length) {
        speakingRef.current = false
        return
      }
      lastSpokenRef.current = `${lastSpokenRef.current}${segments.join('')}`.slice(-1200)
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      yieldCompanionPad()
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
      setCircuitBroken(false)
      void player.speak(segments, playbackExtras(voiceId), {
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
            setEngineHint('朗读断了，看字幕')
            applyEvent({ type: 'PLAYBACK_ENDED' })
          } else if (reason === 'circuit-broken') {
            setCircuitBroken(true)
            setEngineHint('朗读断了，看字幕')
            applyEvent({ type: 'PLAYBACK_ENDED' })
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
      if (cascadeSpeechBlocked()) return
      const segments = prepareSpeech(text)
      lastSpokenRef.current = `${lastSpokenRef.current}${segments.join('')}`.slice(-1200)
      setRounds(current => withCurrentAssistant(current, { role: 'assistant', text, segments, activeIndex: undefined }))
      const player = ensurePlayer()
      const voiceId = activeVoiceId()
      player.configure(voiceId, settings.rate, settings.volume, settings)
      yieldCompanionPad()
      speakingRef.current = true
      setAssistantAloud(true)
      syncSpeechModesRef.current()
      setCircuitBroken(false)
      void player.speak(segments, playbackExtras(voiceId), {
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
            setEngineHint('朗读断了，看字幕')
            applyEvent({ type: 'PLAYBACK_ENDED' })
          } else if (reason === 'circuit-broken') {
            setCircuitBroken(true)
            setEngineHint('朗读断了，看字幕')
            applyEvent({ type: 'PLAYBACK_ENDED' })
          } else if (reason === 'completed') {
            applyEvent({ type: 'PLAYBACK_ENDED' })
          }
          // 'interrupted' is dispatched synchronously by interrupt().
        },
      })
    },
    [activeVoiceId, ensurePlayer, handleEngineFallback, machine, settings],
  )

  const clipSpokenCaption = useCallback(() => {
    const spoken = clipAssistantToSpoken(companionCaptionFromStream(assistantTextRef.current), spokenUpToRef.current)
    holdSpokenCaptionRef.current = true
    if (spoken) {
      setRounds(current => withCurrentAssistant(current, { role: 'assistant', text: spoken }))
    }
    return spoken
  }, [])

  const interrupt = useCallback(() => {
    clipSpokenCaption()
    finishVoiceTurn('ok')
    dropCompanionPad()
    playerRef.current?.interrupt()
    setGain(0)
    speakingRef.current = false
    setAssistantAloud(false)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    if (stateRef.current === 'speaking' || stateRef.current === 'thinking') {
      applyEvent({ type: 'INTERRUPT' })
    }
    syncSpeechModesRef.current()
    captionHandleRef.current?.resumeCapture()
    speechHandleRef.current?.resumeCapture()
    setCircuitBroken(false)
    setStreamTick(0)
  }, [applyEvent, clipSpokenCaption])

  const cancelReply = useCallback(() => {
    const spoken = clipSpokenCaption()
    finishVoiceTurn('ok')
    if (talkHandleRef.current) void talkHandleRef.current.cancelOutput()
    onCancel?.(spoken)
    spokenUpToRef.current = 0
    dropCompanionPad()
    playerRef.current?.interrupt()
    setGain(0)
    speakingRef.current = false
    setAssistantAloud(false)
    setStreamTick(0)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    if (stateRef.current === 'thinking' || stateRef.current === 'speaking') {
      applyEvent({ type: 'INTERRUPT' })
    }
    syncSpeechModesRef.current()
    captionHandleRef.current?.resumeCapture()
    speechHandleRef.current?.resumeCapture()
  }, [applyEvent, clipSpokenCaption, onCancel])

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
    const interruptedListen = userInterruptedRef.current && (state === 'listening' || state === 'idle')
    if ((state === 'listening' || state === 'idle') && (interruptedListen || playerRef.current?.isBusy() !== true)) {
      speakingRef.current = false
    }
    const speakingAloud = !interruptedListen && (state === 'speaking' || speakingRef.current || playerRef.current?.isBusy() === true)
    const assistantBusy = !interruptedListen && (state === 'thinking' || speakingAloud)
    const listenThrough =
      assistantBusy &&
      companionVoiceBargeInEnabled(settingsRef.current) &&
      (activeRecognizerRef.current === 'local' || activeRecognizerRef.current === 'volc')
    const next = {
      commitPaused: assistantBusy,
      playback: assistantBusy,
      echoGuardMs: interruptEchoRef.current ? INTERRUPT_ECHO_MS : ECHO_GUARD_MS,
      listenThrough,
    }
    interruptEchoRef.current = false
    const prev = speechSyncRef.current
    if (
      prev &&
      prev.commitPaused === next.commitPaused &&
      prev.playback === next.playback &&
      prev.echoGuardMs === next.echoGuardMs &&
      prev.listenThrough === next.listenThrough
    ) {
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
      if (busy && !userInterruptedRef.current && stateRef.current !== 'listening' && stateRef.current !== 'idle') speakingRef.current = true
      if (!busy && (stateRef.current === 'listening' || stateRef.current === 'idle')) speakingRef.current = false
    }
    tick()
    const timer = window.setInterval(tick, 80)
    return () => window.clearInterval(timer)
  }, [])

  const releaseSpeakingTurn = useCallback(() => {
    setGain(0)
    speakingRef.current = false
    setAssistantAloud(false)
    if (stateRef.current === 'speaking') applyEvent({ type: 'PLAYBACK_ENDED' })
    else if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
    syncSpeechModes()
  }, [machine, syncSpeechModes])

  useEffect(() => {
    syncSpeechModes()
  }, [machine.state, streamTick, settings.fullDuplex, settings.voicePath, syncSpeechModes])

  useEffect(() => {
    if (machine.state !== 'thinking' || assistantText.trim()) {
      setConnectingHint(false)
      return
    }
    const timer = window.setTimeout(() => setConnectingHint(true), 2000)
    return () => window.clearTimeout(timer)
  }, [machine.state, assistantText])

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
      applyEvent({ type: 'REPLY_TERMINAL' })
      syncSpeechModes()
    }
  }, [error, localError, machine, syncSpeechModes])

  const beginUserTurn = useCallback(
    (transcript: string) => {
      const text = clipCompanionPrompt(cleanUserTranscript(transcript))
      const skinTurn = consumeCompanionSkinCommand(text, settingsRef.current.visualSkin === 'particle' ? 'particle' : 'classic')
      if (skinTurn) {
        const stored = loadCompanionSettings()
        saveCompanionSettings({ ...stored, visualSkin: skinTurn.next })
        setSettings(loadCompanionSettings())
        setInterimText('')
        if (settingsRef.current.autoSpeak && ttsAvailableRef.current !== false) {
          void unlockTtsAudio()
          ensurePlayer().enqueue([skinTurn.speech], { ...settingsRef.current, voiceId: activeVoiceId() }, {})
        } else {
          setEngineHint(skinTurn.speech)
        }
        return
      }
      if (text && sentThisUtteranceRef.current === text && !userInterruptedRef.current) {
        return
      }
      if (!transcriptAcceptance(text, { commit: true }) && !userInterruptedRef.current) {
        const lastAssistant = [...roundsRef.current].reverse().find(round => round.role === 'assistant')?.text ?? ''
        if (shouldQueueBusyUserTranscript({
          state: stateRef.current,
          text,
          lastSpoken: lastSpokenRef.current,
          lastAssistant,
          assistantBusy: assistantTurnBusy(),
        })) {
          pendingSendRef.current = text
          setEngineHint('下一句已记下，这轮说完就回')
          return
        }
        discardEchoCaption(transcript)
        return
      }
      if (talkHandleRef.current && !talkHandoffRef.current) {
        cancelCaptionFade()
        setInterimText('')
        setRounds([{ role: 'user', text }])
        return
      }
      cancelCaptionFade()
      userInterruptedRef.current = false
      sentThisUtteranceRef.current = text
      handledReplyRef.current = false
      staleReplyRef.current = assistantTextRef.current
      silentRestartsRef.current = 0
      spokenUpToRef.current = 0
      speakingRef.current = false
      setAssistantAloud(false)
      streamCaptionRef.current = ''
      lastActivitySpokenRef.current = ''
      toolsRanThisTurnRef.current = false
      setStreamTick(0)
      setInterimText('')
      setLocalError(undefined)
      dropCompanionPad()
      playerRef.current?.interrupt()
      onEngagedRef.current?.()
      setRounds([{ role: 'user', text }])
      if (stateRef.current === 'idle') {
        applyEvent({ type: 'MIC_ACTIVATE' })
      }
      if (!chatReadyRef.current) {
        pendingSendRef.current = text
        syncSpeechModes()
        return
      }
      applyEvent({ type: 'RECOGNIZED_FINAL' })
      startVoiceTurn(settingsRef.current.voicePath)
      markVoiceTiming('endpoint')
      void unlockTtsAudio()
      const sent = onSendRef.current(text)
      speakCompanionPad()
      syncSpeechModes()
      void Promise.resolve(sent).then(ok => {
        if (ok !== false) return
        if (userInterruptedRef.current) {
          pendingSendRef.current = null
          setEngineHint('')
          if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
          return
        }
        pendingSendRef.current = text
        setEngineHint('上一句还在发送，这轮说完再回')
        dropCompanionPad()
        if (stateRef.current === 'thinking') applyEvent({ type: 'REPLY_TERMINAL' })
      })
    },
    [applyEvent, assistantTurnBusy, syncSpeechModes, transcriptAcceptance, discardEchoCaption, ensurePlayer, activeVoiceId],
  )

  useEffect(() => {
    if (chatStatus === 'streaming' || chatStatus === 'done') return
    talkSuppressPlayRef.current = false
  }, [chatStatus])

  const seedSentRef = useRef(false)
  useEffect(() => {
    const text = seedPrompt?.trim()
    if (!text || seedSentRef.current) return
    if (sessionId && !entryReady) return
    if (talkPendingRef.current) return
    seedSentRef.current = true
    if (talkHandleRef.current && isCompanionIdleChat(text)) {
      setRounds([{ role: 'user', text }])
      return
    }
    beginUserTurn(text)
  }, [beginUserTurn, seedPrompt, sessionId, entryReady, talkLive])

  useEffect(() => {
    if (!chatReady || !pendingSendRef.current) return
    if (machine.state === 'thinking' || machine.state === 'speaking') return
    let cancelled = false
    let attempts = 0
    const flush = () => {
      if (cancelled || !chatReadyRef.current || !pendingSendRef.current) return
      if (userInterruptedRef.current) {
        pendingSendRef.current = null
        setEngineHint('')
        return
      }
      if (stateRef.current === 'thinking' || stateRef.current === 'speaking') return
      const text = pendingSendRef.current
      pendingSendRef.current = null
      const sent = onSendRef.current(text)
      void Promise.resolve(sent).then(ok => {
        if (cancelled) return
        if (ok === false) {
          if (userInterruptedRef.current) {
            pendingSendRef.current = null
            setEngineHint('')
            return
          }
          pendingSendRef.current = text
          setEngineHint('上一句还在发送，这轮说完再回')
          if (attempts++ >= 6) return
          window.setTimeout(flush, 450)
          return
        }
        if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
        applyEvent({ type: 'RECOGNIZED_FINAL' })
        speakCompanionPad()
      })
    }
    flush()
    return () => {
      cancelled = true
    }
  }, [chatReady, machine, applyEvent])

  const acceptBargeIn = useCallback(
    (transcript: string) => {
      const text = cleanUserTranscript(transcript)
      if (!text) return
      if (looksLikePlaybackEcho(text, lastSpokenRef.current)) return
      const state = stateRef.current
      if (state !== 'thinking' && state !== 'speaking') return
      staleReplyRef.current = assistantTextRef.current
      userInterruptedRef.current = true
      handledReplyRef.current = true
      cancelReply()
      beginUserTurn(text)
    },
    [beginUserTurn, cancelReply],
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
      applyEvent({ type: 'MIC_ACTIVATE' })
      applyEvent({ type: 'RECOGNIZED_FINAL' })
      applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
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
    if (!chatReadyRef.current) {
      setEngineHint('对话模型还没就绪。配好模型后再进入月伴，不会空听。')
      return
    }
    if (!allowListenRef.current) {
      setEngineHint(entryBlockRef.current || '听、说、想还没齐，不会空听。')
      return
    }
    setLocalError(undefined)
    setEngineHint('')
    unlockTtsAudio()
    if (speechHandleRef.current) {
      setRecognizerLive(true)
      syncSpeechModes()
      speechHandleRef.current.resumeCapture()
      if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
      armListenLoop()
      setHintVisible(false)
      return
    }
    if (talkHandleRef.current) {
      setRecognizerLive(true)
      if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
      armListenLoop()
      setHintVisible(false)
      return
    }
    if (openingListenRef.current) {
      listenQueuedRef.current = true
      return
    }
    openingListenRef.current = true
    setInterimText('')
    const speechOptions: CompanionSpeechOptions = {
      duplex: true,
      environment: settingsRef.current.speechEnvironment,
      onInterim: transcript => {
        const next = cleanUserTranscript(transcript)
        if (!transcriptAcceptance(next)) {
          const lastAssistant = [...roundsRef.current].reverse().find(round => round.role === 'assistant')?.text ?? ''
          if (shouldQueueBusyUserTranscript({
            state: stateRef.current,
            text: next,
            lastSpoken: lastSpokenRef.current,
            lastAssistant,
            assistantBusy: assistantTurnBusy(),
          })) {
            pendingSendRef.current = next
            setInterimText(next)
            setEngineHint('下一句已记下，这轮说完就回')
            return
          }
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
        listenOverrideRef.current = undefined
        volcDeafRestartsRef.current = 0
        beginUserTurn(transcript)
      },
      bargeIn: () =>
        companionVoiceBargeInEnabled(settingsRef.current) &&
        (activeRecognizerRef.current === 'local' || activeRecognizerRef.current === 'volc'),
      onBargeIn: acceptBargeIn,
      spokenText: () => lastSpokenRef.current,
      onError: issue => {
        speechHandleRef.current = undefined
        setRecognizerLive(false)
        // A local recognizer that dies mid-session under 'auto' is a fallback,
        // not something to make the user read and act on. An explicit 本地 card
        // is never overridden: quietly switching to the system recognizer
        // would ship audio off the machine for someone who asked it not to be.
        if (activeRecognizerRef.current === 'volc') {
          setEngineHint('火山听写连不上。已选火山卡，不会改用本机或系统识别。请检查语音模型密钥。VOICE-004')
          if (stateRef.current === 'listening') applyEvent({ type: 'MIC_CANCEL' })
          return
        }
        if (activeRecognizerRef.current === 'local' && companionListenKind(settingsRef.current.voicePath, settingsRef.current.recognizer) === 'auto') {
          localAsrReadyRef.current = false
          activeRecognizerRef.current = 'cloud'
          setAsrRoute('cloud')
          void startCompanionSpeech(speechOptions).then(adoptHandle).catch(abandon)
          return
        }
        if (!shouldKeepHandsFreeLoop({ exited: exitedRef.current, userPausedMic: false, errorCode: issue.code })) {
          setListenLoop(false)
          setLocalError(issue)
          if (stateRef.current === 'listening') applyEvent({ type: 'MIC_CANCEL' })
          return
        }
        setListenLoop(true)
        if (!isCompanionInfraBusy(issue.code)) setLocalError(issue)
        if (stateRef.current === 'listening') applyEvent({ type: 'MIC_CANCEL' })
      },
      onLevels: next => {
        setLevels(next)
      },
      onEndWithoutFinal: () => {
        if (exitedRef.current) return
        speechHandleRef.current?.resumeCapture()
        if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
      },
    }

    const adoptHandle = (handle: CompanionSpeechHandle) => {
      // begin() is fire-and-forget: startCompanionSpeech / startLocal / openCloud
      // can resolve after exit() or unmount. Without this guard the mic + ASR
      // graph reopens with no owner (same leak class as the talk handle).
      if (exitedRef.current || !stageAliveRef.current) {
        void handle.stop()
        openingListenRef.current = false
        listenQueuedRef.current = false
        return
      }
      openingListenRef.current = false
      listenQueuedRef.current = false
      if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
      speechHandleRef.current = handle
      setRecognizerLive(true)
      syncSpeechModes()
      handle.resumeCapture()
      armListenLoop()
      setHintVisible(false)
      void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
    }

    const abandon = (issue: BridgeClientError) => {
      openingListenRef.current = false
      setRecognizerLive(false)
      if (listenQueuedRef.current && !exitedRef.current) {
        listenQueuedRef.current = false
        window.setTimeout(() => {
          if (exitedRef.current || !stageAliveRef.current) return
          startListeningRef.current(auto)
        }, 80)
      }
      if (!shouldKeepHandsFreeLoop({ exited: exitedRef.current, userPausedMic: false, errorCode: issue.code })) {
        setListenLoop(false)
        setLocalError(issue)
        return
      }
      setListenLoop(true)
      if (auto) setHintVisible(true)
      else if (!isCompanionInfraBusy(issue.code)) setLocalError(issue)
    }

    const begin = async () => {
      const override = listenOverrideRef.current
      listenOverrideRef.current = undefined
      const listenKind = override ?? companionListenKind(settingsRef.current.voicePath, settingsRef.current.recognizer)
      const openCloud = () => {
        activeRecognizerRef.current = 'cloud'
        setAsrRoute('cloud')
        return startCompanionSpeech(speechOptions).then(adoptHandle)
      }
      const openLocal = async () => {
        activeRecognizerRef.current = 'local'
        setAsrRoute('local')
        const opening = Promise.resolve(startLocalCompanionSpeech(speechOptions))
        try {
          adoptHandle(await withDeadline(opening, 2500))
        } catch (issue) {
          void opening.then(handle => handle.stop(), () => {})
          throw issue
        }
      }
      const fallbackVolcToWorkingListen = async () => {
        if (exitedRef.current) {
          openingListenRef.current = false
          return
        }
        setEngineHint('火山听写连不上。已选火山卡，不会改用本机或系统识别。请检查语音模型密钥。VOICE-004')
        openingListenRef.current = false
        if (stateRef.current === 'listening') applyEvent({ type: 'MIC_CANCEL' })
      }

      const adoptTalk = (handle: CompanionTalkHandle) => {
        if (exitedRef.current || !stageAliveRef.current) {
          void handle.stop()
          openingListenRef.current = false
          talkPendingRef.current = false
          return
        }
        talkHandleRef.current = handle
        talkPendingRef.current = false
        setTalkLive(true)
        setAsrRoute('volc')
        setEntryLights(companionTalkLiveLights(preparedLightsRef.current))
        openingListenRef.current = false
        listenQueuedRef.current = false
        if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
        setRecognizerLive(true)
        armListenLoop()
        setHintVisible(false)
        void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
      }

      if (shouldOfferCompanionTalk(settingsRef.current.voicePath, hasTalkModelRef.current, sessionIdRef.current, settingsRef.current.talkRealtime) && !talkRetryBlocked(talkRetryRef.current, Date.now())) {
        talkPendingRef.current = true
        try {
          const handle = await startCompanionTalk({
            sessionId: sessionIdRef.current!,
            onAudio: (pcm, mime) => {
              if (exitedRef.current || talkSuppressPlayRef.current) return
              const player = ensurePlayer()
              const rate = /rate=(\d+)/.exec(mime)?.[1]
              player.enqueueTalkPcm(pcm, rate ? Number(rate) : 24_000)
              speakingRef.current = true
              setAssistantAloud(true)
              if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
              if (stateRef.current === 'listening' || stateRef.current === 'thinking') {
                applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
              }
              markVoiceTiming('firstAudio')
            },
            onUserTranscript: transcript => {
              if (exitedRef.current || !stageAliveRef.current) return
              const next = cleanUserTranscript(transcript)
              if (!next) return
              onEngagedRef.current?.()
              setHeardThisVisit(true)
              setVoiceHeard(true)
              setInterimText('')
              setRounds([{ role: 'user', text: next }])
              if (stateRef.current === 'speaking') {
                playerRef.current?.interrupt()
                void talkHandleRef.current?.cancelOutput()
              }
              if (stateRef.current === 'idle') applyEvent({ type: 'MIC_ACTIVATE' })
              if (stateRef.current === 'listening' || stateRef.current === 'idle') applyEvent({ type: 'RECOGNIZED_FINAL' })
            },
            onAssistantTranscript: transcript => {
              if (exitedRef.current || !stageAliveRef.current) return
              const piece = transcript.trim()
              if (!piece) return
              setRounds(current => {
                const last = current[current.length - 1]
                const prev = last?.role === 'assistant' ? last.text : ''
                const shown = !prev ? piece : piece.startsWith(prev) ? piece : prev + piece
                return withCurrentAssistant(current, { role: 'assistant', text: shown })
              })
            },
            onBarge: () => {
              if (exitedRef.current || !stageAliveRef.current) return
              const spoken = clipSpokenCaption()
              onCancel?.(spoken)
              playerRef.current?.interrupt()
              speakingRef.current = false
              setAssistantAloud(false)
              void talkHandleRef.current?.cancelOutput()
              if (stateRef.current === 'thinking' || stateRef.current === 'speaking') {
                applyEvent({ type: 'MIC_CLICK_WHILE_SPEAKING' })
              }
            },
            onToolHandoff: text => {
              if (exitedRef.current || !stageAliveRef.current) return
              talkSuppressPlayRef.current = true
              talkHandoffRef.current = true
              void talkHandleRef.current?.cancelOutput()
              beginUserTurn(text)
              talkHandoffRef.current = false
            },
            onError: issue => {
              if (issue.code === 'TALK_BARGE') return
              setEngineHint(issue.message || TALK_FALLBACK_BANNER)
            },
            onEnded: () => {
              talkHandleRef.current = undefined
              talkLiveRef.current = false
              // Unmount cleanup already stopped/cleared talk and disposed the
              // player; skip the UI updates to avoid setState after unmount.
              if (exitedRef.current || !stageAliveRef.current) return
              playerRef.current?.interrupt()
              setTalkLive(false)
              setEntryLights(preparedLightsRef.current)
              const caption = assistantTextRef.current.trim()
              setRounds(current => {
                if (current.some(round => round.role === 'assistant' && round.text.trim())) return current
                return caption ? withCurrentAssistant(current, { role: 'assistant', text: caption }) : current
              })
            },
          }, { firstAudioMs: TALK_FIRST_AUDIO_MS })
          if (exitedRef.current) {
            await handle?.stop()
            openingListenRef.current = false
            talkPendingRef.current = false
            return
          }
          if (handle) {
            adoptTalk(handle)
            return
          }
        } catch {
          /* cascade below */
        }
        talkRetryRef.current = noteTalkFailure(talkRetryRef.current, Date.now())
        talkPendingRef.current = false
        talkLiveRef.current = false
        setTalkLive(false)
        setEngineHint(
          talkRetryRef.current.failures >= TALK_MAX_FAILURES
            ? '通话核未就绪。已选火山卡，不会改用本机或系统识别。VOICE-004'
            : '通话核连接不稳，本轮先用语模型，稍后自动重试通话。',
        )
      }

      if (listenKind === 'volc') {
        activeRecognizerRef.current = 'volc'
        setAsrRoute('volc')
        try {
          const listed = await Promise.race([
            getProviderBridge().list().catch(() => undefined),
            new Promise<undefined>(resolve => {
              window.setTimeout(() => resolve(undefined), VOLC_ASR_DECISION_MS)
            }),
          ])
          if (exitedRef.current) {
            openingListenRef.current = false
            return
          }
          const picked = listed ? pickDefaultVoice(listed.items) : undefined
          if (!picked) {
            await fallbackVolcToWorkingListen()
            return
          }
          const opening = Promise.resolve(startVolcCompanionSpeech(speechOptions, picked.provider.id))
          try {
            const handle = await withDeadline(opening, VOLC_ASR_DECISION_MS)
            if (exitedRef.current || !openingListenRef.current) {
              handle.stop()
              openingListenRef.current = false
              return
            }
            adoptHandle(handle)
          } catch {
            void opening.then(handle => handle.stop(), () => {})
            throw new Error('LISTEN_DEADLINE')
          }
        } catch {
          if (exitedRef.current) {
            openingListenRef.current = false
            return
          }
          await fallbackVolcToWorkingListen()
        }
        return
      }

      if (listenKind === 'local') {
        try {
          await openLocal()
        } catch (issue) {
          try {
            await new Promise<void>(resolve => {
              window.setTimeout(resolve, 400)
            })
            if (exitedRef.current) {
              openingListenRef.current = false
              return
            }
            await openLocal()
          } catch {
            abandon(issue as BridgeClientError)
          }
        }
        return
      }

      // Cloud card, or leftover 'auto' on 云端: prefer sherpa when it is already
      // installed so talking still works if Web Speech is dead in WebView2.
      const installed =
        listenKind === 'auto'
          ? await readyWithin(localAsrProbeRef.current, LOCAL_ASR_DECISION_MS)
          : false
      if (exitedRef.current) {
        openingListenRef.current = false
        return
      }
      const preferLocal = listenKind === 'auto' && installed
      try {
        if (preferLocal) await openLocal()
        else await openCloud()
      } catch (issue) {
        if (preferLocal) {
          localAsrReadyRef.current = false
          void openCloud().catch(abandon)
          return
        }
        const localReady = await readyWithin(localAsrProbeRef.current, LOCAL_ASR_DECISION_MS)
        if (localReady && !exitedRef.current) {
          setEngineHint('系统识别不可用，已改用本机识别')
          void openLocal().catch(abandon)
          return
        }
        abandon(issue as BridgeClientError)
      }
    }
    void begin()
  }, [applyEvent, armListenLoop, beginUserTurn, acceptBargeIn, markAssistantAloud, setListenLoop, syncSpeechModes, transcriptAcceptance, discardEchoCaption, clipSpokenCaption, ensurePlayer, onCancel])

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
        applyEvent({ type: 'MIC_ACTIVATE' })
        return
      }
      if (captionHandleRef.current) {
        silentRestartsRef.current = 0
        captionHandleRef.current.resumeCapture()
        applyEvent({ type: 'MIC_ACTIVATE' })
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
      const preferred = companionListenKind(settingsRef.current.voicePath, settingsRef.current.recognizer)
      const next = companionListenFailover(
        activeRecognizerRef.current,
        preferred,
        localAsrReadyRef.current,
      )
      if (companionVolcDeafGiveUp(preferred, next, volcDeafRestartsRef.current + 1)) {
        volcDeafRestartsRef.current = 0
        setEngineHint('火山听写听得到声音但转不出字。已选火山卡，不会改用本机识别。VOICE-004')
        if (stateRef.current === 'listening') applyEvent({ type: 'MIC_CANCEL' })
        return
      }
      if (preferred === 'volc' && next === 'volc') volcDeafRestartsRef.current += 1
      else volcDeafRestartsRef.current = 0
      listenOverrideRef.current = next
      speechHandleRef.current?.stop()
      speechHandleRef.current = undefined
      captionHandleRef.current?.stop()
      captionHandleRef.current = undefined
      openingListenRef.current = false
      setRecognizerLive(false)
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

  useEffect(() => {
    if (machine.state === 'idle' && !settings.fullDuplex && heardThisVisit) setHintVisible(true)
  }, [machine.state, settings.fullDuplex, heardThisVisit])

  useEffect(() => {
    if (machine.state === 'idle') sentThisUtteranceRef.current = ''
  }, [machine.state])

  useEffect(() => () => {
    exitedRef.current = true
    pendingSendRef.current = null
    sentThisUtteranceRef.current = ''
  }, [])

  useEffect(() => {
    if (machine.state !== 'listening' || !interimText.trim()) return
    const timer = window.setTimeout(() => {
      if (stateRef.current !== 'listening' || exitedRef.current) return
      const caption = interimTextRef.current.trim()
      if (!caption || sentThisUtteranceRef.current === clipCompanionPrompt(cleanUserTranscript(caption))) return
      const committed =
        speechHandleRef.current?.forceCommit(caption) === true ||
        captionHandleRef.current?.forceCommit(caption) === true
      if (stageForceCommitMayBeginTurn(committed)) beginUserTurn(caption)
    }, FORCE_COMMIT_MS)
    return () => window.clearTimeout(timer)
  }, [machine.state, interimText, beginUserTurn])

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
    const fade = window.setTimeout(() => {
      if (interimTextRef.current.trim()) {
        cancelCaptionFade()
        return
      }
      setCaptionFading(true)
    }, 700)
    const clear = window.setTimeout(() => {
      fadeTimersRef.current = null
      const pending = interimTextRef.current.trim()
      const user = roundsRef.current.find(round => round.role === 'user')
      if (pending || (user && (stateRef.current === 'listening' || stateRef.current === 'idle'))) {
        setCaptionFading(false)
        return
      }
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

  startListeningRef.current = startListening

  useEffect(() => {
    if (!chatReady) return
    let cancelled = false
    const probe = (attempt: number) => {
      if (cancelled || exitedRef.current) return
      if (speechHandleRef.current || openingListenRef.current) return
      void prepareCompanionEntry(settingsRef.current, thinkProviderId && thinkModelId ? { preferredLLM: { providerId: thinkProviderId, modelId: thinkModelId } } : undefined).then(prepared => {
        if (cancelled || exitedRef.current) return
        setSettings(prepared.settings)
        settingsRef.current = prepared.settings
        setEntryLights(prepared.lights)
        setEntryBlock(prepared.blockReason)
        entryBlockRef.current = prepared.blockReason
        allowListenRef.current = prepared.allowListen
        hasTalkModelRef.current = prepared.hasTalkModel
        preparedLightsRef.current = prepared.lights
        setEntryReady(true)
        talkPendingRef.current = shouldOfferCompanionTalk(prepared.settings.voicePath, prepared.hasTalkModel, sessionIdRef.current, prepared.settings.talkRealtime) && !talkRetryBlocked(talkRetryRef.current, Date.now())
        if (!prepared.allowListen) {
          autoStartTriedRef.current = false
          setLocalError(new BridgeClientError(prepared.blockReason || '听、说、想还没齐，不会空听。', 'CHAT_CONFIG_MISSING', false, 'engine'))
          const delay = Math.min(800 * 2 ** attempt, 6400)
          entryRetryTimerRef.current = window.setTimeout(() => probe(attempt + 1), delay)
          return
        }
        autoStartTriedRef.current = true
        setLocalError(undefined)
        // V3 (opt-in): warm the cold-start synth engine before the first reply.
        void prewarmVoice(prepared.settings)
        speakReadyRef.current = prepared.speakReady
        if (!prepared.speakReady && prepared.settings.voicePath === 'local') {
          const launching = prepared.lights.some(light => light.key === 'speak' && (light.label.includes('启动中') || light.label.includes('克隆未就绪')))
          setEngineHint(launching ? '本机朗读还在启动，这轮先用晓晓读。' : '本机朗读未就绪，这轮先用晓晓读给你听。')
          setEntryLights(current => current.map(light => (
            light.key === 'speak'
              ? { ...light, label: light.label.includes('克隆未就绪') ? light.label : '晓晓（本机朗读未就绪）', state: 'warn' as const }
              : light
          )) as typeof current)
        }
        if (stateRef.current === 'idle' && !speechHandleRef.current) startListeningRef.current(true)
      })
    }
    probe(0)
    return () => {
      cancelled = true
      window.clearTimeout(entryRetryTimerRef.current)
    }
  }, [chatReady, thinkProviderId, thinkModelId])

  const stopAssistantAndListen = useCallback(() => {
    const state = stateRef.current
    if (state !== 'thinking' && state !== 'speaking') return
    userInterruptedRef.current = true
    handledReplyRef.current = true
    pendingSendRef.current = null
    setEngineHint('')
    const spoken = clipSpokenCaption()
    if (talkHandleRef.current) void talkHandleRef.current.cancelOutput()
    onCancel?.(spoken)
    spokenUpToRef.current = 0
    dropCompanionPad()
    playerRef.current?.interrupt()
    setGain(0)
    speakingRef.current = false
    setAssistantAloud(false)
    setStreamTick(0)
    interruptEchoRef.current = true
    echoUntilRef.current = performance.now() + INTERRUPT_ECHO_MS
    armListenLoop()
    applyEvent({ type: 'MIC_CLICK_WHILE_SPEAKING' })
    syncSpeechModes()
    speechHandleRef.current?.resumeCapture()
    captionHandleRef.current?.resumeCapture()
    if (!speechHandleRef.current && !captionHandleRef.current) {
      startListening(false)
    }
  }, [applyEvent, armListenLoop, clipSpokenCaption, onCancel, startListening, syncSpeechModes])

  const pauseListening = useCallback(() => {
    speechHandleRef.current?.stop()
    speechHandleRef.current = undefined
    captionHandleRef.current?.stop()
    captionHandleRef.current = undefined
    setRecognizerLive(false)
    setListenLoop(false)
    silentRestartsRef.current = 0
    applyEvent({ type: 'MIC_CANCEL' })
  }, [applyEvent, setListenLoop])

  const toggleMic = useCallback(() => {
    void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
    const state = stateRef.current
    if (state === 'listening') return
    if (state === 'thinking' || state === 'speaking') {
      stopAssistantAndListen()
      return
    }
    startListening(false)
  }, [startListening, stopAssistantAndListen])

  const interruptAssistant = stopAssistantAndListen

  const exit = useCallback(() => {
    exitedRef.current = true
    pendingSendRef.current = null
    sentThisUtteranceRef.current = ''
    setListenLoop(false)
    openingListenRef.current = false
    speechHandleRef.current?.stop()
    speechHandleRef.current = undefined
    captionHandleRef.current?.stop()
    captionHandleRef.current = undefined
    const talk = talkHandleRef.current
    talkHandleRef.current = undefined
    talkPendingRef.current = false
    setTalkLive(false)
    void talk?.stop()
    if (stateRef.current === 'speaking' || stateRef.current === 'thinking') cancelReply()
    onExit()
  }, [cancelReply, onExit, setListenLoop])

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

  useEffect(() => {
    const reopen = () => {
      if (exitedRef.current || document.visibilityState === 'hidden') return
      if (stateRef.current !== 'listening' && stateRef.current !== 'idle') return
      const handle = speechHandleRef.current ?? captionHandleRef.current
      if (handle) {
        handle.resumeCapture()
        handle.pulseRecognition()
        return
      }
      if (stateRef.current === 'listening' || autoLoopRef.current) {
        openingListenRef.current = false
        startListeningRef.current(true)
      }
    }
    document.addEventListener('visibilitychange', reopen)
    const devices = navigator.mediaDevices
    devices?.addEventListener?.('devicechange', reopen)
    return () => {
      document.removeEventListener('visibilitychange', reopen)
      devices?.removeEventListener?.('devicechange', reopen)
    }
  }, [])

  useEffect(() => {
    if (!asrRoute) return
    const label = companionListenLightLabel(asrRoute)
    setEntryLights(current => {
      const listen = current.find(light => light.key === 'listen')
      if (listen?.label === label && listen.state === 'on') return current
      return current.map(light => (light.key === 'listen' ? { ...light, label, state: 'on' as const } : light)) as [EntryLight, EntryLight, EntryLight]
    })
  }, [asrRoute])

  const retrySegment = () => {
    setCircuitBroken(false)
    const last = rounds[rounds.length - 1]
    if (last?.role === 'assistant' && last.text) {
      applyEvent({ type: 'MIC_ACTIVATE' })
      applyEvent({ type: 'RECOGNIZED_FINAL' })
      applyEvent({ type: 'REPLY_COMPLETED', speakable: true })
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
  const surfaceState = companionSurfaceState(machine.state, (assistantAloud || playerSounding) && !executing && !userInterruptedRef.current, executing)
  const liveCaption = !!interimText.trim() && (machine.state === 'listening' || machine.state === 'idle')
  interimTextRef.current = interimText

  return (
    <div
      className="companion-stage"
      ref={rootRef}
      tabIndex={-1}
      data-state={surfaceState}
      data-skin={visualSkin}
      data-started={autoLoopRef.current || machine.state !== 'idle'}
      data-hands-free={handsFree ? 'true' : 'false'}
      data-asr-route={asrRoute || undefined}
      data-talk-live={talkLive ? 'true' : undefined}
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
      <div className="companion-aurora" aria-hidden="true">
        {visualSkin === 'particle' ? (
          canUseCompanionWebgl() ? (
            <ParticleMoonScene
              state={surfaceState}
              gain={gain}
              levels={levels}
              burstToken={0}
              high={dustHigh}
              onFps={fps => {
                if (fps && fps < 40 && dustHigh) {
                  lowFps.current += 1
                  if (lowFps.current >= 8) setDustHigh(false)
                  return
                }
                lowFps.current = 0
              }}
            />
          ) : (
            <div className="stardust-sim-fallback" aria-hidden="true">
              <span className="stardust-sim-disc" />
            </div>
          )
        ) : canUseCompanionWebgl() ? (
          <Aurora
            colorStops={AURORA_STOPS}
            {...auroraForEnter(surfaceState, gain, enter)}
          />
        ) : (
          <div className="companion-aurora-fallback" />
        )}
      </div>
      <div className="companion-banners">
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
          朗读断了，看字幕
          <button type="button" onClick={retrySegment}>
            重试本段朗读
          </button>
          <button type="button" onClick={() => setCircuitBroken(false)}>
            忽略
          </button>
        </div>
      )}
      {(localError ?? error) && (localError ?? error)!.code === 'CHAT_CONFIG_MISSING' && (
        <div className="companion-banner warn" role="status">
          {(localError ?? error)!.message}
        </div>
      )}
      {(localError ?? error) &&
        (localError ?? error)!.code !== 'CHAT_CONFIG_MISSING' && (
        <div className="companion-banner error" role="alert">
          {(localError ?? error)!.message}
          <span>代码 {(localError ?? error)!.code}</span>
        </div>
      )}
      {computerControlOff && (
        <div className="companion-banner warn persist-failed-banner" role="status">
          电脑控制未启用。第一次控桌面请到设置里打开，月伴不会自己打开。
        </div>
      )}
      {persistFailed && (
        <div className="companion-banner warn persist-failed-banner" role="status">
          回答已经生成，但没写进会话。
          <button type="button" onClick={() => onRetryPersist?.()}>只重试写入</button>
        </div>
      )}
      {resumeAvailable && !persistFailed && (
        <div className="companion-banner warn turn-resume-banner" role="status">
          上次对话还没做完。
          <button type="button" onClick={() => onResume?.()}>继续上次</button>
        </div>
      )}
      {memorySummary && !persistFailed && (
        onOpenMemory
          ? <button type="button" className="companion-banner companion-memory-link" onClick={onOpenMemory}>{memorySummary}</button>
          : <div className="companion-banner" role="status">{memorySummary}</div>
      )}
      {!chatReady && (
        <div className="companion-banner warn" role="status">
          对话模型还没就绪。配好模型后再进入月伴，不会空听。
        </div>
      )}
      {ttsAvailable === false && (
        <div className="companion-banner warn" role="status">
          朗读断了，看字幕
        </div>
      )}
      </div>
      <div className="companion-stage-core">
      <div className="companion-moon-slot">
      {visualSkin === 'classic' ? (
      <MoonSphere
        state={surfaceState}
        gain={gain}
        levels={levels}
        enter={enter}
        interruptible={surfaceState !== 'listening' || audioLocked}
        onInterrupt={
          machine.state === 'speaking' || machine.state === 'thinking' || surfaceState === 'speaking' || surfaceState === 'thinking'
            ? interruptAssistant
            : surfaceState === 'listening'
              ? () => {
                  void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
                }
              : toggleMic
        }
      />
      ) : (
      <div className={`companion-moon state-${surfaceState}`} data-state={surfaceState} data-visual="css">
        <button
          type="button"
          className="companion-moon-body"
          tabIndex={-1}
          aria-label={
            surfaceState === 'idle'
              ? '月亮：轻点开始说话'
              : surfaceState === 'listening'
                ? '月亮正在聆听'
                : surfaceState === 'thinking'
                  ? '月亮正在回应'
                  : '月亮正在说话，点击打断朗读'
          }
          onClick={
            machine.state === 'speaking' || machine.state === 'thinking' || surfaceState === 'speaking' || surfaceState === 'thinking'
              ? interruptAssistant
              : surfaceState === 'listening'
                ? () => {
                    void unlockTtsAudio().then(() => setAudioLocked(getTtsAudioState() !== 'running'))
                  }
                : toggleMic
          }
        />
      </div>
      )}
      </div>
      {userAsk && onUserAsk && (
        <UserAskWizard pack={userAsk} busy={chatStatus === 'streaming'} onSubmit={onUserAsk} />
      )}
      <div className="companion-status" aria-live="polite">
        <span className={`companion-status-dot state-${surfaceState}`} aria-hidden="true" />
        {companionStatusLabel(surfaceState, executing)}
        {surfaceState === 'listening' && (
          <span className="companion-status-sub">
            {interimText.trim()
              ? '正在听你说…'
              : !recognizerLive
                ? '正在接通麦克风…'
                : voiceHeard
                  ? '正在听…'
                  : heardThisVisit || rounds.some(round => round.role === 'user')
                    ? '我在听'
                    : '我在听，请说话'}
          </span>
        )}
        {surfaceState === 'thinking' && (
          <span className="companion-status-sub">
            {userAsk
              ? '要你点，我不能代点是'
              : activityStatus?.trim() ||
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
        {asrRoute && (surfaceState === 'listening' || surfaceState === 'speaking') && (
          <span className="companion-status-path">{companionAsrPathLabel(asrRoute, settings.recognizer)}</span>
        )}
      </div>
      <ToolTrajectory items={toolActivities ?? []} />
      {pendingApproval && (
        <div className="companion-approval" role="dialog" aria-label="确认危险操作">
          <p>{pendingApproval.summary?.trim() || `月汐想执行 ${pendingApproval.name}，需要你确认。`}</p>
          <div className="companion-approval-actions">
            <button type="button" onClick={onApproveTool}>允许这一次</button>
            <button type="button" onClick={onRejectTool}>拒绝</button>
          </div>
        </div>
      )}
      {hintVisible && machine.state === 'idle' && (
        <p className="companion-hint" aria-live="polite">
          {zh
            ? `轻点月亮或按空格开始说话；说完再答。不想听完点「打断」或按 ${formatInterruptHotkey(settings.interruptHotkey)}`
            : `Tap the moon or press Space to talk; tap Interrupt or ${formatInterruptHotkey(settings.interruptHotkey)} while she is speaking`}
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
          {connectingHint && !engineHint && (
            <p className="companion-subtitle-hint">正在接，还没开口</p>
          )}
          {engineHint && (
            <p className="companion-subtitle-hint warn">{engineHint}</p>
          )}
          {deafRecognizer && (
            <p className="companion-subtitle-hint warn">
              {activeRecognizerRef.current === 'local'
                ? '听到你的声音了，本机识别还没有出字…'
                : activeRecognizerRef.current === 'volc'
                  ? '听到你的声音了，火山听写还没有出字…'
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
      </div>
      <div className="companion-chrome">
        <button type="button" className="companion-exit" aria-label={zh ? '退出月伴对话（Esc）' : 'Exit companion talk (Esc)'} onClick={exit}>
          {zh ? '退出' : 'Exit'}
        </button>
        <button
          type="button"
          className="companion-interrupt"
          aria-label={zh ? `打断月汐（${formatInterruptHotkey(settings.interruptHotkey)}）` : `Interrupt (${formatInterruptHotkey(settings.interruptHotkey)})`}
          disabled={surfaceState !== 'thinking' && surfaceState !== 'speaking'}
          onClick={interruptAssistant}
        >
          {zh ? '打断' : 'Interrupt'}
          <span className="companion-interrupt-key">{formatInterruptHotkey(settings.interruptHotkey)}</span>
        </button>
        <button
          type="button"
          className="companion-pause"
          aria-label={
            machine.state === 'thinking' || machine.state === 'speaking'
              ? (zh ? '停止这一轮' : 'Stop this turn')
              : (zh ? '暂停聆听' : 'Pause listening')
          }
          disabled={machine.state === 'idle'}
          onClick={() => {
            if (machine.state === 'thinking' || machine.state === 'speaking') {
              interruptAssistant()
              return
            }
            pauseListening()
          }}
        >
          {machine.state === 'thinking' || machine.state === 'speaking' ? (zh ? '停止' : 'Stop') : (zh ? '暂停' : 'Pause')}
        </button>
        <CompanionSkinSwitch compact zh={zh} value={visualSkin} onChange={applyVisualSkin} />
        <CompanionEntryLights lights={entryLights} thinkReady={chatReady && !entryBlock.includes('对话模型')} />
      </div>
    </div>
  )
}

function SubtitleRow({ round, live }: { round: SubtitleRound; live?: boolean }): React.JSX.Element {
  const zh = useZh()
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
      <span className="who" aria-hidden="true">{zh ? '月汐' : 'Lunitide'}</span>
      <span className="plain-text">{round.text || '…'}</span>
    </p>
  )
}

