// ttsPlayer.interrupt.test.ts pins the MC-04 real-machine acceptance:
// interrupting during playback must silence within 100ms even while the
// tts.cancel receipt is artificially delayed by 3s (前端静音为准，不等回执).
// It also covers the player-side M95-001 degradation (MC-05): an
// engine-unavailable synthesize error resolves through onEngineUnavailable
// and never surfaces a dialog.
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import type { TtsBridge, TtsSynthesizePayload, TtsSynthesizeResult } from '../../bridge/client'
import { TtsPlayer } from './ttsPlayer'
import { defaultCompanionSettings } from './companionSettings'

const bridge = {
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
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
    expect(bridge.synthesize).toHaveBeenCalledTimes(1) // 第二段被丢弃
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
    // First segment already degraded to subtitles; later segments are never attempted.
    expect(bridge.synthesize).toHaveBeenCalledTimes(1)
    expect(failures).toHaveLength(1)
    expect(failures[0]).toEqual([-1, -1])
  })
})
