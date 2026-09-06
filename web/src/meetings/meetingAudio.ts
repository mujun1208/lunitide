import { MeetingAudioQueue } from './meetingAudioQueue'
import { startPcmCapture, type PcmCaptureHandle } from '../session/companion/pcmCapture'

/** 1.2s of 16 kHz PCM, under the 65536-char base64 ceiling. */
export const MEETING_AUDIO_BATCH_FRAMES = 12
/** Bridge schema maxLength for meetings.audio.append pcm (base64 characters). */
export const MEETING_AUDIO_MAX_B64 = 65536
export const ASR_INTERRUPTED_NOTICE = '录制中，实时转写中断可补。停止后的补转写只用本机识别。'
export const LIVE_CAPTION_MAX_LINES = 80

export function trimLiveSegments<T>(items: T[], max = LIVE_CAPTION_MAX_LINES): T[] {
  return items.length <= max ? items : items.slice(-max)
}

export type MeetingAudioBatch = {
  captureSessionId: string
  chunkSeq: number
  sampleStart: number
  sampleCount: number
  digest: string
}

export function verifyMeetingAudioAck(ack: unknown, batch: MeetingAudioBatch): void {
  if (!ack || typeof ack !== 'object' || Object.entries(batch).some(([key, value]) => (ack as Record<string, unknown>)[key] !== value)) {
    throw new Error('音频保存确认不匹配，正在保留原批次重试')
  }
}

export type MeetingPcmFrame = { base64: string; samples: Int16Array; peak: number }

export type MeetingAudioHandle = {
  stop: () => Promise<void>
  flush: () => Promise<void>
  attachExtraStream: (stream: MediaStream) => void
}

export async function startMeetingAudioRecorder(options: {
  meetingId: string
  extraStreams?: MediaStream[]
  append: (pcm: string, batch: MeetingAudioBatch) => Promise<unknown>
  onFrame?: (frame: MeetingPcmFrame) => void
  onError?: (error: Error) => void
  onExtraEnded?: () => void
}): Promise<MeetingAudioHandle> {
  const queue = await MeetingAudioQueue.open()
  let closed = false
  let stopPromise: Promise<void> | undefined
  let recycling = false
  let inFlight: Promise<void> | undefined
  let writing: Promise<void> | undefined
  let retryAt = 0
  let retryTimer: number | undefined
  let writeTimer: number | undefined
  let empty = false
  let drained = false
  let durableVersion = 0
  const captureSessionId = crypto.randomUUID()
  const unsaved: Int16Array[] = []
  const extras = [...(options.extraStreams ?? [])]
  let capture: PcmCaptureHandle | undefined
  const report = (error: unknown) => options.onError?.(error instanceof Error ? error : new Error(String(error)))

  const pump = (all = false): Promise<void> => {
    if (inFlight) return inFlight
    if (Date.now() < retryAt) return Promise.resolve()
    const version = durableVersion
    inFlight = (async () => {
      const batch = await queue.next(options.meetingId, captureSessionId, all)
      empty = !batch && version === durableVersion
      drained = all && empty
      if (!batch) return
      const ack = await options.append(batch.pcm, batch.identity)
      await queue.acknowledge(batch, ack)
      retryAt = 0
    })().catch(error => {
      empty = false
      report(error)
      retryAt = Date.now() + 500
    }).finally(() => {
      inFlight = undefined
      if (!closed && !empty) {
        window.clearTimeout(retryTimer)
        retryTimer = window.setTimeout(() => { void pump() }, Math.max(0, retryAt - Date.now()))
      }
    })
    return inFlight
  }

  const saveFrames = (): Promise<void> => {
    if (writing) return writing
    writing = (async () => {
      while (unsaved.length) {
        await queue.enqueue(options.meetingId, captureSessionId, unsaved[0])
        unsaved.shift()
        durableVersion++
        drained = false
        empty = false
        void pump()
      }
    })().catch(error => {
      // Stop collecting on local storage failure. Keep the failed frame in
      // memory for retry, and keep all committed PCM across renderer restarts.
      closed = true
      const owned = capture; capture = undefined
      void owned?.stop()
      report(new Error(`无法保存本机录音，已停止采集；请释放磁盘空间后重试停止。${String(error)}`))
    }).finally(() => {
      writing = undefined
      if (unsaved.length) writeTimer = window.setTimeout(() => { void saveFrames() }, 500)
    })
    return writing
  }

  const drain = async () => {
    const until = Date.now() + 120_000
    while (Date.now() < until) {
      if (!writing && unsaved.length) void saveFrames()
      if (!writing && unsaved.length === 0) {
        if (!inFlight && drained) return
        void pump(true)
      }
      await new Promise<void>(resolve => { window.setTimeout(resolve, 20) })
    }
    throw new Error('录音已停止，仍有音频未确认保存。本机队列会在重新打开会议后继续恢复，也可再次点击停止重试。')
  }

  const boot = async () => {
    const opened = await startPcmCapture({
      extraStreams: extras,
      onFrame: frame => {
        if (closed) return
        // Copy before the device reuses its buffer. Persist even short tails.
        unsaved.push(frame.samples.slice())
        empty = false
        drained = false
        void saveFrames()
        options.onFrame?.(frame)
      },
      onError: error => {
        if (closed) return
        report(error)
        void recycle()
      },
      onExtraEnded: () => { if (!closed) options.onExtraEnded?.() },
    })
    if (closed) await opened.stop()
    else capture = opened
  }

  const recycle = async () => {
    if (closed || recycling) return
    recycling = true
    try {
      await capture?.stop()
      capture = undefined
      if (!closed) await boot()
    } catch (error) {
      report(error)
      if (!closed) window.setTimeout(() => { void recycle() }, 1200)
    } finally { recycling = false }
  }

  // Replays old capture identities before newly captured frames; a missing
  // ACK after backend commit therefore cannot duplicate the audio on restart.
  void pump(true)
  try { await boot() } catch (error) { closed = true; queue.close(); throw error }

  return {
    attachExtraStream: stream => {
      if (closed) return
      if (!extras.includes(stream)) extras.push(stream)
      capture?.attachExtraStream(stream)
    },
    flush: async () => { capture?.flush(); drained = false; await drain() },
    stop: () => {
      if (stopPromise) return stopPromise
      capture?.flush()
      closed = true
      drained = false
      window.clearTimeout(retryTimer)
      const ownedCapture = capture
      capture = undefined
      const release = ownedCapture?.stop()
      stopPromise = (async () => {
        await release
        await drain()
        window.clearTimeout(writeTimer)
        queue.close()
      })().catch(error => { stopPromise = undefined; throw error })
      return stopPromise
    },
  }
}
