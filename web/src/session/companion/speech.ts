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

type SpeechRecognitionEventLike = { results: ArrayLike<{ 0: { transcript: string }; isFinal: boolean }> }
type SpeechRecognitionLike = {
  lang: string
  continuous: boolean
  interimResults: boolean
  onresult: ((event: SpeechRecognitionEventLike) => void) | null
  onerror: ((event?: { error?: string }) => void) | null
  onend: (() => void) | null
  start: () => void
  stop: () => void
}

const speechRecognitionConstructor = () =>
  (window as typeof window & { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike }).SpeechRecognition ??
  (window as typeof window & { webkitSpeechRecognition?: new () => SpeechRecognitionLike }).webkitSpeechRecognition

export interface CompanionSpeechCallbacks {
  /** A final transcript arrived — sent straight to ChatBridge by the stage. */
  onFinal: (transcript: string) => void
  /** User spoke over assistant playback or a slow reply (full-duplex barge-in). */
  onBargeIn?: (transcript: string) => void
  /** Interim transcript (real-time, throttled ~100ms) — shown as grey subtitle. */
  onInterim?: (transcript: string) => void
  /** Error mapped to the frozen microphone/speech error codes. */
  onError: (error: BridgeClientError) => void
  /** 12 normalized ring levels, ~30fps while listening. */
  onLevels?: (levels: number[]) => void
  /** Recognition ended without a final (silence timeout, engine stop). */
  onEndWithoutFinal?: () => void
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
  /** Voice-triggered interrupt while assistant is playing or thinking. */
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
  /** Enable voice barge-in detection (thinking or speaking). */
  setBargeInActive: (active: boolean) => void
}

/** Commit after this much analyser silence once we already have text. */
export const UTTERANCE_SILENCE_MS = 620
/** Commit when interim/final text stops changing (Windows SR re-fires the same interim). */
export const UTTERANCE_STABLE_MS = 780
/** Incomplete-looking phrases (mid-command tails) need a longer stable window. */
export const INCOMPLETE_STABLE_MS = 1350
/** Incomplete phrases also tolerate a longer trailing pause before commit. */
export const INCOMPLETE_SILENCE_MS = 980
/** Minimum time with text on screen before we accept a non-terminal commit. */
export const MIN_UTTERANCE_MS = 480

/** Energy above this (0–1 peak) counts as voice for endpointing. */
export const VOICE_PEAK = 0.09
/** Stronger peak while assistant speaks — avoids TTS bleed despite AEC. */
export const BARGE_IN_PEAK = 0.42

export function speechProfile(environment: SpeechEnvironment = 'normal'): SpeechProfile {
  if (environment === 'noisy') {
    return {
      voicePeak: 0.14,
      utteranceSilenceMs: 520,
      utteranceStableMs: 640,
      bargeInPeakDelta: 0.06,
      minVoiceHoldMs: 280,
    }
  }
  return {
    voicePeak: VOICE_PEAK,
    utteranceSilenceMs: UTTERANCE_SILENCE_MS,
    utteranceStableMs: UTTERANCE_STABLE_MS,
    bargeInPeakDelta: 0.04,
    minVoiceHoldMs: 360,
  }
}
/** Sustained voice before a barge-in fires. Thinking only; playback uses mute. */
export const BARGE_IN_HOLD_MS = 280
/** After TTS ends, ignore mic/SR until speaker ring-out dies. */
export const ECHO_GUARD_MS = 700
/** After a click/voice interrupt, unmute quickly so the user can talk. */
export const INTERRUPT_ECHO_MS = 160

