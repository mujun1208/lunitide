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
  return plan.extraStreams.some(hasLiveAudioTrack)
}

export function planOwnsEngineLoopback(plan: MeetingCapturePlan | undefined): boolean {
  return plan?.engineOwned === true
}

/**
 * True only when a mic+系统声 recording has a STRUCTURAL absence of any system
 * source — never on momentary silence. Two capture shapes:
 *   • engine-owned WASAPI loopback: there is no browser track to inspect, so the
 *     truth is whether the engine opened a loopback session (active). A live but
 *     quiet session (nobody talking / paused media) is present, not missing.
 *   • browser-owned (getDisplayMedia fallback): missing when no live system-audio
 *     track exists, or the meter confirmed three silent frames on that track.
 * This is what stops the false "只在录麦克风" banner from firing while a song is
 * plainly being transcribed through the engine loopback.
 */
export function meetingSystemAudioMissing(input: {
  recording: boolean
  audioSource: string | undefined
  plan: MeetingCapturePlan | undefined
  engineLoopbackActive: boolean | undefined
  systemHeard: boolean | undefined
}): boolean {
  if (!input.recording) return false
  if ((input.audioSource ?? 'microphone_and_system') !== 'microphone_and_system') return false
  if (planOwnsEngineLoopback(input.plan)) {
    return input.engineLoopbackActive === false
  }
  return input.systemHeard === false || (input.plan !== undefined && !planHasLiveSystemAudio(input.plan))
}

/**
 * Live-caption fallback decision (Issue 3). 云端 (browser Web Speech) opens its
 * own mic and can silently return nothing while the meeting recorder holds the
 * device; 火山 needs a separate seed-asr provider and can also start yet emit
 * nothing. When the selected engine produces no caption within the watchdog
 * window, transparently move the LIVE path to this-PC sherpa (which consumes the
 * same mixed mic+系统声 PCM). The stop-time 补转写 is unaffected either way.
 *   • 'local'       — sherpa is ready; switch the live caption to it.
 *   • 'unavailable' — sherpa not installed; keep trying but tell the user the
 *                     full transcript still lands after stop (补转写).
 *   • 'none'        — a caption already arrived, we already fell back, or the
 *                     engine is already local (its own stall watchdog covers it).
 */
export function shouldFallbackLiveCaption(input: {
  listen: MeetingListen | undefined
  sawRealCaption: boolean
  alreadyFellBack: boolean
  localReady: boolean
}): 'local' | 'unavailable' | 'none' {
  if (input.sawRealCaption || input.alreadyFellBack) return 'none'
  if (input.listen === 'local') return 'none'
  return input.localReady ? 'local' : 'unavailable'
}

/** Live ASR diagnostics surfaced on the meeting workbench: which engine is
 *  really running (after any deaf-engine fallback), which Volc provider it dialed,
 *  whether the recorder's external PCM is the single audio source (the anti
 *  double-mic guarantee), and whether captions fell back to this-PC sherpa. */
export type MeetingAsrRuntime = {
  backend: MeetingListen
  providerId?: string
  externalPcm: boolean
  fellBack: boolean
}

export function meetingAsrRuntimeLine(runtime: MeetingAsrRuntime): string {
  const engine = runtime.backend === 'volc' ? '火山 seed-asr' : runtime.backend === 'local' ? '本机 sherpa' : '系统听写'
  const parts = [`引擎：${engine}`]
  if (runtime.backend === 'volc' && runtime.providerId) parts.push(`供应商：${runtime.providerId.slice(0, 8)}…`)
  const source = runtime.externalPcm
    ? '外部录音 PCM（单路，无双麦）'
    : runtime.backend === 'cloud'
      ? '浏览器听写'
      : '引擎麦克风'
  parts.push(`音源：${source}`)
  parts.push(`字幕：${runtime.fellBack ? '已回退本机' : '直采'}`)
  return parts.join(' · ')
}

/** Three energy frames vs three silent frames. Undefined = keep the last label. */
export function noteLoopbackEnergy(prev: { hits: number; zeros: number }, peak: number): { hits: number; zeros: number; heard?: boolean } {
  if (peak > 0) {
    const hits = prev.hits + 1
    return { hits, zeros: 0, heard: hits >= 3 ? true : undefined }
  }
  const zeros = prev.zeros + 1
  return { hits: 0, zeros, heard: zeros >= 3 ? false : undefined }
}

export function captureStateNotice(plan: MeetingCapturePlan | undefined): string {
  if (!plan) return ''
  if (planHasLiveSystemAudio(plan)) return ''
  if (planOwnsEngineLoopback(plan)) return plan.notice
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
  // Both PCM-capable recognizers (本机 sherpa, 火山 volc) can take the meeting
  // recorder's already-mixed mic+系统声 PCM, or open their own capture over the
  // this-PC extra streams. Only 云端 Web Speech cannot: it captures its own mic
  // inside the browser engine and exposes no way to inject loopback, so its
  // live caption is mic-only (系统声 still lands in the WAV and the stop-time
  // sherpa 补转写). Routing volc through the same path is what finally lets the
  // 火山 live caption hear the other party, not just the local user.
  const pcmCapable = listen === 'local' || listen === 'volc'
  const usesExternalPcm = pcmCapable && options.externalPcm === true
  const extraStreams = pcmCapable && !usesExternalPcm ? options.extraStreams : undefined
  const buffer = createMeetingLineBuffer(line => options.onFinal(line))
  const speechOptions: CompanionSpeechOptions = {
    ...options,
    extraStreams,
    externalPcm: usesExternalPcm ? options.externalPcm : undefined,
    meterless: listen === 'cloud',
    duplex: true,
    holdUtterance: true,
    spokenText: () => '',
    onFinal: text => buffer.push(text),
    onInterim: text => options.onInterim?.(text),
  }
  const handle = listen === 'volc'
    ? await startVolcCompanionSpeech(speechOptions, options.volcProviderId!)
    : await (listen === 'local' ? startLocalCompanionSpeech : startCompanionSpeech)(speechOptions)
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
    if (planOwnsEngineLoopback(current)) return { ...next, engineOwned: true }
  }
  if (current && planOwnsEngineLoopback(current) && !planHasLiveSystemAudio(next)) return current
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
