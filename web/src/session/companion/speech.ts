// speech.ts is the M9.5 companion voice input (T-9.5.3.1): it reuses
// the toggleSpeech pipeline (getUserMedia with the saved microphone,
// zh-CN final-only SpeechRecognition, analyser-driven levels, and the
// frozen microphone/speech error codes) but routes each final
// transcript straight to ChatBridge instead of the composer.
import { BridgeClientError } from '../../bridge/client'
import { microphoneConstraints, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { MOON_RING_BINS } from './MoonSphere'
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
  /** Interim transcript (real-time, throttled ~100ms) — shown as grey subtitle. */
  onInterim?: (transcript: string) => void
  /** Error mapped to the frozen microphone/speech error codes. */
  onError: (error: BridgeClientError) => void
  /** 12 normalized ring levels, ~30fps while listening. */
  onLevels?: (levels: number[]) => void
  /** Recognition ended without a final (silence timeout, engine stop). */
  onEndWithoutFinal?: () => void
}

export interface CompanionSpeechHandle {
  /** Stop recognition and release the microphone immediately. */
  stop: () => void
}

/** Commit the utterance after this much silence once we already have text.
 *  ~400ms matches cloud voice agents (Doubao / realtime): faster than
 *  waiting for the OS SpeechRecognition isFinal (often 0.8–1.5s). */
export const UTTERANCE_SILENCE_MS = 400

/** Energy above this (0–1 peak) counts as voice for endpointing. */
const VOICE_PEAK = 0.13

export function shouldCommitUtterance(hasText: boolean, silentForMs: number, silenceMs = UTTERANCE_SILENCE_MS): boolean {
  return hasText && silentForMs >= silenceMs
}

export function startCompanionSpeech(callbacks: CompanionSpeechCallbacks): Promise<CompanionSpeechHandle> {
  const Recognition = speechRecognitionConstructor()
  if (!Recognition || !navigator.mediaDevices?.getUserMedia) {
    return Promise.reject(new BridgeClientError('当前系统 WebView 不支持语音输入', 'SPEECH_RECOGNITION_UNAVAILABLE', false, 'renderer'))
  }
  let recognition: SpeechRecognitionLike | undefined
  let stream: MediaStream | undefined
  let context: AudioContext | undefined
  let frame = 0
  let finished = false
  const teardown = () => {
    if (frame) cancelAnimationFrame(frame)
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
      const extra = { echoCancellation: true as const, noiseSuppression: true as const }
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
    let vadEnabled = false
    const assembled = () => {
      const f = finals.trim()
      const i = interim.trim()
      if (!i) return f
      if (!f) return i
      if (f.includes(i)) return f
      return `${f}${i}`
    }
    const commit = (text: string) => {
      if (finished || !text) return
      finished = true
      recognition?.stop()
      teardown()
      callbacks.onFinal(text)
    }
    const tryCommitFromSilence = (voiceAt: number, silenceMs = UTTERANCE_SILENCE_MS) => {
      if (!vadEnabled || finished) return
      if (shouldCommitUtterance(Boolean(assembled()), performance.now() - voiceAt, silenceMs)) commit(assembled())
    }
    try {
      const AudioContextClass = window.AudioContext
      context = new AudioContextClass()
      const analyser = context.createAnalyser()
      analyser.fftSize = 64
      analyser.smoothingTimeConstant = 0.72
      context.createMediaStreamSource(media).connect(analyser)
      const samples = new Uint8Array(analyser.frequencyBinCount)
      const bucket = Math.max(1, Math.floor(samples.length / MOON_RING_BINS))
      vadEnabled = true
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
        if (rawPeak / 255 >= VOICE_PEAK) lastVoiceAt = performance.now()
        else tryCommitFromSilence(lastVoiceAt)
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
      let finalTranscript = ''
      let interimTranscript = ''
      for (let i = 0; i < event.results.length; i++) {
        if (event.results[i].isFinal) finalTranscript += event.results[i][0].transcript
        else interimTranscript += event.results[i][0].transcript
      }
      finals = finalTranscript
      interim = interimTranscript
      if (interimTranscript) {
        const now = performance.now()
        if (now - lastInterimAt >= 100) {
          lastInterimAt = now
          callbacks.onInterim?.(interimTranscript.trim())
        }
      }
      // Without an analyser, keep the previous "first final wins" path so
      // a machine that cannot VAD still sends. With VAD, keep listening
      // until silence so a multi-phrase utterance is one ChatBridge turn.
      if (!vadEnabled && finalTranscript.trim()) {
        commit(assembled())
        return
      }
      tryCommitFromSilence(lastVoiceAt, 280)
    }
    rec.onerror = event => {
      if (finished) return
      finished = true
      teardown()
      const denied = event?.error === 'not-allowed'
      const serviceDisabled = event?.error === 'service-not-allowed'
      callbacks.onError(
        new BridgeClientError(
          denied ? '语音识别服务拒绝访问，请检查 Windows 在线语音识别设置' : serviceDisabled ? 'Windows 在线语音识别服务未启用或不可用' : '系统在线语音识别失败；请检查网络和语言设置',
          denied ? 'SPEECH_SERVICE_PERMISSION_DENIED' : serviceDisabled ? 'SPEECH_SERVICE_DISABLED' : 'SPEECH_RECOGNITION_FAILED',
          false,
          'renderer',
        ),
      )
    }
    rec.onend = () => {
      if (finished) return
      const text = assembled()
      if (text) {
        commit(text)
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
