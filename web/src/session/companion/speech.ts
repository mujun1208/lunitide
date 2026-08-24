// speech.ts is the M9.5 companion voice input (T-9.5.3.1): it reuses
// the toggleSpeech pipeline (getUserMedia with the saved microphone,
// zh-CN final-only SpeechRecognition, analyser-driven levels, and the
// frozen microphone/speech error codes) but routes each final
// transcript straight to ChatBridge instead of the composer.
import { BridgeClientError } from '../../bridge/client'
import { microphoneConstraints, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { MOON_RING_BINS } from './MoonSphere'
import { looksIncompleteUtterance, looksLikePlaybackEcho } from './companionText'
import { sharedTtsAudioContext, unlockTtsAudio } from './ttsPlayer'

type SpeechRecognitionHypothesis = { transcript: string; confidence?: number }
type SpeechRecognitionResultLike = { 0: SpeechRecognitionHypothesis; length: number; isFinal: boolean } & Record<number, SpeechRecognitionHypothesis>
type SpeechRecognitionEventLike = { resultIndex: number; results: ArrayLike<SpeechRecognitionResultLike> }
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
  /** The user talked over her: cut the reply and start their turn. */
  onBargeIn?: (transcript: string) => void
  /** Text currently being spoken aloud — used to tell echo from the user. */
  spokenText?: () => string
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
  /** Assistant TTS active — mute the mic, keep recognition warm, ignore output. */
  setAssistantPlayback: (active: boolean, echoGuardMs?: number) => void
  /** Pause silence-based commit while thinking/speaking. */
  setCommitPaused: (paused: boolean) => void
  /** No-op when false (default). Automatic voice barge-in stays off. */
  setBargeInActive: (active: boolean) => void
  /** Restart the recognizer if it stalled (listening with no transcript). */
  pulseRecognition: () => void
  /** Force-send the current assembled transcript (fallback when endpointing stalls). */
  forceCommit: () => void
  /** After a user gesture: resume Web Audio and (re)start recognition if it is dead. */
  resumeCapture: () => void
}

/** Commit after this much analyser silence once we already have a complete phrase.
 *  Fast after a real stop (~0.3s), still WeChat-like: never slice mid-clause.
 *  End-of-speech → send stays well under the 2–3s user cap. */
export const UTTERANCE_SILENCE_MS = 280
/** Commit when complete interim/final text stops changing. */
export const UTTERANCE_STABLE_MS = 220
/** Incomplete-looking phrases (mid-command tails) wait for the rest of the sentence. */
export const INCOMPLETE_STABLE_MS = 900
/** Incomplete phrases also tolerate a longer trailing pause before commit. */
export const INCOMPLETE_SILENCE_MS = 900
/** After Windows isFinal on a fragment, wait this long for more tokens before commit. */
export const INCOMPLETE_HOLD_MS = 1600
/** Hard ceiling: if Windows never sends the rest of the sentence, stop waiting. */
export const INCOMPLETE_HARD_MS = 2200
/** Minimum time with text on screen before we accept a non-terminal commit. */
export const MIN_UTTERANCE_MS = 140
/** Force-commit only after the transcript AND the mic have been quiet. Ceiling for “I stopped talking”. */
export const STUCK_TRANSCRIPT_MS = 1800
/** Restart SR only after this long of real mic energy with no transcript. */
export const VOICE_WITHOUT_TEXT_MS = 1200
/** How long since the last SR token before a voice-energy restart. */
export const VOICE_RESTART_RESULT_MS = 2500
/** Minimum gap between stall-recovery restarts (avoids WebView SR crashes). */
export const STALL_RESTART_GAP_MS = 1600
/** Recycle a silent WebView SR session only after Windows has had time to
 *  return the first token. Restarting at ~2s aborted in-flight recognition
 *  and made every utterance look like “听不到”. Never stop() while
 *  speechActive or text is already on screen. */
export const STALL_RESTART_AFTER_MS = 8000
/** After TTS, a zombie Windows SR session must recycle quickly. */
export const STALL_RESTART_AFTER_PLAYBACK_MS = 500

