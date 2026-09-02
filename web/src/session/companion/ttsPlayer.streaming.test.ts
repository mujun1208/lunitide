// Whether a streamed reply reaches the speaker as one continuous reading.
//
// The player deliberately merges streamed sentences so a turn is one or two
// clips instead of one synthesis per period. What no test covered is *when* it
// starts synthesizing the merged tail. Starting only after the clip in the
// speaker has all but run out puts the engine's own few hundred milliseconds
// of latency into the middle of her answer — the stutter the merging exists to
// prevent, arriving through the mechanism meant to prevent it.
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { TtsBridge, TtsSynthesizeResult } from '../../bridge/client'
import { TtsPlayer } from './ttsPlayer'
import { defaultCompanionSettings } from './companionSettings'

const bridge = {
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
  refAudios: vi.fn(),
  ensureRefEngine: vi.fn(),
  installRefEngine: vi.fn(),
  installOnnxEngine: vi.fn(),
  stream: vi.fn(),
} satisfies Record<keyof TtsBridge, ReturnType<typeof vi.fn>>

vi.mock('../../bridge/client', () => ({ getTtsBridge: () => bridge }))

const WAV_STUB = btoa(String.fromCharCode(...new Array(44).fill(0)))
const okResult = (): TtsSynthesizeResult => ({ wav_base64: WAV_STUB, duration_hint: 3 })

let playEvents: number[] = []

beforeEach(() => {
  vi.stubGlobal('URL', { ...URL, createObjectURL: vi.fn(() => 'blob:mock'), revokeObjectURL: vi.fn() })
  // play() resolves but 'ended' is never dispatched, so the opening clip stays
  // in the speaker for the whole test — the state this is about.
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(() => {
    playEvents.push(performance.now())
    return Promise.resolve()
  })
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  bridge.synthesize.mockReset().mockResolvedValue(okResult())
  bridge.cancel.mockReset().mockResolvedValue(undefined)
  playEvents = []
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const textOf = (call: number): string => bridge.synthesize.mock.calls[call]?.[0]?.text ?? ''

test('sends the streamed tail to the engine while the opening clip is still playing', async () => {
  const player = new TtsPlayer()
  const settings = defaultCompanionSettings()

  player.enqueue(['今天多云。'], settings, {})
  await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(1))
  await vi.waitFor(() => expect(playEvents.length).toBe(1))

  player.enqueue(['气温二十六度，晚上转晴。'], settings, {})

  // The engine has to be working on the tail before the opening clip runs
  // out. If this only happens once playback ends, every reply longer than one
  // sentence carries a hole where the engine was thinking.
  await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(2), { timeout: 2000 })
  expect(textOf(1)).toContain('气温二十六度')

  player.interrupt()
})

test('holds streamed text while the engine is busy, then sends it as one clip', async () => {
  const player = new TtsPlayer()
  const settings = defaultCompanionSettings()
  bridge.synthesize.mockImplementation(() => new Promise(resolve => setTimeout(() => resolve(okResult()), 120)))

  player.enqueue(['第一句。'], settings, {})
  player.enqueue(['第二句。'], settings, {})
  player.enqueue(['第三句。'], settings, {})

  // The opening sentence leaves immediately — that is the head start. What
  // arrives while it is in the engine waits, instead of queueing a request per
  // sentence behind it and turning one reading into three.
  await new Promise(resolve => setTimeout(resolve, 60))
  expect(bridge.synthesize).toHaveBeenCalledTimes(1)
  expect(textOf(0)).toBe('第一句。')

  // Once the opening clip is sounding, everything held goes over together, so
  // the number of joins does not depend on how the model chose to break up
  // its output.
  await vi.waitFor(() => expect(playEvents.length).toBe(1))
  player.enqueue(['第四句。'], settings, {})
  await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalledTimes(2), { timeout: 2000 })
  expect(textOf(1)).toBe('第二句。第三句。第四句。')

  player.interrupt()
})
