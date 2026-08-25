import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BridgeClientError } from '../../bridge/client'
import { BARGE_IN_SETTLE_MS } from './speech'

const asr = {
  finish: vi.fn(),
  cancel: vi.fn(),
  commit: vi.fn(),
  setMuted: vi.fn(),
}

let onTranscript: (text: string, final: boolean) => void = () => {}
let onLevel: (peak: number) => void = () => {}
let onAsrError: (error: unknown) => void = () => {}
let startRejects: Error | undefined

vi.mock('./localAsr', () => ({
  startLocalAsr: vi.fn(async (callbacks: Record<string, (...args: never[]) => void>) => {
    if (startRejects) throw startRejects
    onTranscript = callbacks.onTranscript as typeof onTranscript
    onLevel = callbacks.onLevel as typeof onLevel
    onAsrError = callbacks.onError as typeof onAsrError
    return asr
  }),
}))

const { startLocalCompanionSpeech } = await import('./localSpeech')

const harness = () => {
  const onFinal = vi.fn()
  const onBargeIn = vi.fn()
  const onInterim = vi.fn()
  const onError = vi.fn()
  const onLevels = vi.fn()
  const onSpeechStart = vi.fn()
  let spoken = ''
  return {
    onFinal,
    onBargeIn,
    onInterim,
    onError,
    onLevels,
    onSpeechStart,
    say: (text: string) => {
      spoken = text
    },
    options: {
      onFinal,
      onBargeIn,
      onInterim,
      onError,
      onLevels,
      onSpeechStart,
      spokenText: () => spoken,
    },
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  startRejects = undefined
  asr.commit.mockImplementation(async () => '')
  asr.finish.mockResolvedValue('')
})

afterEach(() => {
  vi.useRealTimers()
})

