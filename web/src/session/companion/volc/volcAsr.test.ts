import { beforeEach, describe, expect, it, vi } from 'vitest'

const bridge = {
  start: vi.fn(),
  append: vi.fn(),
  finish: vi.fn(),
  stop: vi.fn(),
}

type FrameSink = (frame: { base64: string; samples: Int16Array; peak: number }) => void
let emitFrame: FrameSink = () => {}
const stopCapture = vi.fn()
const flushCapture = vi.fn()

vi.mock('../../../bridge/client', () => ({
  getVoiceBridge: () => bridge,
}))

vi.mock('../pcmCapture', () => ({
  startPcmCapture: vi.fn(async (options: { onFrame: FrameSink; onError?: (e: Error) => void }) => {
    emitFrame = options.onFrame
    return { stop: stopCapture, mute: vi.fn(), resume: vi.fn(), flush: flushCapture, setMuted: vi.fn() }
  }),
}))

const { startPcmCapture } = await import('../pcmCapture')
const { startVolcAsr, VOLC_EXTERNAL_PCM_RESCUE_MS } = await import('./volcAsr')

const PROVIDER = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const frame = (peak = 0.2) => ({ base64: 'AAAA', samples: new Int16Array(1600), peak })
const settle = () => new Promise(resolve => setTimeout(resolve, 0))

beforeEach(() => {
  vi.clearAllMocks()
  bridge.start.mockResolvedValue({ sessionId: 'v1' })
  bridge.append.mockResolvedValue({ text: '', final: false })
  bridge.finish.mockResolvedValue({ text: '' })
  bridge.stop.mockResolvedValue({ notice: 'VOICE_SESSION_CLOSED' })
})

describe('startVolcAsr', () => {
  it('opens a volc session with the chosen provider', async () => {
    await startVolcAsr(PROVIDER)
    expect(bridge.start).toHaveBeenCalledWith({ language: 'zh-CN', backend: 'volc', providerId: PROVIDER })
  })

  it('forwards an explicit endWindowMs for meeting VAD', async () => {
    await startVolcAsr(PROVIDER, { endWindowMs: 400 })
    expect(bridge.start).toHaveBeenCalledWith({
      language: 'zh-CN',
      backend: 'volc',
      providerId: PROVIDER,
      endWindowMs: 400,
    })
  })

  it('commit drains the last transcript without finishing the websocket', async () => {
    const handle = await startVolcAsr(PROVIDER)
    bridge.append.mockResolvedValue({ text: '你好月汐', final: true })
    emitFrame(frame())
    await settle()
    await expect(handle.commit()).resolves.toBe('你好月汐')
    expect(flushCapture).toHaveBeenCalled()
    expect(bridge.finish).not.toHaveBeenCalled()
    expect(bridge.start).toHaveBeenCalledTimes(1)
    expect(bridge.stop).not.toHaveBeenCalled()
  })

  it('does not re-emit the same definite after commit', async () => {
    const onTranscript = vi.fn()
    const handle = await startVolcAsr(PROVIDER, { onTranscript })
    bridge.append.mockResolvedValue({ text: '你好月汐', final: true })
    emitFrame(frame())
    await settle()
    await handle.commit()
    onTranscript.mockClear()
    emitFrame(frame())
    await settle()
    expect(onTranscript).not.toHaveBeenCalled()
    expect(bridge.start).toHaveBeenCalledTimes(1)
    expect(bridge.finish).not.toHaveBeenCalled()
  })

  it('does not recycle the session after a long stretch of audio', async () => {
    const handle = await startVolcAsr(PROVIDER)
    for (let i = 0; i < 8; i++) {
      emitFrame(frame())
      await settle()
    }
    expect(bridge.start).toHaveBeenCalledTimes(1)
    await handle.commit()
    expect(bridge.start).toHaveBeenCalledTimes(1)
    expect(bridge.finish).not.toHaveBeenCalled()
  })

  it('splits a handshake backlog into ValidFrame-sized appends', async () => {
    let release!: (value: { sessionId: string }) => void
    bridge.start.mockImplementation(
      () =>
        new Promise<{ sessionId: string }>(resolve => {
          release = resolve
        }),
    )
    const pending = startVolcAsr(PROVIDER)
    await settle()
    for (let i = 0; i < 20; i++) emitFrame(frame())
    release({ sessionId: 'v1' })
    await pending
    for (let i = 0; i < 12; i++) await settle()
    expect(bridge.append.mock.calls.length).toBeGreaterThan(1)
    for (const call of bridge.append.mock.calls) {
      expect(atob((call[0] as { pcm: string }).pcm).length).toBeLessThanOrEqual(32_000)
    }
  })

  it('opens its own mic when external PCM never arrives', async () => {
    vi.useFakeTimers()
    try {
      await startVolcAsr(PROVIDER, { externalPcm: true })
      expect(startPcmCapture).not.toHaveBeenCalled()
      await vi.advanceTimersByTimeAsync(VOLC_EXTERNAL_PCM_RESCUE_MS)
      expect(startPcmCapture).toHaveBeenCalledOnce()
    } finally {
      vi.useRealTimers()
    }
  })

  it('never opens the rescue mic when external PCM already flowed', async () => {
    vi.useFakeTimers()
    try {
      const handle = await startVolcAsr(PROVIDER, { externalPcm: true })
      handle.pushFrame(frame())
      await vi.advanceTimersByTimeAsync(VOLC_EXTERNAL_PCM_RESCUE_MS)
      expect(startPcmCapture).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('drops the rescue mic when external PCM recovers after it opened', async () => {
    vi.useFakeTimers()
    try {
      const handle = await startVolcAsr(PROVIDER, { externalPcm: true })
      await vi.advanceTimersByTimeAsync(VOLC_EXTERNAL_PCM_RESCUE_MS)
      expect(startPcmCapture).toHaveBeenCalledOnce()
      await vi.advanceTimersByTimeAsync(1)
      // The recorder tap recovers late: external must win so we do not run
      // both the tap and the browser mic into seed-asr at once.
      handle.pushFrame(frame())
      expect(stopCapture).toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
})
