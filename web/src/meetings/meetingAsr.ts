import { localAsrStatus } from '../session/companion/localAsr'
import { int16ToBase64 } from '../session/companion/pcmFrames'
import { startLocalCompanionSpeech } from '../session/companion/localSpeech'
import { startCompanionSpeech, type CompanionSpeechHandle, type CompanionSpeechOptions } from '../session/companion/speech'
import { startVolcCompanionSpeech } from '../session/companion/volc/volcSpeech'
import type { MeetingListen } from './meetingSettings'
import {
  captureThisPcSystemAudio,
  hasLiveAudioTrack,
  isCaptureCanceled,
  NO_SYSTEM_AUDIO_NOTICE,
  stopMediaStream,
  type CaptureThisPcSystemAudioOptions,
} from './meetingCapture'
import { createMeetingLineBuffer } from './meetingText'

export type MeetingAudioSource = 'microphone' | 'microphone_and_system'

export type MeetingCapturePlan = {
  extraStreams: MediaStream[]
  audioSource: MeetingAudioSource
  notice: string
  engineOwned?: boolean
}

const MIC_ONLY_NOTICE = '未能收录系统声音，已继续录制麦克风。'

export function engineLoopbackPlan(): MeetingCapturePlan {
  return { extraStreams: [], audioSource: 'microphone_and_system', notice: '', engineOwned: true }
}

export function mixMeetingPcmS16le(mic: Int16Array, loop: Int16Array): Int16Array {
  const n = Math.max(mic.length, loop.length)
  const out = new Int16Array(n)
  for (let i = 0; i < n; i++) {
    const a = i < mic.length ? mic[i] : 0
    const b = i < loop.length ? loop[i] : 0
    let s = a + b
    if (s > 32767) s = 32767
    else if (s < -32768) s = -32768
    out[i] = s
  }
  return out
}

export function decodeMeetingPcmBase64(b64: string): Int16Array | undefined {
  const raw = b64.trim()
  if (!raw) return
  try {
    const binary = atob(raw)
    const bytes = binary.length & ~1
    if (bytes < 2) return
    const samples = new Int16Array(bytes / 2)
    for (let i = 0; i < samples.length; i++) {
      samples[i] = (binary.charCodeAt(i * 2) | (binary.charCodeAt(i * 2 + 1) << 8)) << 16 >> 16
    }
    return samples
  } catch {
    return
  }
}

export function pcmFrameFromSamples(samples: Int16Array): { base64: string; samples: Int16Array; peak: number } {
  let peak = 0
  for (let i = 0; i < samples.length; i++) {
    const mag = samples[i] < 0 ? -samples[i] : samples[i]
    if (mag > peak) peak = mag
  }
  return { base64: int16ToBase64(samples), samples, peak: peak / 32768 }
}

export function planHasLiveSystemAudio(plan: MeetingCapturePlan | undefined): boolean {
  if (!plan || plan.audioSource !== 'microphone_and_system') return false
  return plan.engineOwned === true || plan.extraStreams.some(hasLiveAudioTrack)
}

export function captureStateNotice(plan: MeetingCapturePlan | undefined): string {
  if (!plan) return ''
  if (planHasLiveSystemAudio(plan)) return ''
  if (plan.audioSource === 'microphone_and_system' && !plan.extraStreams.some(hasLiveAudioTrack)) {
    return '系统声音轨道已中断，正在重试收录。当前仅转写麦克风。'
  }
  return plan.notice
}

export type MeetingSpeechOptions = CompanionSpeechOptions & {
  listen?: MeetingListen | 'auto'
  volcProviderId?: string
}

/** Stop-time catch-up always decodes the WAV with this-PC sherpa. Live listen can be 系统/火山/本机. */
export const MEETING_CATCHUP_HINT = '补转写只用本机识别。选了系统或火山时，停止后的缺口仍走 sherpa；本机未就绪则保留实时字幕。'

function resolveMeetingListen(listen: MeetingSpeechOptions['listen']): MeetingListen {
  if (listen === 'local' || listen === 'volc' || listen === 'cloud') return listen
  return 'cloud'
}

