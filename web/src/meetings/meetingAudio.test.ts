import { webcrypto } from 'node:crypto'
import { IDBFactory } from 'fake-indexeddb'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { startPcmCapture } from '../session/companion/pcmCapture'
import { int16ToBase64 } from '../session/companion/pcmFrames'
import { LIVE_CAPTION_MAX_LINES, MEETING_AUDIO_BATCH_FRAMES, MEETING_AUDIO_MAX_B64, startMeetingAudioRecorder, trimLiveSegments, verifyMeetingAudioAck } from './meetingAudio'

type FrameSink = (frame: { base64: string; samples: Int16Array; peak: number }) => void
type CaptureOpts = { onFrame: FrameSink; onError?: (error: Error) => void }
let emitFrame: FrameSink = () => {}
let failCapture: ((error: Error) => void) | undefined
const stopCapture = vi.fn()
const flushCapture = vi.fn()
const attachExtra = vi.fn()

vi.mock('../session/companion/pcmCapture', () => ({
  startPcmCapture: vi.fn(async (options: CaptureOpts) => {
    emitFrame = options.onFrame
    failCapture = options.onError
    return { stop: stopCapture, flush: flushCapture, setMuted: vi.fn(), attachExtraStream: attachExtra }
  }),
}))

const frame = () => {
  const samples = new Int16Array(1600)
  return { base64: int16ToBase64(samples), samples, peak: 0.2 }
}

describe('meetingAudio', () => {
  beforeEach(() => { vi.stubGlobal('crypto', webcrypto); vi.stubGlobal('indexedDB', new IDBFactory()) })
  afterEach(() => {
    vi.clearAllMocks()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  test('stops capture immediately during a blocked write and drains only the frozen tail', async () => {
    let release!: () => void
    const append = vi.fn().mockImplementationOnce((_pcm, batch) => new Promise(resolve => { release = () => resolve(batch) })).mockImplementation(async (_pcm, batch) => batch)
    const handle = await startMeetingAudioRecorder({ meetingId: 'test-meeting', append })
    for (let i = 0; i < 13; i++) emitFrame(frame())
    const stopping = handle.stop()
    expect(stopCapture).toHaveBeenCalledTimes(1)
    expect(handle.stop()).toBe(stopping)
    for (let i = 0; i < 24; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append).toHaveBeenCalledTimes(1))
    expect(append).toHaveBeenCalledTimes(1)
    release()
    await stopping
    expect(append).toHaveBeenCalledTimes(2)
    expect(append.mock.calls.map(([pcm]) => atob(pcm).length)).toEqual([38400, 3200])
  })

  test('trimLiveSegments keeps only the recent caption window', () => {
    const items = Array.from({ length: LIVE_CAPTION_MAX_LINES + 20 }, (_, i) => i)
    expect(trimLiveSegments(items)).toHaveLength(LIVE_CAPTION_MAX_LINES)
    expect(trimLiveSegments(items)[0]).toBe(20)
  })

  test('a timed-out stop retains the unacknowledged batch for another stop attempt', async () => {
    let release!: () => void
    const append = vi.fn().mockImplementation((_pcm, batch) => new Promise(resolve => { release = () => resolve(batch) }))
    const handle = await startMeetingAudioRecorder({ meetingId: 'test-meeting', append })
    for (let i = 0; i < 12; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append).toHaveBeenCalledOnce())
    vi.useFakeTimers()
    const stopping = handle.stop()
    const rejected = expect(stopping).rejects.toThrow('仍有音频未确认保存')
    await vi.advanceTimersByTimeAsync(120_000)
    await rejected
    expect(stopCapture).toHaveBeenCalledOnce()
    vi.useRealTimers()
    release()
    await handle.stop()
    expect(append).toHaveBeenCalledOnce()
  })

  test('rejects a confirmation belonging to another batch', () => {
    const batch = { captureSessionId: 'capture_session_12345', chunkSeq: 0, sampleStart: 0, sampleCount: 1600, digest: 'a'.repeat(64) }
    expect(() => verifyMeetingAudioAck({ ...batch, audioMs: 100 }, batch)).not.toThrow()
    expect(() => verifyMeetingAudioAck({ ...batch, chunkSeq: 1 }, batch)).toThrow('确认不匹配')
    expect(() => verifyMeetingAudioAck({ audioMs: 100 }, batch)).toThrow('确认不匹配')
  })

  test('batches PCM frames to disk and flushes the tail on stop', async () => {
    const append = vi.fn().mockImplementation(async (_pcm, batch) => batch)
    const handle = await startMeetingAudioRecorder({ meetingId: 'test-meeting', append })
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append).toHaveBeenCalled())
    emitFrame(frame())
    await handle.stop()
    expect(flushCapture).toHaveBeenCalled()
    expect(stopCapture).toHaveBeenCalled()
    expect(append.mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(typeof append.mock.calls[0][0]).toBe('string')
    expect((append.mock.calls[0][0] as string).length).toBeGreaterThan(8)
  })

  test('retries the same audio identity before sending subsequent samples', async () => {
    const append = vi.fn()
      .mockRejectedValueOnce(new Error('Bridge 请求超时'))
      .mockImplementation(async (_pcm, batch) => batch)
    const onError = vi.fn()
    await startMeetingAudioRecorder({ meetingId: 'test-meeting', append, onError })
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(onError).toHaveBeenCalled())
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append.mock.calls.length).toBeGreaterThanOrEqual(3))
    expect(append.mock.calls[1]).toEqual(append.mock.calls[0])
    expect(append.mock.calls[2][1]).toMatchObject({ chunkSeq: 1, sampleStart: 19200, sampleCount: 19200 })
  })

  test('never sends a PCM payload over the bridge base64 ceiling', async () => {
    const append = vi.fn().mockImplementation(async (_pcm, batch) => {
      await new Promise<void>(resolve => { window.setTimeout(resolve, 30) })
      return batch
    })
    const handle = await startMeetingAudioRecorder({ meetingId: 'test-meeting', append })
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES * 4; i++) emitFrame(frame())
    await handle.stop()
    expect(append.mock.calls.length).toBeGreaterThanOrEqual(4)
    for (const [pcm] of append.mock.calls) {
      expect(typeof pcm).toBe('string')
      expect((pcm as string).length).toBeLessThanOrEqual(MEETING_AUDIO_MAX_B64)
    }
  })

  test('restarts PCM capture on device error without ending the meeting', async () => {
    const append = vi.fn().mockImplementation(async (_pcm, batch) => batch)
    await startMeetingAudioRecorder({ meetingId: 'test-meeting', append })
    expect(startPcmCapture).toHaveBeenCalledOnce()
    failCapture?.(new Error('麦克风已断开'))
    await vi.waitFor(() => expect(vi.mocked(startPcmCapture).mock.calls.length).toBe(2))
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append).toHaveBeenCalled())
  })
})
