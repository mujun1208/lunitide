import { afterEach, describe, expect, test, vi } from 'vitest'
import { startPcmCapture } from '../session/companion/pcmCapture'
import { int16ToBase64 } from '../session/companion/pcmFrames'
import { LIVE_CAPTION_MAX_LINES, MEETING_AUDIO_BATCH_FRAMES, MEETING_AUDIO_MAX_B64, startMeetingAudioRecorder, trimLiveSegments } from './meetingAudio'

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
  afterEach(() => {
    vi.clearAllMocks()
  })

  test('trimLiveSegments keeps only the recent caption window', () => {
    const items = Array.from({ length: LIVE_CAPTION_MAX_LINES + 20 }, (_, i) => i)
    expect(trimLiveSegments(items)).toHaveLength(LIVE_CAPTION_MAX_LINES)
    expect(trimLiveSegments(items)[0]).toBe(20)
  })

  test('batches PCM frames to disk and flushes the tail on stop', async () => {
    const append = vi.fn().mockResolvedValue({ audioMs: 1200 })
    const handle = await startMeetingAudioRecorder({ append })
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

  test('keeps writing after an append failure so the meeting is not the only copy', async () => {
    const append = vi.fn()
      .mockRejectedValueOnce(new Error('Bridge 请求超时'))
      .mockResolvedValue({ audioMs: 2400 })
    const onError = vi.fn()
    await startMeetingAudioRecorder({ append, onError })
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(onError).toHaveBeenCalled())
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  test('never sends a PCM payload over the bridge base64 ceiling', async () => {
    const append = vi.fn().mockImplementation(async () => {
      await new Promise<void>(resolve => { window.setTimeout(resolve, 30) })
      return { audioMs: 1 }
    })
    const handle = await startMeetingAudioRecorder({ append })
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES * 4; i++) emitFrame(frame())
    await handle.stop()
    expect(append.mock.calls.length).toBeGreaterThanOrEqual(4)
    for (const [pcm] of append.mock.calls) {
      expect(typeof pcm).toBe('string')
      expect((pcm as string).length).toBeLessThanOrEqual(MEETING_AUDIO_MAX_B64)
    }
  })

  test('restarts PCM capture on device error without ending the meeting', async () => {
    const append = vi.fn().mockResolvedValue({ audioMs: 1 })
    await startMeetingAudioRecorder({ append })
    expect(startPcmCapture).toHaveBeenCalledOnce()
    failCapture?.(new Error('麦克风已断开'))
    await vi.waitFor(() => expect(vi.mocked(startPcmCapture).mock.calls.length).toBe(2))
    for (let i = 0; i < MEETING_AUDIO_BATCH_FRAMES; i++) emitFrame(frame())
    await vi.waitFor(() => expect(append).toHaveBeenCalled())
  })
})
