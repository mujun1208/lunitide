import { localAsrStatus } from '../session/companion/localAsr'
import { startLocalCompanionSpeech } from '../session/companion/localSpeech'
import { startCompanionSpeech, type CompanionSpeechHandle, type CompanionSpeechOptions } from '../session/companion/speech'
import { captureThisPcSystemAudio, hasAudioTrack, isCaptureCanceled, stopMediaStream } from './meetingCapture'
import { createMeetingLineBuffer } from './meetingText'

export type MeetingAudioSource = 'microphone' | 'microphone_and_system'

export type MeetingCapturePlan = {
  extraStreams: MediaStream[]
  audioSource: MeetingAudioSource
  notice: string
}

/** Meeting capture reuses companion ASR. Mic-only unless this-PC system audio is mixed into local ASR. Never treats TTS as user speech. */
export async function startMeetingSpeech(options: CompanionSpeechOptions): Promise<CompanionSpeechHandle> {
  const probe = await localAsrStatus()
  const preferLocal = probe?.supported === true && probe.ready === true
  const extraStreams = preferLocal ? options.extraStreams : undefined
  const open = preferLocal ? startLocalCompanionSpeech : startCompanionSpeech
  const buffer = createMeetingLineBuffer(line => options.onFinal(line))
  const handle = await open({
    ...options,
    extraStreams,
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
  }
}

export async function prepareMeetingCapture(includeSystemAudio: boolean): Promise<MeetingCapturePlan> {
  if (!includeSystemAudio) return { extraStreams: [], audioSource: 'microphone', notice: '' }
  const probe = await localAsrStatus()
  if (!(probe?.supported === true && probe.ready === true)) {
    return { extraStreams: [], audioSource: 'microphone', notice: '收录系统声音需要本机识别模型。当前仅转写麦克风。' }
  }
  try {
    const stream = await captureThisPcSystemAudio()
    if (!hasAudioTrack(stream)) {
      stopMediaStream(stream)
      return { extraStreams: [], audioSource: 'microphone', notice: '未获得系统声音（请在选择窗口时勾选共享音频）。已仅转写麦克风。' }
    }
    return { extraStreams: [stream], audioSource: 'microphone_and_system', notice: '' }
  } catch (error) {
    if (isCaptureCanceled(error)) throw error
    const message = error instanceof Error && error.message ? error.message : '未能收录系统声音，已仅转写麦克风。'
    return { extraStreams: [], audioSource: 'microphone', notice: message }
  }
}

export function audioSourceLabel(source: MeetingAudioSource | string | undefined, live = false): string {
  if (source === 'microphone_and_system') {
    return live ? '正在录制本机麦克风和系统声音' : '本机麦克风 + 本机系统声音（未共享给其他电脑）'
  }
  return live ? '正在录制本机麦克风' : '仅本机麦克风，未混录系统扬声器'
}

export function releaseMeetingCapture(plan: MeetingCapturePlan | undefined): void {
  plan?.extraStreams.forEach(stopMediaStream)
}
