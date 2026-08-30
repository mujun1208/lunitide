import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BARGE_IN_ARM_MS, ENDPOINT_BACKSTOP_MS } from './volcSpeech'
import { INCOMPLETE_HARD_MS } from '../speech'

const asr = {
  finish: vi.fn(),
  cancel: vi.fn(),
  commit: vi.fn(),
  setMuted: vi.fn(),
  restart: vi.fn(),
}

let onTranscript: (text: string, final: boolean) => void = () => {}

vi.mock('./volcAsr', () => ({
  startVolcAsr: vi.fn(async (_providerId: string, callbacks: Record<string, (...args: never[]) => void>) => {
    onTranscript = callbacks.onTranscript as typeof onTranscript
    return asr
  }),
}))

const { startVolcCompanionSpeech } = await import('./volcSpeech')

const PROVIDER = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

const harness = () => {
  const onFinal = vi.fn()
  const onInterim = vi.fn()
  const onError = vi.fn()
  let spoken = ''
  return {
    onFinal,
    onInterim,
    onError,
    say: (text: string) => {
      spoken = text
    },
    options: {
      onFinal,
      onInterim,
      onError,
      spokenText: () => spoken,
    },
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  asr.commit.mockImplementation(async () => '')
})

afterEach(() => {
  vi.useRealTimers()
})

describe('startVolcCompanionSpeech', () => {
  it('commits when seed-asr says the speaker stopped', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('今天天气很好')
    await startVolcCompanionSpeech(stage.options, PROVIDER)

    onTranscript('今天天气很好', false)
    expect(stage.onInterim).toHaveBeenCalledWith('今天天气很好')
    await vi.advanceTimersByTimeAsync(350)
    expect(stage.onFinal).not.toHaveBeenCalled()

    onTranscript('今天天气很好', true)
    await vi.advanceTimersByTimeAsync(1400)
    expect(stage.onFinal).toHaveBeenCalledWith('今天天气很好')
  })

  it('hard-commits a frozen incomplete caption', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('你可以')
    await startVolcCompanionSpeech(stage.options, PROVIDER)

    onTranscript('你可以', false)
    await vi.advanceTimersByTimeAsync(INCOMPLETE_HARD_MS + 200)
    expect(stage.onFinal).toHaveBeenCalledWith('你可以')
  })

  it('does not wait forever on a recognizer that stops reporting endpoints', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('帮我看看明天的安排')
    await startVolcCompanionSpeech(stage.options, PROVIDER)

    onTranscript('帮我看看明天的安排', false)
    await vi.advanceTimersByTimeAsync(ENDPOINT_BACKSTOP_MS + 300)
    expect(stage.onFinal).toHaveBeenCalledWith('帮我看看明天的安排')
  })

  it('with barge-in, a non-echo transcript cuts in and echo does not', async () => {
    const onBargeIn = vi.fn()
    const stage = harness()
    stage.say('很久很久以前有一座山')
    const handle = await startVolcCompanionSpeech(
      { ...stage.options, bargeIn: () => true, onBargeIn },
      PROVIDER,
    )
    handle.setAssistantPlayback(true)
    expect(asr.setMuted).toHaveBeenLastCalledWith(false)
    await vi.advanceTimersByTimeAsync(BARGE_IN_ARM_MS + 20)
    onTranscript('很久很久以前有一座山', false)
    expect(onBargeIn).not.toHaveBeenCalled()
    onTranscript('不是这个', false)
    expect(onBargeIn).toHaveBeenCalledWith('不是这个')
    handle.stop()
  })
})
