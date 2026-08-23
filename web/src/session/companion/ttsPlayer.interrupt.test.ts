// ttsPlayer.interrupt.test.ts pins the MC-04 real-machine acceptance:
// interrupting during playback must silence within 100ms even while the
// tts.cancel receipt is artificially delayed by 3s (前端静音为准，不等回执).
// It also covers the player-side M95-001 degradation (MC-05): an
// engine-unavailable synthesize error resolves through onEngineUnavailable
// and never surfaces a dialog.
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import type { TtsBridge, TtsSynthesizePayload, TtsSynthesizeResult } from '../../bridge/client'
import { TtsPlayer, speechAudioBounds } from './ttsPlayer'
import { defaultCompanionSettings } from './companionSettings'

const bridge = {
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
  refAudios: vi.fn(),
  ensureRefEngine: vi.fn(),
} satisfies Record<keyof TtsBridge, ReturnType<typeof vi.fn>>

vi.mock('../../bridge/client', () => ({ getTtsBridge: () => bridge }))

// A 44-byte all-zero RIFF stub: the player only moves bytes into a Blob,
// it never parses the audio in jsdom.
const WAV_STUB = btoa(String.fromCharCode(...new Array(44).fill(0)))

const okResult = (): TtsSynthesizeResult => ({ wav_base64: WAV_STUB, duration_hint: 1 })

let pauseEvents: number[] = []
let playEvents: number[] = []

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  vi.stubGlobal('URL', {
    ...URL,
    createObjectURL: vi.fn(() => 'blob:mock'),
    revokeObjectURL: vi.fn(),
  })
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLAudioElement) {
    playEvents.push(performance.now())
    return Promise.resolve()
  })
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function (this: HTMLAudioElement) {
    pauseEvents.push(performance.now())
  })
  bridge.synthesize.mockReset().mockResolvedValue(okResult())
  bridge.cancel.mockReset()
  pauseEvents = []
  playEvents = []
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('TtsPlayer interruption (MC-04: silence within 100ms, receipt delayed 3s)', () => {
  test('audio pauses within 100ms of interrupt() while the cancel receipt stays pending', async () => {
    // Receipt never settles within the window under test (injected 3s delay).
    bridge.cancel.mockReturnValue(new Promise(resolve => setTimeout(resolve, 3000)))

    const player = new TtsPlayer()
    // Hold playback inside the first segment: 'ended' never fires.
    const speaking = player.speak(['第一段播放中', '第二段被丢弃'], defaultCompanionSettings(), {})
    await vi.waitFor(() => expect(playEvents.length).toBeGreaterThanOrEqual(1))

    const interruptAt = performance.now()
    player.interrupt()
    expect(pauseEvents.length).toBeGreaterThanOrEqual(1)
    // The local mute is synchronous — it must not wait for the 3s receipt.
    const silenceDelay = pauseEvents[0] - interruptAt
    expect(silenceDelay).toBeLessThanOrEqual(100)

    // Cancel was dispatched to the engine (fire-and-forget, still pending here).
    expect(bridge.cancel).toHaveBeenCalledTimes(1)

    // Advance past the 3s receipt: the pipeline is already quiet and the
    // speak loop exited without playing the second segment.
    await vi.advanceTimersByTimeAsync(3100)
    await speaking
    // P0-2 double prefetch: segment N+1 ('第二段被丢弃') was already
    // synthesized in the background before the interrupt — its wav is
    // discarded with the prefetch queue and never played.
    expect(bridge.synthesize).toHaveBeenCalledTimes(2)
  })

  test('interrupt during synthesis drops the in-flight segment without playback', async () => {
    let releaseSynthesis: (value: TtsSynthesizeResult) => void = () => {}
    bridge.synthesize.mockReturnValue(
      new Promise<TtsSynthesizeResult>(resolve => {
        releaseSynthesis = resolve
      }),
    )
    bridge.cancel.mockResolvedValue({ notice: 'TTS_CANCELLED' } as never)

    const finished: string[] = []
    const player = new TtsPlayer()
    const speaking = player.speak(['慢合成段'], defaultCompanionSettings(), {
      onFinished: reason => finished.push(reason),
    })
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(1))

    player.interrupt()
    releaseSynthesis(okResult()) // late arrival after the interrupt
    await speaking

    expect(playEvents).toHaveLength(0) // never audibly started
    expect(finished).toHaveLength(0) // speak() returned via generation guard
  })
})

