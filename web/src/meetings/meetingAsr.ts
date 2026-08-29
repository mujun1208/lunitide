import { localAsrStatus } from '../session/companion/localAsr'
import { startLocalCompanionSpeech } from '../session/companion/localSpeech'
import { startCompanionSpeech, type CompanionSpeechHandle, type CompanionSpeechOptions } from '../session/companion/speech'
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
}

const MIC_ONLY_NOTICE = '未能收录系统声音，已继续录制麦克风。'

export function planHasLiveSystemAudio(plan: MeetingCapturePlan | undefined): boolean {
  return !!plan && plan.audioSource === 'microphone_and_system' && plan.extraStreams.some(hasLiveAudioTrack)
}

export function captureStateNotice(plan: MeetingCapturePlan | undefined): string {
  if (!plan) return ''
  if (planHasLiveSystemAudio(plan)) return ''
  if (plan.audioSource === 'microphone_and_system' && !plan.extraStreams.some(hasLiveAudioTrack)) {
    return '系统声音轨道已中断，正在重试收录。当前仅转写麦克风。'
  }
  return plan.notice
}

/** Meeting capture reuses companion ASR. Mic-only unless this-PC system audio is mixed into local ASR. Never treats TTS as user speech. */
export async function startMeetingSpeech(options: CompanionSpeechOptions): Promise<CompanionSpeechHandle> {
  const probe = await localAsrStatus()
  const preferLocal = probe?.supported === true && probe.ready === true
  const extraStreams = preferLocal && !options.externalPcm ? options.extraStreams : undefined
  const open = preferLocal ? startLocalCompanionSpeech : startCompanionSpeech
  const buffer = createMeetingLineBuffer(line => options.onFinal(line))
  const handle = await open({
    ...options,
    extraStreams,
    externalPcm: preferLocal ? options.externalPcm : undefined,
    meterless: !preferLocal,
    duplex: true,
    holdUtterance: true,
    spokenText: () => '',
    onFinal: text => buffer.push(text),
    onInterim: text => options.onInterim?.(text),
  })
  const origStop = handle.stop.bind(handle)
  return {
    ...handle,
    flush: async () => {
      await handle.flush?.()
      buffer.flush()
    },
    forceCommit: () => {
      handle.forceCommit()
      buffer.flush()
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