describe('startLocalCompanionSpeech', () => {
  it('commits a complete sentence once the transcript settles', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('今天天气很好')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('今天天气很好', false)
    expect(stage.onInterim).toHaveBeenCalledWith('今天天气很好')

    await vi.advanceTimersByTimeAsync(150)
    expect(stage.onFinal).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(200)
    expect(stage.onFinal).toHaveBeenCalledWith('今天天气很好')
  })

  it('holds an unfinished phrase instead of cutting the user off', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('你可以')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('你可以', false)
    // The settle window that would commit a complete sentence must not commit
    // a dangling one: "你可以" is the middle of a request, not a request.
    await vi.advanceTimersByTimeAsync(600)
    expect(stage.onFinal).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2000)
    expect(stage.onFinal).toHaveBeenCalledWith('你可以')
  })

  it('keeps the utterance open while the user is still adding to it', async () => {
    const stage = harness()
    await startLocalCompanionSpeech(stage.options)

    // Text still growing means the user is still talking, whatever the gaps
    // between tokens look like.
    for (const partial of ['我想', '我想让你', '我想让你帮我', '我想让你帮我看看']) {
      onTranscript(partial, false)
      await vi.advanceTimersByTimeAsync(150)
    }
    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('reuses the streamed partial when the commit comes back empty', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('就按这个来。', false)
    await vi.advanceTimersByTimeAsync(400)
    // Losing the sentence would be worse than sending the copy the user
    // already watched appear in the caption.
    expect(stage.onFinal).toHaveBeenCalledWith('就按这个来。')
  })

  it('interrupts playback when the user talks over her', async () => {
    const stage = harness()
    stage.say('我帮你把这件事安排好了')
    asr.commit.mockResolvedValue('等一下先别说了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setAssistantPlayback(true)
    await vi.advanceTimersByTimeAsync(200)

    onTranscript('等一下先别说了', false)
    await vi.advanceTimersByTimeAsync(0)

    expect(stage.onBargeIn).toHaveBeenCalledWith('等一下先别说了')
    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('does not mistake her own voice for an interruption', async () => {
    const stage = harness()
    stage.say('我帮你把这件事安排好了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setAssistantPlayback(true)
    await vi.advanceTimersByTimeAsync(200)

    onTranscript('我帮你把这件事安排好了', false)
    await vi.advanceTimersByTimeAsync(500)

    expect(stage.onBargeIn).not.toHaveBeenCalled()
    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('drops what the microphone hears while the speaker ramps up', async () => {
    const stage = harness()
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setAssistantPlayback(true, 400)
    onTranscript('刺啦', false)
    await vi.advanceTimersByTimeAsync(100)

    expect(stage.onBargeIn).not.toHaveBeenCalled()
    // The feed pauses for the guard, but the microphone itself stays open —
    // muting it is what makes a companion impossible to interrupt.
    expect(asr.setMuted).toHaveBeenCalledWith(true)
    await vi.advanceTimersByTimeAsync(400)
    expect(asr.setMuted).toHaveBeenLastCalledWith(false)
  })

  it('lets the user interrupt while she is only thinking', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('算了不用查了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setCommitPaused(true)
    // Nothing is playing, so there is no echo to compare against — the guard
    // is the settle window after the sentence that started her thinking.
    await vi.advanceTimersByTimeAsync(BARGE_IN_SETTLE_MS + 100)
    onTranscript('算了不用查了', false)
    await vi.advanceTimersByTimeAsync(0)

    expect(stage.onBargeIn).toHaveBeenCalledWith('算了不用查了')
  })

  it('does not let the tail of the sent sentence restart the turn', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('帮我查一下天气。')
    const handle = await startLocalCompanionSpeech(stage.options)

    // A real turn: the sentence commits, she starts thinking, and the last
    // words of that same sentence are still arriving from the recognizer.
    onTranscript('帮我查一下天气。', false)
    await vi.advanceTimersByTimeAsync(400)
    expect(stage.onFinal).toHaveBeenCalledTimes(1)

    handle.setCommitPaused(true)
    onTranscript('查一下天气', false)
    await vi.advanceTimersByTimeAsync(200)

    expect(stage.onBargeIn).not.toHaveBeenCalled()
  })

  it('ignores a short cough while she is thinking', async () => {
    const stage = harness()
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setCommitPaused(true)
    await vi.advanceTimersByTimeAsync(BARGE_IN_SETTLE_MS + 100)
    onTranscript('嗯', false)
    await vi.advanceTimersByTimeAsync(200)

    expect(stage.onBargeIn).not.toHaveBeenCalled()
  })

  it('never commits while she is thinking or speaking', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('那算了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setCommitPaused(true)
    onTranscript('那算了', false)
    await vi.advanceTimersByTimeAsync(1000)
    expect(stage.onFinal).not.toHaveBeenCalled()

    handle.setCommitPaused(false)
    await vi.advanceTimersByTimeAsync(300)
    expect(stage.onFinal).toHaveBeenCalledWith('那算了')
  })

  it('paints the ring from the microphone level', async () => {
    const stage = harness()
    await startLocalCompanionSpeech(stage.options)

    onLevel(0.3)
    const levels = stage.onLevels.mock.calls.at(-1)?.[0] as number[]
    expect(levels).toHaveLength(12)
    expect(levels.at(-1)).toBeGreaterThan(0)
    expect(levels.every(value => value >= 0 && value <= 1)).toBe(true)
  })

  it('announces speech the first time the user is heard, once', async () => {
    const stage = harness()
    await startLocalCompanionSpeech(stage.options)

    onTranscript('你好', false)
    onTranscript('你好吗', false)
    expect(stage.onSpeechStart).toHaveBeenCalledTimes(1)
  })

  it('surfaces a recognizer failure as a bridge error and stops', async () => {
    const stage = harness()
    await startLocalCompanionSpeech(stage.options)

    onAsrError(new Error('sidecar exited'))

    expect(stage.onError).toHaveBeenCalledTimes(1)
    expect(stage.onError.mock.calls[0][0]).toBeInstanceOf(BridgeClientError)
    expect(asr.cancel).toHaveBeenCalled()
    // A dead recognizer must not keep painting a live-looking meter.
    expect(stage.onLevels).toHaveBeenLastCalledWith(Array.from({ length: 12 }, () => 0))
  })

  it('stops the recognizer and clears the meter on stop', async () => {
    const stage = harness()
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.stop()
    onTranscript('还在说', false)
    await vi.advanceTimersByTimeAsync(1000)

    expect(asr.cancel).toHaveBeenCalled()
    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('sends the pending text on demand when endpointing has not fired', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('就这样吧')
    const handle = await startLocalCompanionSpeech(stage.options)

    onTranscript('就这样吧', false)
    handle.forceCommit()
    await vi.advanceTimersByTimeAsync(0)

    expect(stage.onFinal).toHaveBeenCalledWith('就这样吧')
  })

  it('propagates a refusal to start so the stage can fall back', async () => {
    startRejects = new Error('模型未安装')
    await expect(startLocalCompanionSpeech(harness().options)).rejects.toThrow('模型未安装')
  })
})
