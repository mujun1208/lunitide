import { int16ToBase64 } from '../session/companion/pcmFrames'
import { startPcmCapture, type PcmCaptureHandle } from '../session/companion/pcmCapture'

/** 1.2s of 16 kHz PCM, under the 65536-char base64 ceiling. */
export const MEETING_AUDIO_BATCH_FRAMES = 12
/** Bridge schema maxLength for meetings.audio.append pcm (base64 characters). */
export const MEETING_AUDIO_MAX_B64 = 65536
export const ASR_INTERRUPTED_NOTICE = '录制中，实时转写中断可补，停止后会补转写'
export const LIVE_CAPTION_MAX_LINES = 80

export function trimLiveSegments<T>(items: T[], max = LIVE_CAPTION_MAX_LINES): T[] {
  return items.length <= max ? items : items.slice(-max)
}

export type MeetingPcmFrame = { base64: string; samples: Int16Array; peak: number }

export type MeetingAudioHandle = {
  stop: () => Promise<void>
  flush: () => Promise<void>
  attachExtraStream: (stream: MediaStream) => void
}

export async function startMeetingAudioRecorder(options: {
  extraStreams?: MediaStream[]
  append: (pcm: string) => Promise<unknown>
  onFrame?: (frame: MeetingPcmFrame) => void
  onError?: (error: Error) => void
  onExtraEnded?: () => void
}): Promise<MeetingAudioHandle> {
  let closed = false
  let recycling = false
  let inFlight = false
  let pending: Int16Array[] = []
  let pendingSamples = 0
  const extras = [...(options.extraStreams ?? [])]
  const frameSamples = 1600
  const batchSamples = frameSamples * MEETING_AUDIO_BATCH_FRAMES

  const takeBatch = (all: boolean): string | undefined => {
    if (pending.length === 0) return undefined
    const takeSamples = all ? Math.min(pendingSamples, batchSamples) : batchSamples
    if (pendingSamples < takeSamples) return undefined
    const merged = new Int16Array(takeSamples)
    let at = 0
    while (at < takeSamples && pending.length > 0) {
      const chunk = pending[0]
      const room = takeSamples - at
      if (chunk.length <= room) {
        merged.set(chunk, at)
        at += chunk.length
        pending.shift()
      } else {
        merged.set(chunk.subarray(0, room), at)
        pending[0] = chunk.subarray(room)
        at += room
      }
    }
    pendingSamples -= at
    return int16ToBase64(merged.subarray(0, at))
  }

  const pump = (): Promise<void> => {
    if (closed || inFlight) return Promise.resolve()
    const pcm = takeBatch(false)
    if (!pcm) return Promise.resolve()
    inFlight = true
    void options.append(pcm).catch(error => {
      options.onError?.(error instanceof Error ? error : new Error(String(error)))
    }).finally(() => {
      inFlight = false
      void pump()
    })
    return Promise.resolve()
  }

  const drain = async () => {
    const until = Date.now() + 120_000
    while (Date.now() < until) {
      if (!inFlight && pending.length === 0) return
      if (!inFlight) {
        const pcm = takeBatch(true)
        if (!pcm) return
        inFlight = true
        try {
          await options.append(pcm)
        } catch (error) {
          options.onError?.(error instanceof Error ? error : new Error(String(error)))
        } finally {
          inFlight = false
        }
        continue
      }
      await new Promise<void>(resolve => { window.setTimeout(resolve, 20) })
    }
  }

  let capture: PcmCaptureHandle | undefined

  const boot = async () => {
    capture = await startPcmCapture({
      extraStreams: extras,
      onFrame: frame => {
        if (closed) return
        options.onFrame?.(frame)
        pending.push(frame.samples)
        pendingSamples += frame.samples.length
        void pump()
      },
      onError: error => {
        if (closed) return
        options.onError?.(error)
        void recycle()
      },
      onExtraEnded: () => {
        if (closed) return
        options.onExtraEnded?.()
      },
    })
  }

  const recycle = async () => {
    if (closed || recycling) return
    recycling = true
    try {
      await capture?.stop()
      capture = undefined
      if (closed) return
      await boot()
    } catch (error) {
      options.onError?.(error instanceof Error ? error : new Error(String(error)))
      if (!closed) {
        window.setTimeout(() => { void recycle() }, 1_200)
      }
    } finally {
      recycling = false
    }
  }

  await boot()

  return {
    attachExtraStream: stream => {
      if (closed) return
      if (!extras.includes(stream)) extras.push(stream)
      capture?.attachExtraStream(stream)
    },
    flush: async () => {
      capture?.flush()
      await drain()
    },
    stop: async () => {
      if (closed) return
      capture?.flush()
      await drain()
      closed = true
      pending = []
      pendingSamples = 0
      await capture?.stop()
      capture = undefined
    },
  }
}