/** Energy above this (0–1 peak) counts as voice for endpointing.
 *  Low enough that quiet speech keeps the utterance open; idle rings stay below. */
export const VOICE_PEAK = 0.03
/** Stronger peak while assistant speaks — avoids TTS bleed despite AEC. */
export const BARGE_IN_PEAK = 0.16

export function speechProfile(environment: SpeechEnvironment = 'normal'): SpeechProfile {
  if (environment === 'noisy') {
    return {
      voicePeak: 0.08,
      utteranceSilenceMs: 100,
      utteranceStableMs: 120,
      bargeInPeakDelta: 0.04,
      minVoiceHoldMs: 50,
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
export const ECHO_GUARD_MS = 90
/** After a click interrupt, unmute quickly so the user can talk. */
export const INTERRUPT_ECHO_MS = 80

/** Sustained loud voice during assistant TTS playback (energy-only barge-in). */
export const BARGE_IN_PLAYBACK_HOLD_MS = 80

/** Recognized characters needed to cut in while she is speaking. */
export const BARGE_IN_MIN_CHARS = 2
/** She is only thinking (no audio out): ask for a longer utterance so the
 *  late tail of the sentence we just sent cannot restart the turn. */
export const BARGE_IN_THINKING_MIN_CHARS = 4
/** Ignore transcripts for this long after a barge-in fired. */
export const BARGE_IN_GUARD_MS = 800
/** Let the just-committed utterance settle before a new one may replace it. */
export const BARGE_IN_SETTLE_MS = 1200

/**
 * True when what the recognizer just heard is the user talking over the
 * assistant rather than the assistant's own voice returning through the
 * speaker. `spoken` is the text currently being played aloud.
 */
export function shouldBargeInOverPlayback(heard: string, spoken: string, minChars = BARGE_IN_MIN_CHARS): boolean {
  const text = heard.trim()
  if (!text) return false
  if (looksLikePlaybackEcho(text, spoken)) return false
  return Array.from(text).length >= minChars
}

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

/** Incomplete fragments (“你可以”, “合肥的”) must wait for more tokens.
 *  Windows isFinal + speechend is not end-of-speech. */
export function shouldCommitIncomplete(input: {
  silentForMs: number
  silenceMs: number
  msSinceLastResult: number
  speechActive: boolean
  holdMs?: number
  hardMs?: number
}): boolean {
  const holdMs = input.holdMs ?? INCOMPLETE_HOLD_MS
  const hardMs = input.hardMs ?? INCOMPLETE_HARD_MS
  if (input.msSinceLastResult < holdMs) return false
  if (input.speechActive && input.msSinceLastResult < hardMs) return false
  return input.silentForMs >= input.silenceMs || input.msSinceLastResult >= hardMs
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

/** Prefer a longer interim overlay so Windows revisions replace, not duplicate, the caption. */
export function overlayTranscript(finals: string, interim: string): string {
  const f = finals.trim()
  const i = interim.trim()
  if (!i) return f
  if (!f) return i
  if (i.includes(f) || i.startsWith(f)) return i
  if (f.includes(i)) return f
  return `${f}${i}`
}

/** Pick the highest-confidence alternative (WeChat-like matching). */
export function pickRecognitionTranscript(result: { length: number; [index: number]: { transcript: string; confidence?: number } | undefined }): string {
  let best = result[0]
  if (!best) return ''
  const n = result.length ?? 1
  for (let i = 1; i < n; i++) {
    const alt = result[i]
    if (alt && (alt.confidence ?? 0) > (best.confidence ?? 0)) best = alt
  }
  return best.transcript ?? ''
}

/** Idle moon rings must stay below VOICE_PEAK so they never look like speech. */
export function idleMeterLevel(t: number, index: number): number {
  return 0.012 + 0.014 * Math.abs(Math.sin(t * 0.6 + index * 0.45))
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

/** Windows setup copy is only for a first listen that never produced text.
 *  After a successful turn, sitting in “listening” is normal — do not
 *  blame the OS for silence. */
export function shouldShowSpeechSetupHint(input: {
  listening: boolean
  hasInterim: boolean
  listenSeconds: number
  heardThisVisit: boolean
  hasUserRound: boolean
}): boolean {
  if (!input.listening || input.hasInterim) return false
  if (input.heardThisVisit || input.hasUserRound) return false
  return input.listenSeconds >= 20
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
  let meterStream: MediaStream | undefined
  let micSource: MediaStreamAudioSourceNode | undefined
  let context: AudioContext | undefined
  let ownsContext = false
  let analyser: AnalyserNode | undefined
  let spectrum: Uint8Array<ArrayBuffer> | undefined
  let waveform: Uint8Array<ArrayBuffer> | undefined
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
  /** Same as the chat composer: continuous listen so a sentence is not cut. */
  let preferContinuous = true
  let keepAliveTimer = 0
  let commitHintTimer = 0
  let voiceEnergyWithoutTextSince = 0
  let recognitionAlive = false
  const recognitionHeld = () => shouldHoldRecognition(assistantPlayback, echoGuardUntil, performance.now(), playbackBargeIn)
  let recycleAfterPlayback = false
  let lastPlaybackEndedAt = 0
  const muteMic = (muted: boolean) => {
    stream?.getAudioTracks().forEach(track => {
      track.enabled = !muted
    })
    meterStream?.getAudioTracks().forEach(track => {
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
    meterStream?.getTracks().forEach(track => track.stop())
    meterStream = undefined
    micSource?.disconnect()
    micSource = undefined
    stream?.getTracks().forEach(track => track.stop())
    stream = undefined
    if (ownsContext) void context?.close()
    context = undefined
    ownsContext = false
    callbacks.onLevels?.(Array.from({ length: MOON_RING_BINS }, () => 0))
  }
  return (async () => {
    let constraints = microphoneConstraints()
    const withEcho = (value: MediaStreamConstraints): MediaStreamConstraints => {
      const audio = value.audio
      if (audio === false) return value
      // Keep AEC (TTS later). Do not gate the first words with noise
      // suppression — it makes quiet first-round speech late or missing.
      const extra = { echoCancellation: true as const, noiseSuppression: false as const, autoGainControl: true as const }
      return { audio: audio === true || audio == null ? extra : { ...audio, ...extra } }
    }
    constraints = withEcho(constraints)
    void unlockTtsAudio()
    const gumPromise = (async (): Promise<MediaStream> => {
      try {
        return await navigator.mediaDevices.getUserMedia(constraints)
      } catch (error) {
        const name = error instanceof DOMException ? error.name : ''
        if (selectedMicrophoneId() && (name === 'NotFoundError' || name === 'DevicesNotFoundError' || name === 'OverconstrainedError')) {
          saveMicrophoneId('')
          return await navigator.mediaDevices.getUserMedia(withEcho({ audio: true }))
        }
        throw error
      }
    })()
    // Analyser is display-only. Start SpeechRecognition in this turn — before
    // awaiting getUserMedia — so the first utterance is not a 1–3s blank listen.
    let finals = ''
    let interim = ''
    let lastVoiceAt = performance.now()
    let lastTextChangeAt = performance.now()
    let firstTextAt = 0
    let utteranceVoiceSince = 0
    let lastStallRestartAt = 0
    let speechActive = false
    const assembled = () => overlayTranscript(finals, interim)
    const recognitionAlreadyRunning = (error: unknown) =>
      (error instanceof DOMException && error.name === 'InvalidStateError') ||
      (error instanceof Error && /already started|invalid state/i.test(error.message))
    const startRecognition = () => {
      // Echo-hold blocks commit, never a warm start — first tokens must paint
      // the moment Windows SR returns them. Keep the session alive during TTS
      // (mic is muted) so the next user turn is not a 0.5–2s cold start.
      if (finished || !recognition) return false
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
      } catch (error) {
        if (recognitionAlreadyRunning(error)) {
          recognitionAlive = true
          return true
        }
        recognitionAlive = false
        try {
          return tryStart(!preferContinuous)
        } catch (retryError) {
          if (recognitionAlreadyRunning(retryError)) {
            recognitionAlive = true
            return true
          }
          recognitionAlive = false
          return false
        }
      }
    }
    const ensureRecognitionRunning = (clearTranscript = false) => {
      recRestarting = false
      if (recognitionAlive) return
      if (!startRecognition()) restartRecognition(clearTranscript)
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
        if (finished || !recognition) return
        if (!assistantPlayback && recognitionHeld()) return
        startRecognition()
      }, 60)
    }
    const restartIfStalled = () => {
      if (finished || assistantPlayback || recRestarting || recognitionHeld() || assembled().trim() || speechActive) return
      const now = performance.now()
      const recentlyAfterPlayback = lastPlaybackEndedAt > 0 && now - lastPlaybackEndedAt < 15_000
      if (!shouldRestartStalledRecognition({
        speechActive,
        hasText: false,
        held: recognitionHeld(),
        restarting: recRestarting,
        msSinceStart: now - lastStartAt,
        minSessionMs: recentlyAfterPlayback ? STALL_RESTART_AFTER_PLAYBACK_MS : STALL_RESTART_AFTER_MS,
      })) return
      if (now - lastStallRestartAt < STALL_RESTART_GAP_MS) return
      lastStallRestartAt = now
      if (!recognitionAlive) {
        ensureRecognitionRunning(false)
        return
      }
      // A living session after TTS is warm, not a zombie. Recycling it
      // at 500ms aborts the user's first words on the next turn.
      if (recentlyAfterPlayback) return
      restartRecognition(false)
    }
    const scheduleRecognitionAfterGuard = () => {
      window.clearTimeout(restartTimer)
      const wait = Math.max(20, echoGuardUntil - performance.now() + 20)
      restartTimer = window.setTimeout(() => {
        if (finished || recognitionHeld()) return
        if (recognitionAlive) return
        if (!assembled().trim()) ensureRecognitionRunning()
        else tryCommitFromSilence(lastVoiceAt)
      }, wait)
    }
    const scheduleFinalCommit = (text: string, delayMs = 100) => {
      window.clearTimeout(commitHintTimer)
      commitHintTimer = window.setTimeout(() => {
        if (finished || commitPaused || recognitionHeld()) return
        const current = assembled().trim()
        if (!current) return
        // Windows marks phrase fragments isFinal ("你可以"). Never fast-commit those.
        if (looksIncompleteUtterance(current)) return
        if (current === text || text.includes(current) || current.includes(text)) commit(current)
      }, delayMs)
    }
    let lastCommittedCompact = ''
    let lastCommittedAt = 0
    let bargeGuardUntil = 0
    const compactCommit = (value: string) => value.replace(/\s/g, '')
    const commit = (text: string) => {
      if (finished || !text || recognitionHeld() || assistantPlayback) return
      const compact = compactCommit(text)
      const now = performance.now()
      if (compact === lastCommittedCompact && now - lastCommittedAt < 1500) return
      window.clearTimeout(commitHintTimer)
      lastCommittedCompact = compact
      lastCommittedAt = now
      if (duplex) {
        callbacks.onFinal(text)
        finals = ''
        interim = ''
        bargeVoiceSince = 0
        utteranceVoiceSince = 0
        firstTextAt = 0
        lastTextChangeAt = performance.now()
        lastResultAt = performance.now()
        callbacks.onInterim?.('')
        // Keep the Windows session warm for every later turn.
        return
      }
      finished = true
      recognition?.stop()
      teardown()
      callbacks.onFinal(text)
    }
    /**
     * She is thinking or speaking and the user starts talking again. Real
     * conversation lets that cut in. Two guards keep it honest: her own
     * voice coming back through the speaker is dropped as echo, and the
     * late tail of the sentence we just sent can never restart the turn.
     */
    const maybeInterruptTurn = (raw: string) => {
      if (!callbacks.onBargeIn) return
      const text = raw.trim()
      if (!text) return
      const now = performance.now()
      if (now < bargeGuardUntil) return
      if (assistantPlayback && !shouldBargeInOverPlayback(text, callbacks.spokenText?.() ?? '')) {
        // Speaker bleed: drop it so echo never accumulates into a turn.
        if (looksLikePlaybackEcho(text, callbacks.spokenText?.() ?? '')) {
          finals = ''
          interim = ''
        }
        return
      }
      if (!assistantPlayback) {
        if (Array.from(text).length < BARGE_IN_THINKING_MIN_CHARS) return
        if (now - lastCommittedAt < BARGE_IN_SETTLE_MS) return
      }
      const compact = compactCommit(text)
      if (lastCommittedCompact && (compact.includes(lastCommittedCompact) || lastCommittedCompact.includes(compact))) return
      bargeGuardUntil = now + BARGE_IN_GUARD_MS
      finals = ''
      interim = ''
      callbacks.onBargeIn(text)
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
      const incomplete = looksIncompleteUtterance(text)
      // Windows often isFinal + speechend on「你可以」/「合肥的」while the user
      // is still talking. Hold until tokens go stale and the mic is quiet.
      if (incomplete) {
        const staleFor = performance.now() - lastResultAt
        if (
          speechActive &&
          staleFor >= INCOMPLETE_HOLD_MS &&
          staleFor < INCOMPLETE_HARD_MS &&
          performance.now() - lastStallRestartAt >= STALL_RESTART_GAP_MS
        ) {
          lastStallRestartAt = performance.now()
          restartRecognition(false)
          return
        }
        if (
          shouldCommitIncomplete({
            silentForMs: silentFor,
            silenceMs,
            msSinceLastResult: staleFor,
            speechActive,
          })
        ) {
          commit(text)
        }
        return
      }
      const punctuated = /[。？！?!…]$/.test(text)
      const stableTarget = punctuated ? Math.min(stableMs, 120) : stableMs
      if (shouldCommitStable(true, stableFor, stableTarget)) {
        commit(text)
        return
      }
      if (shouldCommitUtterance(true, silentFor, silenceMs)) commit(text)
    }
    const paintLevels = () => {
      if (context?.state === 'suspended') void context.resume()
      const t = performance.now() / 160
      let peak = 0
      let levels: number[]
      const wave = waveform
      const spec = spectrum
      if (analyser && wave && spec) {
        analyser.getByteTimeDomainData(wave)
        analyser.getByteFrequencyData(spec)
        for (let i = 0; i < wave.length; i++) peak = Math.max(peak, Math.abs(wave[i]! - 128))
        const energyNow = peak / 128
        const bucket = Math.max(1, Math.floor(spec.length / MOON_RING_BINS))
        levels = Array.from({ length: MOON_RING_BINS }, (_, index) => {
          let bin = 0
          for (let i = index * bucket; i < Math.min(spec.length, (index + 1) * bucket); i++) bin = Math.max(bin, spec[i]!)
          const mag = Math.max(bin / 255, energyNow)
          if (mag < 0.02) return 0.05
          return Math.min(1, 0.12 + Math.pow(mag, 0.55) * 0.88)
        })
      } else {
        levels = Array.from({ length: MOON_RING_BINS }, (_, index) => idleMeterLevel(t, index))
      }
      const energy = peak / 128
      const now = performance.now()
      if (energy >= profile.voicePeak) {
        speechActive = true
        lastVoiceAt = now
        callbacks.onVoiceEnergy?.()
        if (!assembled().trim()) voiceEnergyWithoutTextSince = voiceEnergyWithoutTextSince || now
        else voiceEnergyWithoutTextSince = 0
      } else if (now - lastVoiceAt > profile.utteranceSilenceMs) {
        speechActive = false
        voiceEnergyWithoutTextSince = 0
      }
      callbacks.onLevels?.(levels)
      tryBargeIn(peak * 2)
      if (
        duplex &&
        !assistantPlayback &&
        !recognitionAlive &&
        energy >= profile.voicePeak &&
        !assembled().trim() &&
        voiceEnergyWithoutTextSince &&
        now - voiceEnergyWithoutTextSince >= VOICE_WITHOUT_TEXT_MS &&
        now - lastResultAt >= VOICE_RESTART_RESULT_MS
      ) {
        voiceEnergyWithoutTextSince = now
        restartRecognition(false)
      }
      tryCommitFromSilence(lastVoiceAt)
      restartIfStalled()
      frame = requestAnimationFrame(paintLevels)
    }
    const rec: SpeechRecognitionLike = new Recognition()
    recognition = rec
    rec.lang = companionRecognitionLang()
    rec.continuous = true
    rec.interimResults = true
    rec.maxAlternatives = 3
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
      // Do not clear speechActive or commit: Windows fires speechend at
      // clause boundaries ("你可以") while the user is still speaking.
    }
    rec.onaudiostart = () => {
      recognitionAlive = true
    }
    rec.onresult = event => {
      const held = recognitionHeld()
      const before = assembled()
      for (let i = event.resultIndex; i < event.results.length; i++) {
        const piece = pickRecognitionTranscript(event.results[i])
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
        lastVoiceAt = now
        speechActive = true
        voiceEnergyWithoutTextSince = 0
        if (!utteranceVoiceSince) utteranceVoiceSince = now
      }
      if (next !== before.trim()) {
        lastTextChangeAt = now
        if (!firstTextAt) firstTextAt = now
      }
      if (assistantPlayback) {
        maybeInterruptTurn(next)
        return
      }
      if (next) callbacks.onInterim?.(next)
      else if (!held) callbacks.onInterim?.('')
      if (held && !playbackBargeIn) return
      if (commitPaused) {
        maybeInterruptTurn(next)
        return
      }
      if (next && !looksIncompleteUtterance(next)) {
        if (/[。？！?!…]$/.test(next)) scheduleFinalCommit(next, 50)
        else if (event.results[event.results.length - 1]?.isFinal) scheduleFinalCommit(next, 100)
      }
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
          if (finished || !recognition) return
          if (assistantPlayback) {
            if (!recognitionAlive) startRecognition()
            return
          }
          if (recognitionHeld()) return
          // stop() during an in-flight utterance drops the rest of the sentence.
          if (event?.error === 'no-speech' && assembled().trim()) return
          restartRecognition(false)
        }, event?.error === 'no-speech' ? (assistantPlayback ? 40 : 300) : 420)
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
      // Windows often stops between clauses ("你可以帮我" then the rest).
      // Never commit on engine stop — keep the hypothesis and wait for more,
      // or for silence/stable endpointing.
      if (assistantPlayback) {
        startRecognition()
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
        }, 40)
        return
      }
      const text = assembled().trim()
      if (text) {
        if (looksIncompleteUtterance(text)) return
        commit(text)
        return
      }
      finished = true
      teardown()
      callbacks.onEndWithoutFinal?.()
    }
    startRecognition()
    let media: MediaStream
    try {
      media = await gumPromise
    } catch (error) {
      finished = true
      try {
        recognition?.stop()
      } catch {
        /* engine may already be stopped */
      }
      teardown()
      throw error
    }
    stream = media
    media.getAudioTracks().forEach(track => {
      track.enabled = true
    })
    const deviceId = media.getAudioTracks()[0]?.getSettings()?.deviceId
    if (deviceId) saveMicrophoneId(deviceId)
    const AudioContextClass =
      window.AudioContext ?? (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    const shared = sharedTtsAudioContext()
    if (shared) {
      context = shared
      ownsContext = false
    } else if (AudioContextClass) {
      context = new AudioContextClass()
      ownsContext = true
    }
    if (context) {
      if (context.state === 'suspended') void context.resume().catch(() => undefined)
      analyser = context.createAnalyser()
      analyser.fftSize = 256
      analyser.smoothingTimeConstant = 0.5
      spectrum = new Uint8Array(new ArrayBuffer(analyser.frequencyBinCount))
      waveform = new Uint8Array(new ArrayBuffer(analyser.fftSize))
      try {
        meterStream = new MediaStream(media.getAudioTracks().map(track => track.clone()))
      } catch {
        meterStream = media
      }
      micSource = context.createMediaStreamSource(meterStream)
      micSource.connect(analyser)
    }
    paintLevels()
    keepAliveTimer = window.setInterval(() => {
      if (finished || recRestarting) return
      if (assistantPlayback) {
        if (!recognitionAlive) startRecognition()
        return
      }
      if (recognitionHeld()) return
      const text = assembled().trim()
      const stableFor = performance.now() - lastTextChangeAt
      if (text) {
        if (looksIncompleteUtterance(text)) {
          tryCommitFromSilence(lastVoiceAt)
          return
        }
        if (!speechActive && stableFor >= STUCK_TRANSCRIPT_MS) commit(text)
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
        // The mic stays hot while she speaks so the user can cut in like on
        // a real call. Energy alone cannot tell her voice from theirs
        // (laptop speaker + built-in mic, same-language female voice), so
        // the decision is made on the transcript: anything matching what we
        // are playing is dropped as echo, anything else interrupts.
        playbackBargeIn = active && !!callbacks.onBargeIn
        window.clearTimeout(echoTimer)
        bargeVoiceSince = 0
        if (active) {
          recycleAfterPlayback = false
          finals = ''
          interim = ''
          firstTextAt = 0
          lastTextChangeAt = performance.now()
          callbacks.onInterim?.('')
          muteMic(!playbackBargeIn)
          recRestarting = false
          // Keep the recognizer warm. Stopping it here forces a cold Windows
          // SR start after every reply (0.5–2s before the next caption).
          // The mic is muted; onresult is ignored while assistantPlayback.
          return
        }
        playbackBargeIn = false
        lastPlaybackEndedAt = performance.now()
        muteMic(false)
        echoGuardUntil = performance.now() + Math.max(0, echoGuardMs)
        recycleAfterPlayback = false
        ensureRecognitionRunning(false)
        echoTimer = window.setTimeout(() => {
          if (finished || assistantPlayback) return
          ensureRecognitionRunning(false)
        }, Math.max(0, echoGuardMs))
      },
      setCommitPaused: (paused: boolean) => {
        if (paused === commitPausedApplied) return
        commitPausedApplied = paused
        commitPaused = paused
        if (paused) {
          finals = ''
          interim = ''
          firstTextAt = 0
          lastTextChangeAt = performance.now()
          callbacks.onInterim?.('')
        }
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
          if (looksIncompleteUtterance(text)) return
          commit(text)
          return
        }
        if (!shouldRestartStalledRecognition({
          speechActive,
          hasText: false,
          held: recognitionHeld(),
          restarting: recRestarting,
          msSinceStart: performance.now() - lastStartAt,
          minSessionMs: lastPlaybackEndedAt > 0 && performance.now() - lastPlaybackEndedAt < 15_000
            ? STALL_RESTART_AFTER_PLAYBACK_MS
            : STALL_RESTART_AFTER_MS,
        })) return
        if (recognitionAlive && lastPlaybackEndedAt > 0 && performance.now() - lastPlaybackEndedAt < 15_000) return
        lastRecognitionPulseAt = performance.now()
        restartRecognition(false)
      },
      forceCommit: () => {
        if (speechActive || assistantPlayback || commitPaused) return
        const text = assembled().trim()
        if (!text) return
        if (looksIncompleteUtterance(text)) return
        commit(text)
      },
      resumeCapture: () => {
        if (finished) return
        void context?.resume()
        if (assistantPlayback) return
        muteMic(false)
        stream?.getAudioTracks().forEach(track => {
          track.enabled = true
        })
        meterStream?.getAudioTracks().forEach(track => {
          track.enabled = true
        })
        if (recRestarting) return
        recycleAfterPlayback = false
        ensureRecognitionRunning(false)
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