describe('TtsPlayer engine-unavailable degradation (MC-05 player side, M95-001)', () => {
  test('falls back to natural when edge synthesis fails', async () => {
    bridge.synthesize.mockImplementation(async (payload: TtsSynthesizePayload) => {
      if (payload.engine === 'edge') throw new Error('M95-002 该段语音合成失败')
      return okResult()
    })
    const engines: string[] = []
    const player = new TtsPlayer()
    player.enqueue(['你好。'], { ...defaultCompanionSettings(), engine: 'edge' }, {
      onEngineFallback: engine => engines.push(engine),
    })
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(2))
    expect(engines).toEqual(['natural'])
    bridge.cancel.mockResolvedValue({ notice: 'TTS_CANCELLED' } as never)
    player.interrupt()
  })

  test('an M95-001 synthesize error resolves through onEngineUnavailable without throwing', async () => {
    bridge.synthesize.mockRejectedValue(
      Object.assign(new Error('本机无可用语音合成引擎'), { code: 'M95-001' }),
    )
    const events: string[] = []
    const player = new TtsPlayer()
    await player.speak(['降级段'], defaultCompanionSettings(), {
      onEngineUnavailable: () => events.push('banner'),
      onFinished: reason => events.push(reason),
      onSegmentFailed: () => events.push('failed'),
    })
    expect(events).toEqual(['banner', 'engine-unavailable'])
    expect(bridge.synthesize).toHaveBeenCalledTimes(3)
    expect(pauseEvents).toHaveLength(0) // nothing was ever played
    expect(bridge.cancel).not.toHaveBeenCalled() // degradation is not an interrupt
  })

  test('degradation beats the circuit breaker: no 3-strike counting on M95-001', async () => {
    bridge.synthesize.mockRejectedValue(
      Object.assign(new Error('本机无可用语音合成引擎'), { code: 'M95-001' }),
    )
    const failures: Array<[number, number]> = []
    const player = new TtsPlayer()
    await player.speak(['一', '二', '三', '四'], defaultCompanionSettings(), {
      onSegmentFailed: (index, count) => failures.push([index, count]),
      onFinished: reason => failures.push([-1, -1]) && undefined,
    })
    expect(bridge.synthesize).toHaveBeenCalledTimes(3)
    expect(failures).toHaveLength(1)
    expect(failures[0]).toEqual([-1, -1])
  })
})

describe('speechAudioBounds', () => {
  test('trims leading and trailing silence while keeping an 8ms pad', () => {
    const sampleRate = 1000
    const channel = new Float32Array(200)
    for (let i = 50; i < 120; i++) channel[i] = 0.5
    const { start, length } = speechAudioBounds(channel, sampleRate)
    expect(start).toBe(42)
    expect(start + length).toBe(128)
  })

  test('keeps the full buffer when the audible span is shorter than 40ms', () => {
    const sampleRate = 1000
    const channel = new Float32Array(200)
    channel[80] = 0.5
    const { start, length } = speechAudioBounds(channel, sampleRate)
    expect(start).toBe(0)
    expect(length).toBe(200)
  })
})

describe('TtsPlayer streaming prefetch', () => {
  test('enqueue synthesizes later segments while the first clip is still playing', async () => {
    const player = new TtsPlayer()
    player.enqueue(['一句。', '两句。', '三句。'], defaultCompanionSettings(), {})
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(3))
    await vi.waitFor(() => expect(playEvents.length).toBe(1))
    bridge.cancel.mockResolvedValue({ notice: 'TTS_CANCELLED' } as never)
    player.interrupt()
  })
})