/** Meeting capture reuses companion ASR. Mic-only unless this-PC system audio is mixed into local ASR. Never treats TTS as user speech. */
export async function startMeetingSpeech(options: MeetingSpeechOptions): Promise<CompanionSpeechHandle> {
  const listen = resolveMeetingListen(options.listen)
  const probe = listen === 'local' ? await localAsrStatus() : undefined
  const localReady = probe?.supported === true && probe.ready === true
  if (listen === 'local' && !localReady) {
    throw new Error('会议听写选了本机，但 sherpa 未就绪。请改选系统或火山，或先装本机识别。')
  }
  if (listen === 'volc' && !options.volcProviderId) {
    throw new Error('会议听写选了火山，但没有可用的语音模型。请在供应商里配置 seed-asr。')
  }
  const preferLocal = listen === 'local'
  const extraStreams = preferLocal && !options.externalPcm ? options.extraStreams : undefined
  const buffer = createMeetingLineBuffer(line => options.onFinal(line))
  const speechOptions: CompanionSpeechOptions = {
    ...options,
    extraStreams,
    externalPcm: preferLocal ? options.externalPcm : undefined,
    meterless: !preferLocal && listen !== 'volc',
    duplex: true,
    holdUtterance: true,
    spokenText: () => '',
    onFinal: text => buffer.push(text),
    onInterim: text => options.onInterim?.(text),
  }
  const handle = listen === 'volc'
    ? await startVolcCompanionSpeech(speechOptions, options.volcProviderId!)
    : await (preferLocal ? startLocalCompanionSpeech : startCompanionSpeech)(speechOptions)
  const origStop = handle.stop.bind(handle)
  return {
    ...handle,
    flush: async () => {
      await handle.flush?.()
      buffer.flush()
    },
    forceCommit: (fallback?: string) => {
      const ok = handle.forceCommit(fallback)
      buffer.flush()
      return ok
    },
    stop: () => {
      buffer.flush()
      origStop()
    },
    pushPcm: handle.pushPcm,
  }
}

/** Always tries mic + this-PC system audio (Stereo Mix, then entire-screen loopback). Never blocks the meeting on picker cancel. */
export async function prepareMeetingCapture(
  options: CaptureThisPcSystemAudioOptions = {},
): Promise<MeetingCapturePlan> {
  try {
    const stream = await captureThisPcSystemAudio(options)
    if (!hasLiveAudioTrack(stream)) {
      stopMediaStream(stream)
      return { extraStreams: [], audioSource: 'microphone', notice: NO_SYSTEM_AUDIO_NOTICE }
    }
    return { extraStreams: [stream], audioSource: 'microphone_and_system', notice: '' }
  } catch (error) {
    if (isCaptureCanceled(error)) {
      return { extraStreams: [], audioSource: 'microphone', notice: MIC_ONLY_NOTICE }
    }
    const message = error instanceof Error && error.message ? error.message : NO_SYSTEM_AUDIO_NOTICE
    return { extraStreams: [], audioSource: 'microphone', notice: message }
  }
}

/** Re-acquire loopback without dropping the meeting. Picker only when interactive. */
export async function recoverMeetingSystemAudio(
  current: MeetingCapturePlan | undefined,
  options: CaptureThisPcSystemAudioOptions = {},
): Promise<MeetingCapturePlan> {
  if (planHasLiveSystemAudio(current)) return current ?? { extraStreams: [], audioSource: 'microphone', notice: '' }
  const next = await prepareMeetingCapture(options)
  if (planHasLiveSystemAudio(next) && current) {
    current.extraStreams.filter(stream => !next.extraStreams.includes(stream)).forEach(stopMediaStream)
  }
  return next
}

export function audioSourceLabel(source: MeetingAudioSource | string | undefined, live = false): string {
  if (source === 'microphone_and_system') {
    return live ? '正在录制麦克风与系统声音' : '麦克风 + 系统声音（未共享给其他电脑）'
  }
  return live ? '正在录制麦克风' : '麦克风（系统声音未收录时仍可一直录）'
}

export function releaseMeetingCapture(plan: MeetingCapturePlan | undefined): void {
  plan?.extraStreams.forEach(stopMediaStream)
}
