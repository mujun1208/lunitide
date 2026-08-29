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
  abort?: () => void
}

const speechRecognitionConstructor = () =>
  (window as typeof window & { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike }).SpeechRecognition ??
  (window as typeof window & { webkitSpeechRecognition?: new () => SpeechRecognitionLike }).webkitSpeechRecognition

export interface CompanionSpeechCallbacks {
  /** A final transcript arrived — sent straight to ChatBridge by the stage. */
  onFinal: (transcript: string) => void
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
}

export interface CompanionSpeechOptions extends CompanionSpeechCallbacks {
  /** Keep mic + recognition alive between turns (phone-call loop). */
  duplex?: boolean
  /** Tighter endpointing when cafes / background voices are present. */
  environment?: SpeechEnvironment
  /**
   * SpeechRecognition only — no second getUserMedia.
   * Used beside MiniCPM-o PCM capture so captions still show the user's words.
   */
  meterless?: boolean
  /** Extra this-PC streams mixed into local ASR. Ignored by Web Speech. */
  extraStreams?: MediaStream[]
  /**
   * Hold clauses together (meeting notes). Companion turn-taking stays the
   * default. Ignores the engine's own endpoint and waits longer before commit.
   */
  holdUtterance?: boolean
  /**
   * Meeting notes owns PCM capture. Local ASR must not open a second
   * microphone or stop the WAV writer when the recognizer recycles.
   */
  externalPcm?: boolean
}

