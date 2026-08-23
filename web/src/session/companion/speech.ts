// speech.ts is the M9.5 companion voice input (T-9.5.3.1): it reuses
// the toggleSpeech pipeline (getUserMedia with the saved microphone,
// zh-CN final-only SpeechRecognition, analyser-driven levels, and the
// frozen microphone/speech error codes) but routes each final
// transcript straight to ChatBridge instead of the composer.
import { BridgeClientError } from '../../bridge/client'
import { microphoneConstraints, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { MOON_RING_BINS } from './MoonSphere'
import { looksIncompleteUtterance } from './companionText'
import { unlockTtsAudio } from './ttsPlayer'

type SpeechRecognitionEventLike = { resultIndex: number; results: ArrayLike<{ 0: { transcript: string }; isFinal: boolean }> }
type SpeechRecognitionLike = {
  lang: string
  continuous: boolean
  interimResults: boolean
  maxAlternatives?: number
  onresult: ((event: SpeechRecognitionEventLike) => void) | null
  onerror: ((event?: { error?: string }) => void) | null
  onend: (() => void) | null
  onspeechstart?: (() => void) | null
  onspeechend?: (() => void) | null
  onsoundstart?: (() => void) | null
  onaudiostart?: (() => void) | null
  start: () => void
  stop: () => void
}

const speechRecognitionConstructor = () =>
  (window as typeof window & { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike }).SpeechRecognition ??
  (window as typeof window & { webkitSpeechRecognition?: new () => SpeechRecognitionLike }).webkitSpeechRecognition

export interface CompanionSpeechCallbacks {
  /** A final transcript arrived — sent straight to ChatBridge by the stage. */
  onFinal: (transcript: string) => void
  /** Unused: automatic voice barge-in is off; interrupt is a button. */
  onBargeIn?: (transcript: string) => void
  /** Interim transcript (real-time) — shown as a live caption while the user talks. */
  onInterim?: (transcript: string) => void
  /** Acoustic start of speech, even before the first transcript. */
  onSpeechStart?: () => void
  /** Mic energy detected — instant feedback before the first SR token. */
  onVoiceEnergy?: () => void
  /** Error mapped to the frozen microphone/speech error codes. */
  onError: (error: BridgeClientError) => void
  /** 12 normalized ring levels, ~30fps while listening. */
  onLevels?: (levels: number[]) => void
  /** Recognition ended without a final (silence timeout, engine stop). */
  onEndWithoutFinal?: () => void
  /** Transient engine issue (network / audio-capture) — shown, then SR retries. */
  onEngineHint?: (message: string) => void
}

export type SpeechEnvironment = 'normal' | 'noisy'

export interface SpeechProfile {
  voicePeak: number
  utteranceSilenceMs: number
  utteranceStableMs: number
  bargeInPeakDelta: number
  minVoiceHoldMs: number
}

export interface CompanionSpeechOptions extends CompanionSpeechCallbacks {
  /** Keep mic + recognition alive between turns (phone-call loop). */
  duplex?: boolean
  /** Kept for API compatibility; automatic voice barge-in is always off. */
  bargeIn?: boolean
  /** Tighter endpointing when cafes / background voices are present. */
  environment?: SpeechEnvironment
}

export interface CompanionSpeechHandle {
  /** Stop recognition and release the microphone immediately. */
  stop: () => void
  /** Assistant TTS active — mute the mic and ignore recognizer output. */
  setAssistantPlayback: (active: boolean, echoGuardMs?: number) => void
  /** Pause silence-based commit while thinking/speaking. */
  setCommitPaused: (paused: boolean) => void
  /** No-op when false (default). Automatic voice barge-in stays off. */
  setBargeInActive: (active: boolean) => void
  /** Restart the recognizer if it stalled (listening with no transcript). */
  pulseRecognition: () => void
  /** Force-send the current assembled transcript (fallback when endpointing stalls). */
  forceCommit: () => void
}

/** Commit after this much analyser silence once we already have text. */
export const UTTERANCE_SILENCE_MS = 180
/** Commit when interim/final text stops changing (Windows SR re-fires the same interim). */
export const UTTERANCE_STABLE_MS = 220
/** Incomplete-looking phrases (mid-command tails) need a longer stable window. */
export const INCOMPLETE_STABLE_MS = 480
/** Incomplete phrases also tolerate a longer trailing pause before commit. */
export const INCOMPLETE_SILENCE_MS = 360
/** Minimum time with text on screen before we accept a non-terminal commit. */
export const MIN_UTTERANCE_MS = 220
/** Force-commit if the same transcript sits unchanged while listening. */
export const STUCK_TRANSCRIPT_MS = 600
/** Restart SR when mic hears voice but no transcript arrives. */
export const VOICE_WITHOUT_TEXT_MS = 700
/** How long since the last SR token before a voice-energy restart. */
export const VOICE_RESTART_RESULT_MS = 500
/** Minimum gap between stall-recovery restarts (avoids WebView SR crashes). */
export const STALL_RESTART_GAP_MS = 1600
/** Release getUserMedia this long before SpeechRecognition.start(). */
export const MIC_RELEASE_BEFORE_SR_MS = 350
/** Recycle a silent WebView SR session quickly. Windows `continuous=true`
 *  often starts without throwing and then never emits results; waiting
 *  several seconds here is what made quiet/normal speech look like
 *  “还没听清”. One-shot sessions end on their own — this only kicks
 *  zombie continuous sessions or a hung start(). Never stop() while
 *  speechActive or text is already on screen. */
export const STALL_RESTART_AFTER_MS = 1800

/** Energy above this (0–1 peak) counts as voice for endpointing. */
export const VOICE_PEAK = 0.07
/** Stronger peak while assistant speaks — avoids TTS bleed despite AEC. */
export const BARGE_IN_PEAK = 0.16

export function speechProfile(environment: SpeechEnvironment = 'normal'): SpeechProfile {
  if (environment === 'noisy') {
    return {
      voicePeak: 0.12,
      utteranceSilenceMs: 420,
      utteranceStableMs: 480,
      bargeInPeakDelta: 0.04,
      minVoiceHoldMs: 200,
    }
  }
  return {
    voicePeak: VOICE_PEAK,
    utteranceSilenceMs: UTTERANCE_SILENCE_MS,
    utteranceStableMs: UTTERANCE_STABLE_MS,
      bargeInPeakDelta: 0.03,
      minVoiceHoldMs: 0,
    }
}
/** Sustained voice before a barge-in fires during thinking. Playback uses energy gate. */
export const BARGE_IN_HOLD_MS = 90
/** After TTS ends, ignore mic/SR until speaker ring-out dies.
 *  Laptop speakers + built-in mic leave 300–600ms of residual echo even
 *  with Chromium/WASAPI AEC — shorter guards transcribe her own voice. */
export const ECHO_GUARD_MS = 420
/** After a click interrupt, unmute quickly so the user can talk. */
export const INTERRUPT_ECHO_MS = 80

/** Sustained loud voice during assistant TTS playback (energy-only barge-in). */
export const BARGE_IN_PLAYBACK_HOLD_MS = 80

export function shouldHoldRecognition(playback: boolean, guardUntil: number, now: number, allowPlaybackBargeIn = false): boolean {
  if (allowPlaybackBargeIn) return now < guardUntil
  return playback || now < guardUntil
}

export function shouldBargeInDuringPlayback(peak: number, voiceForMs: number, holdMs = BARGE_IN_PLAYBACK_HOLD_MS): boolean {
  return peak >= BARGE_IN_PEAK && voiceForMs >= holdMs
}

export function shouldCommitUtterance(hasText: boolean, silentForMs: number, silenceMs = UTTERANCE_SILENCE_MS): boolean {
  return hasText && silentForMs >= silenceMs
}

export function shouldCommitStable(hasText: boolean, stableForMs: number, stableMs = UTTERANCE_STABLE_MS): boolean {
  return hasText && stableForMs >= stableMs
}

export function endpointingForText(text: string, profile: SpeechProfile): { stableMs: number; silenceMs: number } {
  if (looksIncompleteUtterance(text)) {
    return { stableMs: INCOMPLETE_STABLE_MS, silenceMs: INCOMPLETE_SILENCE_MS }
  }
  return { stableMs: profile.utteranceStableMs, silenceMs: profile.utteranceSilenceMs }
}

export function shouldDeferCommit(text: string, textSinceMs: number): boolean {
  const trimmed = text.trim()
  if (!trimmed) return true
  if (/[。？！?!…]$/.test(trimmed)) return false
  return textSinceMs < MIN_UTTERANCE_MS
}

export function shouldBargeIn(hasText: boolean, voiceForMs: number, holdMs = BARGE_IN_HOLD_MS): boolean {
  return hasText && voiceForMs >= holdMs
}

export const PERMANENT_SPEECH_ERRORS = new Set(['not-allowed', 'service-not-allowed', 'language-not-supported'])
export const isPermanentSpeechError = (error?: string) => !!error && PERMANENT_SPEECH_ERRORS.has(error)

/** Windows Speech Runtime matches the installed zh-CN pack, not zh-Hans-CN. */
export function companionRecognitionLang(navigatorLanguage = typeof navigator !== 'undefined' ? navigator.language : ''): string {
  const lang = (navigatorLanguage || '').trim()
  if (!lang) return 'zh-CN'
  const lower = lang.toLowerCase()
  if (lower === 'zh' || lower.startsWith('zh-')) return 'zh-CN'
  return lang
}

/** Idle moon rings must stay below VOICE_PEAK so they never look like speech. */
export function idleMeterLevel(t: number, index: number): number {
  return 0.018 + 0.022 * Math.abs(Math.sin(t * 0.6 + index * 0.45))
}

export function shouldRestartStalledRecognition(input: {
  speechActive: boolean
  hasText: boolean
  held: boolean
  restarting: boolean
  msSinceStart: number
  minSessionMs?: number
}): boolean {
  if (input.speechActive || input.hasText || input.held || input.restarting) return false
  return input.msSinceStart >= (input.minSessionMs ?? STALL_RESTART_AFTER_MS)
}

export function speechEngineHint(error?: string): string {
  if (error === 'audio-capture') return '麦克风被占用或尚未释放，正在重试识别'
  if (error === 'network') return '语音识别网络中断，正在重试'
  if (error === 'no-speech') return ''
  if (error === 'aborted') return ''
  return error ? `语音识别暂时失败（${error}），正在重试` : ''
}

export function startCompanionSpeech(options: CompanionSpeechOptions): Promise<CompanionSpeechHandle> {
  const { duplex = false, bargeIn: _unusedBargeIn = false, environment = 'normal', ...callbacks } = options
  const bargeIn = false
  const profile = speechProfile(environment)
  const Recognition = speechRecognitionConstructor()
  if (!Recognition || !navigator.mediaDevices?.getUserMedia) {
    return Promise.reject(new BridgeClientError('当前系统 WebView 不支持语音输入', 'SPEECH_RECOGNITION_UNAVAILABLE', false, 'renderer'))
  }
  let recognition: SpeechRecognitionLike | undefined
  let stream: MediaStream | undefined
  let context: AudioContext | undefined
  let frame = 0
  let finished = false
  let recSilenceTimer = 0
  let restartTimer = 0
  let assistantPlayback = false
  let bargeInActive = false
  let commitPaused = false
  let bargeVoiceSince = 0
  let recRestarting = false
  let echoGuardUntil = 0
  let echoTimer = 0
  let playbackBargeIn = false
  let playbackApplied = false
  let playbackEchoGuardMs = ECHO_GUARD_MS
  let commitPausedApplied = false
  let bargeInActiveApplied = false
  let lastResultAt = performance.now()
  let lastRecognitionPulseAt = 0
  let lastStartAt = 0
  /** WebView2 often accepts continuous=true then stays mute. Prefer one-shot. */
  let preferContinuous = false
  let keepAliveTimer = 0
  let commitHintTimer = 0
  let voiceEnergyWithoutTextSince = 0
  let recognitionAlive = false
  const recognitionHeld = () => shouldHoldRecognition(assistantPlayback, echoGuardUntil, performance.now(), playbackBargeIn)
  const muteMic = (muted: boolean) => {
    stream?.getAudioTracks().forEach(track => {
      track.enabled = !muted
    })
  }
  const teardown = () => {
    if (frame) cancelAnimationFrame(frame)
    window.clearInterval(keepAliveTimer)
    window.clearTimeout(recSilenceTimer)
    window.clearTimeout(restartTimer)
    window.clearTimeout(echoTimer)
    window.clearTimeout(commitHintTimer)
    stream?.getTracks().forEach(track => track.stop())
    stream = undefined
    void context?.close()
    context = undefined
    callbacks.onLevels?.(Array.from({ length: MOON_RING_BINS }, () => 0))
  }
  return (async () => {
    let constraints = microphoneConstraints()
    const withEcho = (value: MediaStreamConstraints): MediaStreamConstraints => {
      const audio = value.audio
      if (audio === false) return value
      const extra = { echoCancellation: true as const, noiseSuppression: true as const, autoGainControl: true as const }
      return { audio: audio === true || audio == null ? extra : { ...audio, ...extra } }
    }
    constraints = withEcho(constraints)
    let media: MediaStream
    try {
      media = await navigator.mediaDevices.getUserMedia(constraints)
    } catch (error) {
      const name = error instanceof DOMException ? error.name : ''
      if (selectedMicrophoneId() && (name === 'NotFoundError' || name === 'DevicesNotFoundError' || name === 'OverconstrainedError')) {
        saveMicrophoneId('')
        constraints = withEcho({ audio: true })
        media = await navigator.mediaDevices.getUserMedia(constraints)
      } else throw error
    }
    stream = media
    void unlockTtsAudio()
    // Probe permission on the saved device, then release capture so WebView2
    // SpeechRecognition owns WASAPI. Holding getUserMedia makes the level meter
    // move while SR hears silence; analyser-driven restarts then abort every
    // utterance before Windows returns text.
    const deviceId = media.getAudioTracks()[0]?.getSettings()?.deviceId
    if (deviceId) saveMicrophoneId(deviceId)
    media.getTracks().forEach(track => track.stop())
    stream = undefined
    await new Promise<void>(resolve => window.setTimeout(resolve, MIC_RELEASE_BEFORE_SR_MS))
    let finals = ''
    let interim = ''
    let lastVoiceAt = performance.now()
    let lastTextChangeAt = performance.now()
    let firstTextAt = 0
    let utteranceVoiceSince = 0
    let lastStallRestartAt = 0
    let speechActive = false
    const assembled = () => {
      const f = finals.trim()
      const i = interim.trim()
      if (!i) return f
      if (!f) return i
      if (f.includes(i)) return f
      return `${f}${i}`
    }
    const startRecognition = () => {
      if (finished || !recognition || recognitionHeld()) return false
      const tryStart = (continuous: boolean) => {
        recognition!.continuous = continuous
        recognition!.start()
        recognitionAlive = true
        lastStartAt = performance.now()
        lastRecognitionPulseAt = lastStartAt
        voiceEnergyWithoutTextSince = 0
        preferContinuous = continuous
        return true
      }
      try {
        return tryStart(preferContinuous)
      } catch {
        if (recognitionAlive) return true
        try {
          return tryStart(!preferContinuous)
        } catch {
          recognitionAlive = false
          return false
        }
      }
    }
    const restartRecognition = (clearTranscript = true) => {
      if (finished || !recognition) return
      if (clearTranscript) {
        finals = ''
        interim = ''
        bargeVoiceSince = 0
        utteranceVoiceSince = 0
        firstTextAt = 0
        lastTextChangeAt = performance.now()
        lastResultAt = performance.now()
        callbacks.onInterim?.('')
      }
      recRestarting = true
      window.clearTimeout(restartTimer)
      window.clearTimeout(commitHintTimer)
      try {
        recognition.stop()
      } catch {
        /* engine may already be stopped */
      }
      restartTimer = window.setTimeout(() => {
        recRestarting = false
        if (finished || !recognition || recognitionHeld()) return
        startRecognition()
      }, 60)
    }
    const restartIfStalled = () => {
      if (finished || recRestarting || recognitionHeld() || assembled().trim() || speechActive) return
      const now = performance.now()
      if (!shouldRestartStalledRecognition({
        speechActive,
        hasText: false,
        held: recognitionHeld(),
        restarting: recRestarting,
        msSinceStart: now - lastStartAt,
      })) return
      if (now - lastStallRestartAt < STALL_RESTART_GAP_MS) return
      lastStallRestartAt = now
      if (!recognitionAlive) {
        if (!startRecognition()) restartRecognition(false)
        return
      }
      restartRecognition(false)
    }
    const scheduleRecognitionAfterGuard = () => {
      window.clearTimeout(restartTimer)
      const wait = Math.max(20, echoGuardUntil - performance.now() + 20)
      restartTimer = window.setTimeout(() => {
        if (finished || recognitionHeld()) return
        if (!assembled().trim()) restartRecognition()
        else tryCommitFromSilence(lastVoiceAt)
      }, wait)
    }
    const scheduleFinalCommit = (text: string, delayMs = 100) => {
      window.clearTimeout(commitHintTimer)
      commitHintTimer = window.setTimeout(() => {
        if (finished || commitPaused || recognitionHeld()) return
        const current = assembled().trim()
        if (!current) return
        if (current === text || text.includes(current) || current.includes(text)) commit(current)
      }, delayMs)
    }
    const commit = (text: string) => {
      if (finished || !text || recognitionHeld() || assistantPlayback) return
      window.clearTimeout(commitHintTimer)
      if (duplex) {
        callbacks.onFinal(text)
        restartRecognition(true)
        return
      }
      finished = true
      recognition?.stop()
      teardown()
      callbacks.onFinal(text)
    }
    const tryBargeIn = (rawPeak: number) => {
      if (finished || !duplex || !bargeIn || !bargeInActive || !callbacks.onBargeIn) return
      const peak = rawPeak / 255
      if (assistantPlayback) {
        if (!playbackBargeIn) {
          bargeVoiceSince = 0
          return
        }
        if (peak >= BARGE_IN_PEAK) {
          if (!bargeVoiceSince) bargeVoiceSince = performance.now()
          else if (shouldBargeInDuringPlayback(peak, performance.now() - bargeVoiceSince)) {
            bargeVoiceSince = 0
            callbacks.onBargeIn(assembled().trim() || '')
            restartRecognition()
          }
        } else bargeVoiceSince = 0
        return
      }
      if (recognitionHeld()) {
        bargeVoiceSince = 0
        return
      }
      const text = assembled()
      if (!text) return
      if (peak >= profile.voicePeak + profile.bargeInPeakDelta) {
        if (!bargeVoiceSince) bargeVoiceSince = performance.now()
        else if (shouldBargeIn(true, performance.now() - bargeVoiceSince)) {
          bargeVoiceSince = 0
          callbacks.onBargeIn(text)
          restartRecognition()
        }
      } else bargeVoiceSince = 0
    }
    const tryCommitFromSilence = (voiceAt: number) => {
      if (finished || commitPaused || recognitionHeld() || assistantPlayback) return
      const text = assembled().trim()
      if (!text) return
      const textSince = firstTextAt ? performance.now() - firstTextAt : 0
      if (shouldDeferCommit(text, textSince)) return
      const { stableMs, silenceMs } = endpointingForText(text, profile)
      const silentFor = performance.now() - voiceAt
      const stableFor = performance.now() - lastTextChangeAt
      const punctuated = /[。？！?!…]$/.test(text)
      const stableTarget = punctuated ? Math.min(stableMs, 180) : stableMs
      if (shouldCommitStable(true, stableFor, stableTarget)) {
        commit(text)
        return
      }
      if (shouldCommitUtterance(true, silentFor, silenceMs)) commit(text)
    }
    const paintLevels = () => {
      const t = performance.now() / 160
      const speaking = speechActive || !!assembled().trim()
      const levels = Array.from({ length: MOON_RING_BINS }, (_, index) => {
        if (!speaking) return idleMeterLevel(t, index)
        return 0.28 + 0.55 * Math.abs(Math.sin(t * 1.7 + index * 0.7))
      })
      callbacks.onLevels?.(levels)
      frame = requestAnimationFrame(paintLevels)
    }
    paintLevels()
    const rec: SpeechRecognitionLike = new Recognition()
    recognition = rec
    rec.lang = companionRecognitionLang()
    rec.continuous = false
    rec.interimResults = true
    rec.maxAlternatives = 1
    const markSpeech = () => {
      if (finished) return
      speechActive = true
      voiceEnergyWithoutTextSince = voiceEnergyWithoutTextSince || performance.now()
      lastVoiceAt = performance.now()
      callbacks.onSpeechStart?.()
      callbacks.onVoiceEnergy?.()
    }
    rec.onsoundstart = markSpeech
    rec.onspeechstart = markSpeech
    rec.onspeechend = () => {
      speechActive = false
      tryCommitFromSilence(lastVoiceAt)
    }
    rec.onaudiostart = () => {
      recognitionAlive = true
      // Audio graph is live — treat as hearing even before the first token
      // so quiet speech still lights the caption strip.
      callbacks.onVoiceEnergy?.()
    }
    rec.onresult = event => {
      const held = recognitionHeld()
      const before = assembled()
      for (let i = event.resultIndex; i < event.results.length; i++) {
        const piece = event.results[i][0].transcript
        if (event.results[i].isFinal) {
          finals += piece
          interim = ''
        } else {
          interim = piece
        }
      }
      const now = performance.now()
      const next = assembled().trim()
      if (next) {
        lastResultAt = now
        voiceEnergyWithoutTextSince = 0
        if (!utteranceVoiceSince) utteranceVoiceSince = now
      }
      if (next !== before.trim()) {
        lastTextChangeAt = now
        if (!firstTextAt) firstTextAt = now
      }
      if (assistantPlayback) return
      if (next) callbacks.onInterim?.(next)
      else if (!held) callbacks.onInterim?.('')
      if (held && !playbackBargeIn) return
      if (next && /[。？！?!…]$/.test(next)) scheduleFinalCommit(next, 160)
      if (event.results[event.results.length - 1]?.isFinal && next) scheduleFinalCommit(next, 80)
      window.clearTimeout(recSilenceTimer)
      if (next && !commitPaused) {
        const { stableMs } = endpointingForText(next, profile)
        recSilenceTimer = window.setTimeout(() => tryCommitFromSilence(lastVoiceAt), stableMs)
      }
      if (!commitPaused) tryCommitFromSilence(lastVoiceAt)
    }
    rec.onerror = event => {
      if (finished) return
      if (event?.error === 'aborted' || recRestarting) return
      const hint = speechEngineHint(event?.error)
      if (hint) callbacks.onEngineHint?.(hint)
      if (isPermanentSpeechError(event?.error)) {
        finished = true
        teardown()
        const denied = event?.error === 'not-allowed'
        const serviceDisabled = event?.error === 'service-not-allowed'
        callbacks.onError(
          new BridgeClientError(
            denied ? '语音识别服务拒绝访问，请检查 Windows 在线语音识别设置' : serviceDisabled ? 'Windows 在线语音识别服务未启用或不可用' : '当前语言不受语音识别支持',
            denied ? 'SPEECH_SERVICE_PERMISSION_DENIED' : serviceDisabled ? 'SPEECH_SERVICE_DISABLED' : 'SPEECH_RECOGNITION_FAILED',
            false,
            'renderer',
          ),
        )
        return
      }
      if (duplex && (event?.error === 'no-speech' || event?.error === 'network' || event?.error === 'audio-capture')) {
        window.clearTimeout(restartTimer)
        restartTimer = window.setTimeout(() => {
          if (finished || !recognition || recognitionHeld()) return
          restartRecognition(false)
        }, event?.error === 'no-speech' ? 180 : 420)
        return
      }
      if (duplex) return
      finished = true
      teardown()
      callbacks.onEndWithoutFinal?.()
    }
    rec.onend = () => {
      recognitionAlive = false
      if (finished) return
      if (recRestarting) return
      const text = assembled().trim()
      if (text && !recognitionHeld()) {
        commit(text)
        return
      }
      if (recognitionHeld()) {
        scheduleRecognitionAfterGuard()
        return
      }
      if (duplex) {
        restartTimer = window.setTimeout(() => {
          if (finished || !recognition || recognitionHeld()) return
          if (!startRecognition()) restartRecognition(false)
        }, 80)
        return
      }
      finished = true
      teardown()
      callbacks.onEndWithoutFinal?.()
    }
    window.setTimeout(() => {
      if (!finished) startRecognition()
    }, 40)
    keepAliveTimer = window.setInterval(() => {
      if (finished || recognitionHeld() || recRestarting) return
      const text = assembled().trim()
      const stableFor = performance.now() - lastTextChangeAt
      if (text) {
        if (stableFor >= STUCK_TRANSCRIPT_MS) commit(text)
        return
      }
      if (speechActive) return
      restartIfStalled()
    }, 400)
    return {
      stop: () => {
        if (finished) return
        finished = true
        window.clearInterval(keepAliveTimer)
        recognition?.stop()
        teardown()
      },
      setAssistantPlayback: (active: boolean, echoGuardMs = ECHO_GUARD_MS) => {
        if (active === playbackApplied && (active || echoGuardMs === playbackEchoGuardMs)) return
        playbackApplied = active
        playbackEchoGuardMs = echoGuardMs
        assistantPlayback = active
        // Never arm playback barge-in. Laptop speaker + built-in mic cannot
        // reliably tell TTS echo from the user (browser AEC still leaks the
        // same-language female voice). She talks to the end; click the moon
        // to stop. After playback, echo-guard then listen.
        playbackBargeIn = false
        window.clearTimeout(echoTimer)
        bargeVoiceSince = 0
        if (active) {
          finals = ''
          interim = ''
          firstTextAt = 0
          lastTextChangeAt = performance.now()
          callbacks.onInterim?.('')
          muteMic(true)
          recRestarting = true
          try {
            recognition?.stop()
          } catch {
            /* already stopped */
          }
          return
        }
        playbackBargeIn = false
        muteMic(false)
        echoGuardUntil = performance.now() + Math.max(0, echoGuardMs)
        echoTimer = window.setTimeout(() => {
          if (finished || assistantPlayback) return
          if (!recognitionAlive) restartRecognition()
        }, Math.max(0, echoGuardMs))
      },
      setCommitPaused: (paused: boolean) => {
        if (paused === commitPausedApplied) return
        commitPausedApplied = paused
        commitPaused = paused
      },
      setBargeInActive: (active: boolean) => {
        if (active === bargeInActiveApplied) return
        bargeInActiveApplied = active
        bargeInActive = active
        if (!active) bargeVoiceSince = 0
      },
      pulseRecognition: () => {
        if (finished || recognitionHeld() || recRestarting) return
        const text = assembled().trim()
        if (text) {
          commit(text)
          return
        }
        if (!shouldRestartStalledRecognition({
          speechActive,
          hasText: false,
          held: recognitionHeld(),
          restarting: recRestarting,
          msSinceStart: performance.now() - lastStartAt,
        })) return
        lastRecognitionPulseAt = performance.now()
        restartRecognition(false)
      },
      forceCommit: () => {
        const text = assembled().trim()
        if (text) commit(text)
      },
    }
  })().catch(error => {
    teardown()
    const name = error instanceof DOMException ? error.name : ''
    const denied = name === 'NotAllowedError' || name === 'SecurityError'
    const missing = name === 'NotFoundError' || name === 'DevicesNotFoundError' || name === 'OverconstrainedError'
    const busyDevice = name === 'NotReadableError' || name === 'TrackStartError'
    throw new BridgeClientError(
      denied ? '麦克风权限被拒绝，请允许桌面应用访问麦克风' : missing ? '未检测到可用麦克风，请在“设置 → 语音与麦克风”中检查设备' : busyDevice ? '麦克风被其他应用占用或驱动无法启动' : '无法启动麦克风，请检查设备设置',
      denied ? 'MICROPHONE_PERMISSION_DENIED' : missing ? 'MICROPHONE_DEVICE_NOT_FOUND' : busyDevice ? 'MICROPHONE_DEVICE_BUSY' : 'MICROPHONE_START_FAILED',
      false,
      'renderer',
    )
  })
}
