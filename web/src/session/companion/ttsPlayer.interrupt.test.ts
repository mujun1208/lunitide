// ttsPlayer.interrupt.test.ts pins the MC-04 real-machine acceptance:
// interrupting during playback must silence within 100ms even while the
// tts.cancel receipt is artificially delayed by 3s (前端静音为准，不等回执).
// It also covers the player-side M95-001 degradation (MC-05): an
// engine-unavailable synthesize error resolves through onEngineUnavailable
// and never surfaces a dialog.
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import type { TtsBridge, TtsSynthesizeResult } from '../../bridge/client'
import { TtsPlayer, speechAudioBounds } from './ttsPlayer'
import { defaultCompanionSettings } from './companionSettings'

const bridge = {
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
  refAudios: vi.fn(),
  ensureRefEngine: vi.fn(),
  installRefEngine: vi.fn(),
  stream: vi.fn(),
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

  test('interrupt({ cancelEngine: false }) silences locally without aborting the next synth', async () => {
    const player = new TtsPlayer()
    const speaking = player.speak(['垫音'], defaultCompanionSettings(), {})
    await vi.waitFor(() => expect(playEvents.length).toBeGreaterThanOrEqual(1))
    bridge.cancel.mockClear()
    player.interrupt({ cancelEngine: false })
    expect(pauseEvents.length).toBeGreaterThanOrEqual(1)
    expect(bridge.cancel).not.toHaveBeenCalled()
    await speaking
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

    expect(player.isBusy()).toBe(true)
    player.interrupt()
    expect(player.isBusy()).toBe(false)
    releaseSynthesis(okResult()) // late arrival after the interrupt
    await speaking

    expect(playEvents).toHaveLength(0) // never audibly started
    expect(finished).toHaveLength(0) // speak() returned via generation guard
  })

  test('flush does not fire onFinished after dispose or interrupt', async () => {
    let releaseSynthesis: (value: TtsSynthesizeResult) => void = () => {}
    bridge.synthesize.mockReturnValue(
      new Promise<TtsSynthesizeResult>(resolve => {
        releaseSynthesis = resolve
      }),
    )
    const player = new TtsPlayer()
    const finished: string[] = []
    const speaking = player.speak(['还在合成'], defaultCompanionSettings(), {})
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(1))
    expect(player.isBusy()).toBe(true)
    const flushing = player.flush({ onFinished: reason => finished.push(reason) })
    player.dispose()
    releaseSynthesis(okResult())
    await vi.advanceTimersByTimeAsync(200)
    await flushing
    await speaking
    expect(finished).toEqual([])
  })
})

describe('TtsPlayer engine-unavailable degradation (MC-05 player side, M95-001)', () => {
  test('does not fall back to classic SAPI when edge synthesis fails', async () => {
    bridge.synthesize.mockRejectedValue(new Error('M95-002 该段语音合成失败'))
    const engines: string[] = []
    const player = new TtsPlayer()
    player.enqueue(['你好。'], { ...defaultCompanionSettings(), engine: 'edge' }, {
      onEngineFallback: engine => engines.push(engine),
    })
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalled())
    expect(bridge.synthesize.mock.calls.map(call => call[0].engine)).not.toContain('sapi')
    expect(engines).not.toContain('sapi')
    bridge.cancel.mockResolvedValue({ notice: 'TTS_CANCELLED' } as never)
    player.interrupt()
  })

  test('lockEngine does not fall through to Edge for a selected ref voice', async () => {
    bridge.synthesize.mockRejectedValue(Object.assign(new Error('ref offline'), { code: 'M95-002' }))
    const engines: string[] = []
    const player = new TtsPlayer()
    player.enqueue(['嗯'], { ...defaultCompanionSettings(), engine: 'ref', voiceId: 'refpack:优质台湾腔.wav', lockEngine: true }, {
      onEngineFallback: engine => engines.push(engine),
    })
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalled())
    expect(bridge.synthesize.mock.calls.map(call => call[0].engine)).toEqual(['ref'])
    expect(engines).toEqual([])
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
    expect(bridge.synthesize).toHaveBeenCalledTimes(1)
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
    expect(bridge.synthesize).toHaveBeenCalledTimes(1)
    expect(failures).toHaveLength(1)
    expect(failures[0]).toEqual([-1, -1])
  })
})

describe('speechAudioBounds', () => {
  test('trims leading and trailing silence while keeping an 18ms pad', () => {
    const sampleRate = 1000
    const channel = new Float32Array(200)
    for (let i = 50; i < 120; i++) channel[i] = 0.5
    const { start, length } = speechAudioBounds(channel, sampleRate)
    expect(start).toBe(32)
    expect(start + length).toBe(138)
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
  test('enqueue joins a batch of sentences into one synth so playback does not chop', async () => {
    const player = new TtsPlayer()
    player.enqueue(['一句。', '两句。', '三句。'], defaultCompanionSettings(), {})
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(1))
    expect(bridge.synthesize.mock.calls[0][0].text).toBe('一句。两句。三句。')
    await vi.waitFor(() => expect(playEvents.length).toBe(1))
    bridge.cancel.mockResolvedValue({ notice: 'TTS_CANCELLED' } as never)
    player.interrupt()
  })

  test('later enqueue joins the hold tail instead of starting a new synth per sentence', async () => {
    const player = new TtsPlayer()
    player.enqueue(['一句。'], defaultCompanionSettings(), {})
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(1))
    player.enqueue(['两句。'], defaultCompanionSettings(), {})
    player.enqueue(['三句。'], defaultCompanionSettings(), {})
    await vi.advanceTimersByTimeAsync(100)
    expect(bridge.synthesize).toHaveBeenCalledTimes(1)
    expect(player.isBusy()).toBe(true)
    bridge.cancel.mockResolvedValue({ notice: 'TTS_CANCELLED' } as never)
    player.interrupt()
  })
})