export interface CompanionSpeechHandle {
  /** Stop recognition and release the microphone immediately. */
  stop: () => void
  /** Assistant TTS active — mute the mic, keep recognition warm, ignore output. */
  setAssistantPlayback: (active: boolean, echoGuardMs?: number) => void
  /** Pause silence-based commit while thinking/speaking. */
  setCommitPaused: (paused: boolean) => void
  /** Restart the recognizer if it stalled (listening with no transcript). */
  pulseRecognition: () => void
  /** Force-send the current assembled transcript (fallback when endpointing stalls). */
  forceCommit: () => void
  /** After a user gesture: resume Web Audio and (re)start recognition if it is dead. */
  resumeCapture: () => void
  /** Commit whatever is in the buffer, including unfinished clauses. Meeting notes awaits this before stop. */
  flush?: () => void | Promise<void>
  /** Feed PCM captured by the meeting recorder into local ASR. */
  pushPcm?: (frame: { base64: string; samples: Int16Array; peak: number }) => void
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
/** Last-resort commit when the transcript AND the mic have been quiet.
 *  Sits just above the 1.2s turn-end so it never becomes the rule they feel. */
export const STUCK_TRANSCRIPT_MS = 1400
/** Last-resort stage commit. Sits just above the 1.2s turn-end so a stuck
 *  caption still answers, without becoming the everyday endpoint. */
export const FORCE_COMMIT_MS = 1400
/** Restart SR only after this long of real mic energy with no transcript.
 *  Windows returns a first interim within a few hundred milliseconds of
 *  speech, so a second of talking with nothing on screen is already wrong. */
export const VOICE_WITHOUT_TEXT_MS = 900
/** How long since the last SR token before a voice-energy restart.
 *  Bounds how long a dead session can swallow speech before it is replaced,
 *  so it is kept just above the time a healthy engine takes to return its
 *  first token rather than well clear of it. */
export const VOICE_RESTART_RESULT_MS = 1400
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

export function speechProfile(environment: SpeechEnvironment = 'normal'): SpeechProfile {
  if (environment === 'noisy') {
    return {
      voicePeak: 0.08,
      utteranceSilenceMs: 100,
      utteranceStableMs: 120,
    }
  }
  return {
    voicePeak: VOICE_PEAK,
    utteranceSilenceMs: UTTERANCE_SILENCE_MS,
    utteranceStableMs: UTTERANCE_STABLE_MS,
  }
}
/** After TTS ends, ignore mic/SR until speaker ring-out dies.
 *  Laptop speakers + built-in mic leave 300–600ms of residual echo even
 *  with Chromium/WASAPI AEC — shorter guards transcribe her own voice. */
export const ECHO_GUARD_MS = 450
/** After a click interrupt, unmute quickly so the user can talk. */
export const INTERRUPT_ECHO_MS = 80
/** How long after her turn a transcript may still be her, not the user. */
export const PLAYBACK_TAIL_MS = 2500

/** Recognition is ignored while she speaks, and while the speaker rings out. */
export function shouldHoldRecognition(playback: boolean, guardUntil: number, now: number): boolean {
  return playback || now < guardUntil
}

export function shouldCommitUtterance(hasText: boolean, silentForMs: number, silenceMs = UTTERANCE_SILENCE_MS): boolean {
  return hasText && silentForMs >= silenceMs
}

export function shouldCommitStable(hasText: boolean, stableForMs: number, stableMs = UTTERANCE_STABLE_MS): boolean {
  return hasText && stableForMs >= stableMs
}

/** Silence that ends a turn: 1.2s after they actually stop talking. */
export const TURN_END_SILENCE_MS = 1200
/** Incomplete phrases like 「打开网」 wait a beat longer than a finished
 *  sentence so a breath mid-clause is not treated as done. */
export const TURN_END_INCOMPLETE_SILENCE_MS = 1500
/** Meeting notes: people pause mid-thought; keep the clause open longer than companion turn-taking. */
export const MEETING_TURN_END_SILENCE_MS = 2000
/** Unfinished meeting phrases wait a little past the 2s hold. */
export const MEETING_TURN_END_INCOMPLETE_SILENCE_MS = 2500
/** A turn never ends while the transcript is still growing. */
export const TURN_END_TEXT_SETTLE_MS = 400

export function turnEndWindows(holdUtterance = false): { silenceMs: number; incompleteSilenceMs: number } {
  if (holdUtterance) {
    return { silenceMs: MEETING_TURN_END_SILENCE_MS, incompleteSilenceMs: MEETING_TURN_END_INCOMPLETE_SILENCE_MS }
  }
  return { silenceMs: TURN_END_SILENCE_MS, incompleteSilenceMs: TURN_END_INCOMPLETE_SILENCE_MS }
}

/**
 * Whether the user has finished speaking.
 *
 * One rule for both recognizers, because the question is about the speaker,
 * not the engine. They used to answer it separately and were wrong in the
 * same direction: the cloud path ended a turn after ~220ms of unchanged
 * text, the local path after ~280ms of quiet, and both of those are ordinary
 * gaps in the middle of a sentence. 「你好月汐」 was committed as 「你好」
 * and answered as 「你好」, with the rest arriving during the reply, where it
 * was correctly discarded as hers.
 *
 * A turn ends on the room staying quiet for as long as someone pauses when
 * they have genuinely finished, agreed by two independent signals — either
 * alone is wrong, since a breath still trips the energy gate and a recognizer
 * running behind the audio still trips the transcript.
 */
export function turnEnded(input: {
  speechActive: boolean
  /** Undefined when the level has never crossed the speech gate. */
  silentForMs: number | undefined
  msSinceLastResult: number
  incomplete: boolean
  silenceMs?: number
  incompleteSilenceMs?: number
}): boolean {
  if (input.speechActive) return false
  if (input.msSinceLastResult < TURN_END_TEXT_SETTLE_MS) return false
  const quiet = input.incomplete
    ? (input.incompleteSilenceMs ?? TURN_END_INCOMPLETE_SILENCE_MS)
    : (input.silenceMs ?? TURN_END_SILENCE_MS)
  // A microphone whose level never reaches the speech gate — a quiet device,
  // or aggressive noise suppression — still has to be able to end a turn.
  // Requiring the energy signal outright would leave that user waiting
  // forever for a reply, gated on evidence their hardware cannot produce, so
  // the transcript going quiet stands in for it.
  if (input.silentForMs === undefined) return input.msSinceLastResult >= quiet
  return input.silentForMs >= quiet
}

/**
 * Last-resort commit used by forceCommit / the local backstop.
 *
 * Windows can keep speechActive true by re-firing the same interim, and
 * analyser noise can do the same. A caption that has not grown for the
 * 1.2s product window is a finished turn even if those signals lie.
 * Incomplete commands still must not commit on a 50–400ms breath — only
 * after 1.2s of true silence after they actually stopped.
 */
export function shouldForceCommitUtterance(input: {
  speechActive: boolean
  silentForMs: number | undefined
  textStableForMs: number
  incomplete: boolean
  silenceMs?: number
  incompleteSilenceMs?: number
}): boolean {
  if (input.textStableForMs < TURN_END_TEXT_SETTLE_MS) return false
  const quiet = input.incomplete
    ? (input.incompleteSilenceMs ?? TURN_END_INCOMPLETE_SILENCE_MS)
    : (input.silenceMs ?? TURN_END_SILENCE_MS)
  // A caption that has not grown for the product window is a finished turn.
  // Analyser energy after TTS (fan, echo, Windows zombie speechActive) used
  // to keep this false forever — round 3 sat on 聆听中 with 「把开了…」 and
  // never called chat.start. Growing transcripts reset textStableForMs, so
  // this cannot fire while they are still adding words.
  if (input.textStableForMs >= Math.max(quiet, STUCK_TRANSCRIPT_MS)) {
    if (!input.incomplete) return true
    if (!input.speechActive || (input.silentForMs !== undefined && input.silentForMs >= quiet)) return true
    // Incomplete + analyser still hot: 「你可以」/「打开网」may still grow.
    // Past the hard ceiling the recognizer is stuck, not the speaker.
    if (input.textStableForMs >= INCOMPLETE_HARD_MS) return true
    return false
  }
  if (input.speechActive && (input.silentForMs === undefined || input.silentForMs < quiet)) {
    return false
  }
  return turnEnded({
    speechActive: input.speechActive,
    silentForMs: input.silentForMs,
    msSinceLastResult: input.textStableForMs,
    incomplete: input.incomplete,
    silenceMs: input.silenceMs,
    incompleteSilenceMs: input.incompleteSilenceMs,
  })
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

/**
 * Pick a recognition alternative. Windows often ranks a short fragment
 * higher-confidence than the longer prefix-extending hypothesis; prefer
 * the longer one when it extends the other.
 */
export function pickRecognitionTranscript(result: { length: number; [index: number]: { transcript: string; confidence?: number } | undefined }): string {
  const n = result.length ?? 1
  const alts: Array<{ transcript: string; confidence: number }> = []
  for (let i = 0; i < n; i++) {
    const alt = result[i]
    const transcript = alt?.transcript ?? ''
    if (!transcript) continue
    alts.push({ transcript, confidence: alt?.confidence ?? 0 })
  }
  if (!alts.length) return ''
  let best = alts[0]!
  for (const alt of alts.slice(1)) {
    const longerExtends =
      (alt.transcript.startsWith(best.transcript) && alt.transcript.length > best.transcript.length) ||
      (best.transcript.startsWith(alt.transcript) && best.transcript.length > alt.transcript.length)
    if (longerExtends) {
      if (alt.transcript.length > best.transcript.length) best = alt
      continue
    }
    if (alt.confidence > best.confidence) best = alt
  }
  return best.transcript
}

/** Idle moon rings must stay below VOICE_PEAK so they never look like speech. */
export function idleMeterLevel(t: number, index: number): number {
  return 0.012 + 0.014 * Math.abs(Math.sin(t * 0.6 + index * 0.45))
}

/**
 * Whether a recognizer that is being spoken to but has produced nothing
 * should be replaced.
 *
 * Deliberately says nothing about whether the session claims to be running.
 * That test used to be here, and it skipped this repair in exactly the case
 * it exists for: a Windows session that started, reported audio, and then
 * never returned a token still looks alive. The only other repair refuses to
 * act while the user is speaking and otherwise waits eight seconds from the
 * start of the session, so a dead recognizer swallowed whole sentences and
 * the user had to say them again.
 *
 * Requiring an empty transcript is what makes replacing it safe: there is no
 * utterance in flight to lose.
 */
export function shouldReplaceSilentRecognizer(input: {
  hasText: boolean
  voiceForMs: number
  msSinceLastResult: number
  msSinceLastRestart: number
}): boolean {
  if (input.hasText) return false
  if (input.voiceForMs < VOICE_WITHOUT_TEXT_MS) return false
  if (input.msSinceLastResult < VOICE_RESTART_RESULT_MS) return false
  return input.msSinceLastRestart >= STALL_RESTART_GAP_MS
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
  const { duplex = false, environment = 'normal', meterless = false, holdUtterance = false, ...callbacks } = options
  const windows = turnEndWindows(holdUtterance)
  const profile = speechProfile(environment)
  const Recognition = speechRecognitionConstructor()
  if (!Recognition || (!meterless && !navigator.mediaDevices?.getUserMedia)) {
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
  let commitPaused = false
  let recRestarting = false
  let echoGuardUntil = 0
  let echoTimer = 0
  let playbackApplied = false
  let playbackEchoGuardMs = ECHO_GUARD_MS
  let commitPausedApplied = false
  let lastResultAt = performance.now()
  let lastRecognitionPulseAt = 0
  let lastStartAt = 0
  /** Same as the chat composer: continuous listen so a sentence is not cut. */
  let preferContinuous = true
  let keepAliveTimer = 0
  let voiceEnergyWithoutTextSince = 0
  let recognitionAlive = false
  const recognitionHeld = () => shouldHoldRecognition(assistantPlayback, echoGuardUntil, performance.now())
    let recycleAfterPlayback = false
    let lastPlaybackEndedAt = 0
    let recGeneration = 0
  /**
   * Silences the streams this module owns: the analyser feed and the meter.
   *
   * Not the recognizer. Web Speech opens its own capture inside the engine and
   * never looks at these tracks, so this does nothing to what Windows hears —
   * a fact worth stating because the code once relied on the opposite, muted
   * here while she was speaking, and had her whole reply transcribed anyway.
   * Keeping the recognizer from hearing her means stopping the recognizer.
   */
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
    const gumPromise = meterless
      ? undefined
      : (async (): Promise<MediaStream> => {
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
        // InvalidStateError on a freshly minted recognizer is Windows still
        // releasing the previous session — not "already running". Treating it
        // as alive left round 3 in 聆听中 with nothing underneath listening.
        if (recognitionAlreadyRunning(error) && recognitionAlive) {
          return true
        }
        recognitionAlive = false
        try {
          return tryStart(!preferContinuous)
        } catch (retryError) {
          if (recognitionAlreadyRunning(retryError) && recognitionAlive) {
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
    const discardRecognizer = () => {
      recGeneration += 1
      recognitionAlive = false
      lastStartAt = 0
      const old = recognition
      recognition = undefined
      try {
        old?.abort?.()
      } catch {
        /* engine may already be stopped */
      }
      try {
        old?.stop()
      } catch {
        /* engine may already be stopped */
      }
    }
    const restartRecognition = (clearTranscript = true) => {
      if (finished) return
      if (clearTranscript) {
        finals = ''
        interim = ''
        utteranceVoiceSince = 0
        firstTextAt = 0
        lastTextChangeAt = performance.now()
        lastResultAt = performance.now()
        callbacks.onInterim?.('')
      }
      recRestarting = true
      window.clearTimeout(restartTimer)
      // Windows WebView dies after a handful of stop()/start() cycles on the
      // same SpeechRecognition object. A new instance after each recycle is
      // what keeps turns 3+ listening.
      mintRecognizer()
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
    let lastCommittedCompact = ''
    let lastCommittedAt = 0
    const compactCommit = (value: string) => value.replace(/\s/g, '')
    const commit = (text: string) => {
      if (finished || !text || recognitionHeld() || assistantPlayback) return
      if (echoOfHerReply(text)) {
        dropWhatSheHeardOfHerself()
        return
      }
      const compact = compactCommit(text)
      const now = performance.now()
      if (compact === lastCommittedCompact && now - lastCommittedAt < 1500) return
      lastCommittedCompact = compact
      lastCommittedAt = now
      if (duplex) {
        callbacks.onFinal(text)
        finals = ''
        interim = ''
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
     * Her turn is hers until the 打断 button ends it or she finishes.
     *
     * Nothing the microphone reports gets a say, so what reaches here is
     * discarded rather than weighed. It is discarded rather than ignored
     * because audio caught around a turn boundary would otherwise sit in the
     * buffer and be delivered as the opening of whatever the user says next.
     */
    const dropWhatSheHeardOfHerself = () => {
      finals = ''
      interim = ''
    }
    /**
     * Whether this is her reply reaching us after her turn is already over.
     *
     * Stopping the recognizer for the length of the reply removes most of
     * this, but not the last of it: the engine can report an utterance it was
     * still holding when it was stopped, and a speaker takes a moment to stop
     * ringing. Bounded to just after her turn, so a user who genuinely repeats
     * her words later is not silenced for it.
     */
    const echoOfHerReply = (text: string) => {
      if (!lastPlaybackEndedAt) return false
      if (performance.now() - lastPlaybackEndedAt > PLAYBACK_TAIL_MS) return false
      return looksLikePlaybackEcho(text, callbacks.spokenText?.() ?? '')
    }
    const tryCommitFromSilence = (voiceAt: number) => {
      if (finished || commitPaused || recognitionHeld() || assistantPlayback) return
      const now = performance.now()
      if (voiceAt && now - voiceAt > profile.utteranceSilenceMs) speechActive = false
      const text = assembled().trim()
      if (!text) return
      const textSince = firstTextAt ? now - firstTextAt : 0
      if (shouldDeferCommit(text, textSince)) return
      const incomplete = looksIncompleteUtterance(text)
      const silentForMs = meterless ? undefined : voiceAt ? performance.now() - voiceAt : undefined
      const textStableForMs = performance.now() - lastTextChangeAt
      if (
        shouldForceCommitUtterance({
          speechActive: meterless ? false : speechActive,
          silentForMs,
          textStableForMs,
          incomplete,
          silenceMs: windows.silenceMs,
          incompleteSilenceMs: windows.incompleteSilenceMs,
        })
      ) {
        commit(text)
      }
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
      const textLocked = !!assembled().trim() && now - lastTextChangeAt >= TURN_END_SILENCE_MS
      if (energy >= profile.voicePeak) {
        // After the transcript has sat still for the 1.2s product window,
        // room noise / speaker ring-out must not keep lastVoiceAt fresh —
        // that is how force-commit never fired on round 3.
        if (!textLocked) {
          speechActive = true
          lastVoiceAt = now
        }
        callbacks.onVoiceEnergy?.()
        if (!assembled().trim()) voiceEnergyWithoutTextSince = voiceEnergyWithoutTextSince || now
        else voiceEnergyWithoutTextSince = 0
      } else if (now - lastVoiceAt > profile.utteranceSilenceMs) {
        speechActive = false
        voiceEnergyWithoutTextSince = 0
      }
      callbacks.onLevels?.(levels)
      if (
        duplex &&
        !assistantPlayback &&
        energy >= profile.voicePeak &&
        voiceEnergyWithoutTextSince &&
        shouldReplaceSilentRecognizer({
          hasText: !!assembled().trim(),
          voiceForMs: now - voiceEnergyWithoutTextSince,
          msSinceLastResult: now - lastResultAt,
          msSinceLastRestart: now - lastStallRestartAt,
        })
      ) {
        voiceEnergyWithoutTextSince = now
        lastStallRestartAt = now
        // The replacement runs in the other mode. Whether Windows returns
        // anything is machine-dependent, and continuous is the mode that just
        // produced nothing here — retrying it identically is the one option
        // already known not to work on this machine.
        preferContinuous = !preferContinuous
        restartRecognition(false)
      }
      tryCommitFromSilence(lastVoiceAt)
      restartIfStalled()
      frame = requestAnimationFrame(paintLevels)
    }
    const markSpeech = () => {
      if (finished) return
      speechActive = true
      voiceEnergyWithoutTextSince = voiceEnergyWithoutTextSince || performance.now()
      lastVoiceAt = performance.now()
      callbacks.onSpeechStart?.()
      callbacks.onVoiceEnergy?.()
    }
    const bindRecognizer = (rec: SpeechRecognitionLike, gen: number) => {
      recognition = rec
      rec.lang = companionRecognitionLang()
      rec.continuous = preferContinuous
      rec.interimResults = true
      rec.maxAlternatives = 3
      rec.onsoundstart = markSpeech
      rec.onspeechstart = markSpeech
      rec.onspeechend = () => {
        // Do not clear speechActive or commit: Windows fires speechend at
        // clause boundaries ("你可以") while the user is still speaking.
      }
      rec.onaudiostart = () => {
        if (gen !== recGeneration) return
        recognitionAlive = true
      }
      rec.onresult = event => {
        if (gen !== recGeneration) return
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
          voiceEnergyWithoutTextSince = 0
          if (!utteranceVoiceSince) utteranceVoiceSince = now
        }
        if (next !== before.trim()) {
          lastVoiceAt = now
          speechActive = true
          lastTextChangeAt = now
          if (!firstTextAt) firstTextAt = now
        }
        if (assistantPlayback) {
          dropWhatSheHeardOfHerself()
          return
        }
        if (next && echoOfHerReply(next)) {
          dropWhatSheHeardOfHerself()
          return
        }
        if (next) callbacks.onInterim?.(next)
        else if (!held) callbacks.onInterim?.('')
        if (held) return
        if (commitPaused) {
          dropWhatSheHeardOfHerself()
          return
        }
        window.clearTimeout(recSilenceTimer)
        if (next && !commitPaused) {
          const quiet = looksIncompleteUtterance(next) ? windows.incompleteSilenceMs : windows.silenceMs
          recSilenceTimer = window.setTimeout(() => tryCommitFromSilence(lastVoiceAt), quiet)
        }
        if (!commitPaused) tryCommitFromSilence(lastVoiceAt)
      }
      rec.onerror = event => {
        if (gen !== recGeneration) return
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
            if (assistantPlayback) return
            if (recognitionHeld()) return
            if (event?.error === 'no-speech' && assembled().trim()) return
            restartRecognition(false)
          }, event?.error === 'no-speech' ? (assistantPlayback ? 40 : 300) : 420)
          return
        }
        if (duplex) {
          restartRecognition(false)
          return
        }
        finished = true
        teardown()
        callbacks.onEndWithoutFinal?.()
      }
      rec.onend = () => {
        if (gen !== recGeneration) return
        recognitionAlive = false
        if (finished) return
        if (recRestarting) return
        if (assistantPlayback) return
        if (recognitionHeld()) {
          scheduleRecognitionAfterGuard()
          return
        }
        if (duplex) {
          restartTimer = window.setTimeout(() => {
            if (finished || recognitionHeld()) return
            if (!startRecognition()) restartRecognition(false)
          }, 40)
          return
        }
        const text = assembled().trim()
        if (text) {
          tryCommitFromSilence(lastVoiceAt)
          return
        }
        finished = true
        teardown()
        callbacks.onEndWithoutFinal?.()
      }
    }
    const mintRecognizer = () => {
      const Ctor = speechRecognitionConstructor()
      if (!Ctor) return false
      discardRecognizer()
      const gen = recGeneration
      bindRecognizer(new Ctor(), gen)
      return true
    }
    mintRecognizer()
    startRecognition()
    if (gumPromise) {
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
    }
    keepAliveTimer = window.setInterval(() => {
      if (finished || recRestarting) return
      if (assistantPlayback) return
      if (recognitionHeld()) return
      const text = assembled().trim()
      if (text) {
        tryCommitFromSilence(lastVoiceAt)
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
        // The mic is muted for the length of the reply. It was left hot for a
        // while so the user could cut in by talking, but neither energy nor
        // transcript can reliably tell her voice from theirs — laptop
        // speaker, built-in mic, same language, same gender — and every false
        // positive cut her off in the middle of a word. Interrupting is the
        // 打断 button's job.
        window.clearTimeout(echoTimer)
        if (active) {
          recycleAfterPlayback = false
          finals = ''
          interim = ''
          firstTextAt = 0
          lastTextChangeAt = performance.now()
          callbacks.onInterim?.('')
          muteMic(true)
          recRestarting = false
          // The recognizer is stopped, not just ignored.
          //
          // It used to be left running to stay warm, on the understanding
          // that muting the microphone kept it from hearing her and that
          // onresult would discard anything that slipped through. Neither
          // held: Web Speech captures audio itself and never saw the muted
          // tracks, so it listened to the entire reply through the speaker,
          // held the transcript, and delivered it the moment the guard came
          // off — arriving as the user's next question, in her words.
          //
          // Nothing needs recognition during her turn now that only the 打断
          // button can end it, so the warm session bought a cold start back
          // and paid for it with that.
          recognition?.stop()
          recognitionAlive = false
          return
        }
        lastPlaybackEndedAt = performance.now()
        muteMic(false)
        // Whatever the engine may still report about her turn belongs to it.
        finals = ''
        interim = ''
        firstTextAt = 0
        echoGuardUntil = performance.now() + Math.max(0, echoGuardMs)
        recycleAfterPlayback = false
        mintRecognizer()
        ensureRecognitionRunning(false)
        echoTimer = window.setTimeout(() => {
          if (finished || assistantPlayback) return
          if (!recognitionAlive) mintRecognizer()
          ensureRecognitionRunning(false)
        }, Math.max(0, echoGuardMs))
      },
      setCommitPaused: (paused: boolean) => {
        if (paused === commitPausedApplied) return
        commitPausedApplied = paused
        commitPaused = paused
        // Keep the on-screen caption. Wiping here made a pause flicker
        // (thinking ↔ listening) erase a sentence that had not committed yet.
        // Playback start already clears finals/interim in setAssistantPlayback.
      },
      pulseRecognition: () => {
        if (finished || recognitionHeld() || recRestarting) return
        const text = assembled().trim()
        if (text) {
          tryCommitFromSilence(lastVoiceAt)
          return
        }
        lastRecognitionPulseAt = performance.now()
        restartRecognition(false)
      },
      forceCommit: () => {
        if (assistantPlayback || commitPaused) return
        const text = assembled().trim()
        if (!text) return
        const now = performance.now()
        if (
          !shouldForceCommitUtterance({
            speechActive: meterless ? false : speechActive,
            silentForMs: meterless ? undefined : lastVoiceAt ? now - lastVoiceAt : undefined,
            textStableForMs: now - lastTextChangeAt,
            incomplete: looksIncompleteUtterance(text),
            silenceMs: windows.silenceMs,
            incompleteSilenceMs: windows.incompleteSilenceMs,
          })
        ) {
          return
        }
        commit(text)
      },
      flush: () => {
        const text = assembled().trim()
        if (text && !assistantPlayback && !commitPaused) commit(text)
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
