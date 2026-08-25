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
    return { stop: stopCapture, mute: vi.fn(), resume: vi.fn() }
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

  it('drops frames while one is in flight rather than queueing them', async () => {
    // A backlog is the wrong repair. Audio delivered late is transcribed
    // after the user stopped talking, and a queue built during a stutter
    // never drains inside the utterance.
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
    emitFrame(frame())
    await settle()
    expect(bridge.append).toHaveBeenCalledTimes(2)
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

  it('does not strand an engine session when the microphone will not open', async () => {
    // start() already opened a session; failing to capture must not leave it
    // holding the recognizer for a turn that will never happen.
    captureRejects = new Error('permission denied')
    await expect(startLocalAsr({})).rejects.toThrow('permission denied')
    expect(bridge.stop).toHaveBeenCalledWith({ sessionId: 'v1' })
  })

  it('is safe to cancel twice and to finish after cancelling', async () => {
    const handle = await startLocalAsr({})
    handle.cancel()
    handle.cancel()
    await expect(handle.finish()).resolves.toBe('')
    expect(bridge.finish).not.toHaveBeenCalled()
  })
})
