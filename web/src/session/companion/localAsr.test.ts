import { beforeEach, describe, expect, it, vi } from 'vitest'

const bridge = {
  status: vi.fn(),
  install: vi.fn(),
  start: vi.fn(),
  append: vi.fn(),
  finish: vi.fn(),
  stop: vi.fn(),
}

type FrameSink = (frame: { base64: string; samples: Int16Array; peak: number }) => void
let emitFrame: FrameSink = () => {}
let failCapture: ((error: Error) => void) | undefined
const stopCapture = vi.fn()
const flushCapture = vi.fn()
let captureRejects: Error | undefined

vi.mock('../../bridge/client', () => ({
  getVoiceBridge: () => bridge,
  BridgeClientError: class extends Error {},
}))

vi.mock('./pcmCapture', () => ({
  startPcmCapture: vi.fn(async (options: { onFrame: FrameSink; onError?: (e: Error) => void }) => {
    if (captureRejects) throw captureRejects
    emitFrame = options.onFrame
    failCapture = options.onError
    return { stop: stopCapture, mute: vi.fn(), resume: vi.fn(), flush: flushCapture, setMuted: vi.fn() }
  }),
}))

const { installLocalAsr, localAsrStatus, startLocalAsr } = await import('./localAsr')

const frame = (peak = 0.2) => ({ base64: 'AAAA', samples: new Int16Array(1600), peak })
const settle = () => new Promise(resolve => setTimeout(resolve, 0))

beforeEach(() => {
  vi.clearAllMocks()
  captureRejects = undefined
  bridge.start.mockResolvedValue({ sessionId: 'v1' })
  bridge.append.mockResolvedValue({ text: '', final: false })
  bridge.finish.mockResolvedValue({ text: '' })
  bridge.stop.mockResolvedValue({ notice: 'VOICE_SESSION_CLOSED' })
})

describe('localAsrStatus', () => {
  it('reports what the engine says', async () => {
    bridge.status.mockResolvedValue({ supported: true, ready: false, downloadBytes: 250_000_000 })
    await expect(localAsrStatus()).resolves.toMatchObject({ supported: true, ready: false })
  })

  it('treats an engine without these methods as simply unavailable', async () => {
    // An older engine, or a bridge that is not up yet. The caller falls back
    // to Web Speech, so this must not throw into the render path.
    bridge.status.mockRejectedValue(new Error('BRIDGE_METHOD_UNKNOWN'))
    await expect(localAsrStatus()).resolves.toBeUndefined()
  })
})

describe('installLocalAsr', () => {
  it('passes a chosen model through and omits it otherwise', async () => {
    bridge.install.mockResolvedValue({ state: 'downloading', percent: 3, doneBytes: 1, totalBytes: 2 })
    await installLocalAsr('streaming-zipformer-zh-14m')
    expect(bridge.install).toHaveBeenCalledWith({ modelId: 'streaming-zipformer-zh-14m' })
    await installLocalAsr()
    expect(bridge.install).toHaveBeenLastCalledWith({})
  })
})