export function shouldHoldRecognition(playback: boolean, guardUntil: number, now: number): boolean {
  return playback || now < guardUntil
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

export function startCompanionSpeech(options: CompanionSpeechOptions): Promise<CompanionSpeechHandle> {
  const { duplex = false, bargeIn = true, environment = 'normal', ...callbacks } = options
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
  const recognitionHeld = () => shouldHoldRecognition(assistantPlayback, echoGuardUntil, performance.now())
  const muteMic = (muted: boolean) => {
    stream?.getAudioTracks().forEach(track => {
      track.enabled = !muted
    })
  }
  const teardown = () => {
    if (frame) cancelAnimationFrame(frame)
    window.clearTimeout(recSilenceTimer)
    window.clearTimeout(restartTimer)
    window.clearTimeout(echoTimer)
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
      const extra = { echoCancellation: true as const, noiseSuppression: true as const, autoGainControl: false as const }
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
    let finals = ''
    let interim = ''
    let lastVoiceAt = performance.now()
    let lastTextChangeAt = performance.now()
    let firstTextAt = 0
    let utteranceVoiceSince = 0
    const assembled = () => {
      const f = finals.trim()
      const i = interim.trim()
      if (!i) return f
      if (!f) return i
      if (f.includes(i)) return f
      return `${f}${i}`
    }
    const restartRecognition = () => {
      if (finished || !recognition) return
      finals = ''
      interim = ''
      bargeVoiceSince = 0
      utteranceVoiceSince = 0
      firstTextAt = 0
      lastTextChangeAt = performance.now()
      callbacks.onInterim?.('')
      recRestarting = true
      window.clearTimeout(restartTimer)
      try {
        recognition.stop()
      } catch {
        /* engine may already be stopped */
      }
      restartTimer = window.setTimeout(() => {
        recRestarting = false
        if (finished || !recognition || recognitionHeld()) return
        try {
          recognition.start()
        } catch {
          /* onend will recover or surface onError */
        }
      }, 60)
    }
    const commit = (text: string) => {
      if (finished || !text || recognitionHeld()) return
      if (duplex) {
        callbacks.onFinal(text)
        restartRecognition()
        return
      }
      finished = true
      recognition?.stop()
      teardown()
      callbacks.onFinal(text)
    }
    const tryBargeIn = (rawPeak: number) => {
      if (finished || !duplex || !bargeIn || !bargeInActive || !callbacks.onBargeIn) return
      // Speaker playback always leaks into the mic. Do not treat that
      // energy as a barge-in — moon click still interrupts.
      if (assistantPlayback || recognitionHeld()) {
        bargeVoiceSince = 0
        return
      }
      const peak = rawPeak / 255
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
      if (finished || commitPaused || recognitionHeld()) return
      const text = assembled().trim()
      if (!text) return
      if (profile.minVoiceHoldMs > 0) {
        if (!utteranceVoiceSince) return
        if (performance.now() - utteranceVoiceSince < profile.minVoiceHoldMs) return
      }
      const textSince = firstTextAt ? performance.now() - firstTextAt : 0
      if (shouldDeferCommit(text, textSince)) return
      const { stableMs, silenceMs } = endpointingForText(text, profile)
      const silentFor = performance.now() - voiceAt
      const stableFor = performance.now() - lastTextChangeAt
      // Stable transcript is enough: Windows Speech Recognition keeps
      // re-emitting the same interim, which used to look like "still talking".
      if (shouldCommitStable(true, stableFor, stableMs)) {
        commit(text)
        return
      }
      if (shouldCommitUtterance(true, silentFor, silenceMs)) commit(text)
    }
    try {
      const AudioContextClass = window.AudioContext
      context = new AudioContextClass()
      void context.resume().then(() => unlockTtsAudio())
      const analyser = context.createAnalyser()
      analyser.fftSize = 64
      analyser.smoothingTimeConstant = 0.4
      context.createMediaStreamSource(media).connect(analyser)
      const samples = new Uint8Array(analyser.frequencyBinCount)
      const bucket = Math.max(1, Math.floor(samples.length / MOON_RING_BINS))
      const meter = () => {
        analyser.getByteFrequencyData(samples)
        const levels: number[] = []
        let rawPeak = 0
        for (let index = 0; index < MOON_RING_BINS; index++) {
          let peak = 0
          for (let i = index * bucket; i < Math.min(samples.length, (index + 1) * bucket); i++) peak = Math.max(peak, samples[i])
          rawPeak = Math.max(rawPeak, peak)
          levels.push(Math.max(0.06, peak / 255))
        }
        const peak = rawPeak / 255
        if (peak >= profile.voicePeak) {
          if (!utteranceVoiceSince && assembled().trim()) utteranceVoiceSince = performance.now()
          lastVoiceAt = performance.now()
        } else tryCommitFromSilence(lastVoiceAt)
        tryBargeIn(rawPeak)
        callbacks.onLevels?.(levels)
        frame = requestAnimationFrame(meter)
      }
      meter()
    } catch {
      // Visual-only loss: recognition still works; commit on isFinal / onend.
    }
    const rec: SpeechRecognitionLike = new Recognition()
    recognition = rec
    rec.lang = 'zh-CN'
    rec.continuous = true
    rec.interimResults = true
    let lastInterimAt = 0
    rec.onresult = event => {
      if (recognitionHeld()) return
      let finalTranscript = ''
      let interimTranscript = ''
      for (let i = 0; i < event.results.length; i++) {
        if (event.results[i].isFinal) finalTranscript += event.results[i][0].transcript
        else interimTranscript += event.results[i][0].transcript
      }
      const prev = assembled()
      finals = finalTranscript
      interim = interimTranscript
      const now = performance.now()
      const next = assembled().trim()
      if (next !== prev.trim()) {
        lastTextChangeAt = now
        if (!firstTextAt) firstTextAt = now
      }
      // Do not bump lastVoiceAt here. Recognition events are not energy;
      // Windows will keep the same interim alive for tens of seconds.
      if (interimTranscript) {
        if (now - lastInterimAt >= 80) {
          lastInterimAt = now
          callbacks.onInterim?.(interimTranscript.trim())
        }
      }
      window.clearTimeout(recSilenceTimer)
      // Windows marks phrase-level finals while the user may still be
      // mid-sentence — never commit on isFinal alone.
      if (next) {
        const { stableMs } = endpointingForText(next, profile)
        recSilenceTimer = window.setTimeout(() => tryCommitFromSilence(lastVoiceAt), stableMs)
      }
      tryCommitFromSilence(lastVoiceAt)
    }
    rec.onerror = event => {
      if (finished) return
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
      if (duplex) return
      finished = true
      teardown()
      callbacks.onEndWithoutFinal?.()
    }
    rec.onend = () => {
      if (finished) return
      if (recRestarting || recognitionHeld()) return
      const text = assembled().trim()
      if (text) {
        commit(text)
        return
      }
      if (duplex) {
        restartTimer = window.setTimeout(() => {
          if (finished || !recognition) return
          try {
            recognition.start()
          } catch {
            finished = true
            teardown()
            callbacks.onEndWithoutFinal?.()
          }
        }, 80)
        return
      }
      finished = true
      teardown()
      callbacks.onEndWithoutFinal?.()
    }
    rec.start()
    return {
      stop: () => {
        if (finished) return
        finished = true
        recognition?.stop()
        teardown()
      },
      setAssistantPlayback: (active: boolean, echoGuardMs = ECHO_GUARD_MS) => {
        assistantPlayback = active
        window.clearTimeout(echoTimer)
        finals = ''
        interim = ''
        bargeVoiceSince = 0
        firstTextAt = 0
        lastTextChangeAt = performance.now()
        callbacks.onInterim?.('')
        if (active) {
          muteMic(true)
          recRestarting = true
          try {
            recognition?.stop()
          } catch {
            /* already stopped */
          }
          return
        }
        muteMic(false)
        echoGuardUntil = performance.now() + Math.max(0, echoGuardMs)
        echoTimer = window.setTimeout(() => {
          if (finished || assistantPlayback) return
          restartRecognition()
        }, Math.max(0, echoGuardMs))
      },
      setCommitPaused: (paused: boolean) => {
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
        bargeInActive = active
        if (!active) bargeVoiceSince = 0
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
