import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CompanionSpeechHandle, CompanionSpeechOptions } from './speech'
import { TURN_END_SILENCE_MS } from './speech'

const state = vi.hoisted(() => ({
  transcript: (_text: string, _final: boolean, _positioned?: boolean) => {},
  level: (_peak: number) => {},
  asr: { cancel: vi.fn(), commit: vi.fn(async () => ''), setMuted: vi.fn(), discardTranscript: vi.fn() },
}))
type Callbacks = { onTranscript: typeof state.transcript; onLevel: typeof state.level }
vi.mock('./localAsr', () => ({ startLocalAsr: async (callbacks: Callbacks) => {
  state.transcript = callbacks.onTranscript
  state.level = callbacks.onLevel
  return state.asr
} }))
vi.mock('./volc/volcAsr', () => ({ startVolcAsr: async (_provider: string, callbacks: Callbacks) => {
  state.transcript = callbacks.onTranscript
  state.level = callbacks.onLevel
  return state.asr
} }))
import { startLocalCompanionSpeech } from './localSpeech'
import { startVolcCompanionSpeech } from './volc/volcSpeech'

beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers() })
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

describe.each([
  ['local', startLocalCompanionSpeech],
  ['cloud-local fallback', startLocalCompanionSpeech],
  ['volc', (options: CompanionSpeechOptions) => startVolcCompanionSpeech(options, 'provider')],
] as const)('%s interrupt capture', (_name, start) => {
  it('preserves the onset and later revisions through a speech interruption', async () => {
    const final = vi.fn()
    const interim = vi.fn()
    let handle: CompanionSpeechHandle
    const barge = vi.fn(() => {
      handle.setCommitPaused(false)
      handle.setAssistantPlayback(false, 80)
      handle.resumeCapture()
    })
    handle = await start({ onFinal: final, onInterim: interim, onError: vi.fn(),
      bargeIn: () => true, onBargeIn: barge, spokenText: () => '很久以前，有一座城。' })
    handle.setCommitPaused(true)
    handle.setAssistantPlayback(true)
    await vi.advanceTimersByTimeAsync(180)
    state.level(0.03)
    state.transcript('查一下今天', false, true)
    expect(barge).toHaveBeenCalledTimes(1)
    expect(interim).toHaveBeenLastCalledWith('查一下今天')
    expect(final).not.toHaveBeenCalled()
    expect(state.asr.setMuted).toHaveBeenLastCalledWith(false)
    await vi.advanceTimersByTimeAsync(100)
    state.level(0.03)
    state.transcript('查一下今天合肥的天气。', true, true)
    await vi.advanceTimersByTimeAsync(TURN_END_SILENCE_MS + 120)
    expect(final).toHaveBeenCalledExactlyOnceWith('查一下今天合肥的天气。')
    handle.stop()
  })

  it('keeps the first words after button stop on repeated turns without reopening ASR', async () => {
    const final = vi.fn()
    const handle = await start({ onFinal: final, onError: vi.fn() })
    for (let turn = 0; turn < 3; turn++) {
      handle.setCommitPaused(true)
      handle.setAssistantPlayback(true)
      handle.setCommitPaused(false)
      handle.setAssistantPlayback(false, 80)
      expect(state.asr.setMuted).toHaveBeenLastCalledWith(false)
      state.level(0.03)
      state.transcript('今天合肥', false, true)
      await vi.advanceTimersByTimeAsync(100)
      state.level(0.03)
      state.transcript('今天合肥的天气怎么样？', true, true)
      await vi.advanceTimersByTimeAsync(TURN_END_SILENCE_MS + 120)
      expect(final).toHaveBeenCalledTimes(turn + 1)
      expect(final).toHaveBeenLastCalledWith('今天合肥的天气怎么样？')
    }
    expect(state.asr.cancel).not.toHaveBeenCalled()
    handle.stop()
  })
})