describe('startLocalAsr', () => {
  it('streams frames and surfaces partials as they arrive', async () => {
    const onTranscript = vi.fn()
    bridge.append.mockResolvedValueOnce({ text: '今天', final: false })
    bridge.append.mockResolvedValueOnce({ text: '今天天气', final: false })

    const handle = await startLocalAsr({ onTranscript })
    emitFrame(frame())
    await settle()
    emitFrame(frame())
    await settle()

    expect(bridge.append).toHaveBeenCalledWith({ sessionId: 'v1', pcm: 'AAAA' })
    expect(onTranscript).toHaveBeenNthCalledWith(1, '今天', false)
    expect(onTranscript).toHaveBeenNthCalledWith(2, '今天天气', false)

    bridge.finish.mockResolvedValue({ text: '今天天气怎么样' })
    await expect(handle.finish()).resolves.toBe('今天天气怎么样')
    expect(stopCapture).toHaveBeenCalled()
  })

  it('keeps audio captured while a request is in flight instead of dropping it', async () => {
    // Frames used to be discarded whenever a request was outstanding. A
    // discarded frame is a tenth of a second the recognizer never hears, and
    // it came back as the missing characters in the middle of a sentence —
    // so what arrives during a round trip is queued and sent behind it.
    let release: (value: { text: string; final: boolean }) => void = () => {}
    bridge.append.mockReturnValueOnce(new Promise(resolve => { release = resolve }))

    await startLocalAsr({})
    emitFrame(frame())
    emitFrame(frame())
    emitFrame(frame())
    await settle()
    expect(bridge.append).toHaveBeenCalledTimes(1)

    release({ text: 'x', final: false })
    await settle()

    // The two frames held back go out together, carrying both frames' samples
    // rather than one frame's worth.
    expect(bridge.append).toHaveBeenCalledTimes(2)
    const second = bridge.append.mock.calls[1]![0] as { pcm: string }
    const single = bridge.append.mock.calls[0]![0] as { pcm: string }
    expect(second.pcm.length).toBeGreaterThan(single.pcm.length)
  })

  it('sends the part of the sentence still held back before ending the turn', async () => {
    // The capture accumulator keeps whatever did not fill a whole frame, which
    // is the last fraction of a second of speech. Nothing else asks for it, so
    // without this the final syllable of every utterance was discarded —
    // 「你好月汐」 came back as 「你好」.
    const handle = await startLocalAsr({})
    emitFrame(frame())
    await settle()

    bridge.finish.mockResolvedValue({ text: '你好月汐' })
    await expect(handle.commit()).resolves.toBe('你好月汐')
    expect(flushCapture).toHaveBeenCalled()
  })

  it('loses one sentence, not the microphone, when a turn fails to transcribe', async () => {
    // This used to run through the same path as a dead engine: one failed
    // commit stopped capture for good, so the first turn worked and every
    // turn after it was met with silence.
    const onError = vi.fn()
    const onTranscriptLost = vi.fn()
    const handle = await startLocalAsr({ onError, onTranscriptLost })
    emitFrame(frame())
    await settle()

    bridge.finish.mockRejectedValueOnce(new Error('decode timed out'))
    bridge.start.mockResolvedValueOnce({ sessionId: 'v2' })
    await expect(handle.commit()).resolves.toBe('')
    expect(onTranscriptLost).toHaveBeenCalledTimes(1)
    expect(onError).not.toHaveBeenCalled()
    expect(stopCapture).not.toHaveBeenCalled()

    // The next sentence still reaches the engine, on the session opened
    // alongside the failed one.
    emitFrame(frame())
    await settle()
    expect(bridge.append).toHaveBeenLastCalledWith({ sessionId: 'v2', pcm: 'AAAA' })
  })

  it('does not retire a session that was never given any audio', async () => {
    // The turn boundary at the start and end of every reply lands here with a
    // muted microphone. Retiring a session that heard silence cost two round
    // trips and asked the recognizer to decode an empty utterance.
    const handle = await startLocalAsr({})
    await expect(handle.commit()).resolves.toBe('')
    expect(bridge.finish).not.toHaveBeenCalled()
    expect(bridge.start).toHaveBeenCalledTimes(1)
  })

  it('reports the microphone level for every frame, dropped or not', async () => {
    const onLevel = vi.fn()
    bridge.append.mockReturnValue(new Promise(() => {}))

    await startLocalAsr({ onLevel })
    emitFrame(frame(0.1))
    emitFrame(frame(0.5))
    await settle()

    // The meter must keep moving even while frames are being dropped, or the
    // UI looks frozen exactly when the engine is struggling.
    expect(onLevel).toHaveBeenCalledTimes(2)
    expect(onLevel).toHaveBeenLastCalledWith(0.5)
  })

  it('closes the engine session when a frame fails', async () => {
    const onError = vi.fn()
    bridge.append.mockRejectedValue(new Error('engine died'))

    await startLocalAsr({ onError })
    emitFrame(frame())
    await settle()

    expect(onError).toHaveBeenCalledTimes(1)
    expect(bridge.stop).toHaveBeenCalledWith({ sessionId: 'v1' })
    expect(stopCapture).toHaveBeenCalled()
  })

  it('reports a lost microphone once, not once per frame', async () => {
    const onError = vi.fn()
    await startLocalAsr({ onError })
    failCapture?.(new Error('microphone unplugged'))
    failCapture?.(new Error('microphone unplugged'))
    await settle()
    expect(onError).toHaveBeenCalledTimes(1)
  })

  it('opens no engine session at all when the microphone will not open', async () => {
    // The microphone is acquired first, so a refused permission costs nothing
    // on the engine side. It used to be the other way round, which both left
    // a session to clean up here and, in the normal case, kept the microphone
    // shut for however long the engine took to load its model.
    captureRejects = new Error('permission denied')
    await expect(startLocalAsr({})).rejects.toThrow('permission denied')
    expect(bridge.start).not.toHaveBeenCalled()
    expect(bridge.stop).not.toHaveBeenCalled()
  })

  it('keeps what was said while the engine was still loading its model', async () => {
    // Opening a session can mean waiting seconds for a model to load. The
    // microphone is already running by then, and the user is already talking
    // — 「我上来说话说了好几次，他都没反应」 was this window, when nothing
    // was recording at all.
    let openSession: (value: { sessionId: string }) => void = () => {}
    bridge.start.mockReturnValueOnce(new Promise(resolve => { openSession = resolve }))

    const starting = startLocalAsr({})
    await settle()
    emitFrame(frame())
    emitFrame(frame())
    await settle()
    expect(bridge.append).not.toHaveBeenCalled()

    openSession({ sessionId: 'v1' })
    await starting
    await settle()

    // Both frames reach the recognizer, joined into the first request.
    expect(bridge.append).toHaveBeenCalledTimes(1)
    expect(bridge.append.mock.calls[0]![0]).toMatchObject({ sessionId: 'v1' })
  })

  it('mixes extra this-PC streams into capture', async () => {
    const extra = { getAudioTracks: () => [{ kind: 'audio' }] } as unknown as MediaStream
    await startLocalAsr({ extraStreams: [extra] })
    const { startPcmCapture } = await import('./pcmCapture')
    expect(startPcmCapture).toHaveBeenCalledWith(expect.objectContaining({ extraStreams: [extra] }))
  })

  it('is safe to cancel twice and to finish after cancelling', async () => {
    const handle = await startLocalAsr({})
    handle.cancel()
    handle.cancel()
    await expect(handle.finish()).resolves.toBe('')
    expect(bridge.finish).not.toHaveBeenCalled()
  })
})
