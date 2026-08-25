import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BridgeClientError } from '../../bridge/client'

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
  const onInterim = vi.fn()
  const onError = vi.fn()
  const onLevels = vi.fn()
  const onSpeechStart = vi.fn()
  let spoken = ''
  return {
    onFinal,
    onInterim,
    onError,
    onLevels,
    onSpeechStart,
    say: (text: string) => {
      spoken = text
    },
    options: {
      onFinal,
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

  it('lets nothing the microphone hears end her turn while she speaks', async () => {
    // Words that plainly are an interruption, and words that are plainly her
    // own coming back through the speaker, are treated the same way: dropped.
    // Telling them apart takes a guess, and a wrong guess cuts her off in the
    // middle of a word. The 打断 button does not have to guess.
    const stage = harness()
    stage.say('我帮你把这件事安排好了')
    asr.commit.mockResolvedValue('等一下先别说了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setAssistantPlayback(true)
    await vi.advanceTimersByTimeAsync(200)

    onTranscript('等一下先别说了', false)
    onTranscript('我帮你把这件事安排好了', false)
    await vi.advanceTimersByTimeAsync(500)

    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('does not carry what she said into the user\u2019s next turn', async () => {
    // Her voice off the speaker used to be written into the utterance buffer
    // and only then refused, so it was still sitting there when her turn
    // ended. A beat later it surfaced as a caption flashing her own last line
    // back at the user, and could be committed as though they had said it.
    const stage = harness()
    stage.say('好的，我把会议改到下午三点了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setAssistantPlayback(true)
    await vi.advanceTimersByTimeAsync(200)
    onTranscript('好的我把会议改到下午三点了', false)
    await vi.advanceTimersByTimeAsync(200)

    // Her turn ends and the user starts a fresh sentence.
    handle.setAssistantPlayback(false, 0)
    asr.commit.mockResolvedValue('那帮我订个会议室。')
    await vi.advanceTimersByTimeAsync(100)
    onTranscript('那帮我订个会议室。', false)
    await vi.advanceTimersByTimeAsync(600)

    expect(stage.onFinal).toHaveBeenCalledWith('那帮我订个会议室。')
    for (const [caption] of stage.onInterim.mock.calls) {
      expect(caption).not.toContain('会议改到下午三点')
    }
  })

  it('keeps the microphone muted for the whole reply, then reopens it', async () => {
    const stage = harness()
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setAssistantPlayback(true, 400)
    await vi.advanceTimersByTimeAsync(1000)
    // Still muted long after the guard window: it stays shut until she
    // stops, which is what removes echo as a category rather than guessing
    // at it from the transcript.
    expect(asr.setMuted).toHaveBeenLastCalledWith(true)

    handle.setAssistantPlayback(false, 400)
    await vi.advanceTimersByTimeAsync(400)
    expect(asr.setMuted).toHaveBeenLastCalledWith(false)
  })

  it('gives the turn back for nothing it hears while she is thinking', async () => {
    // Once she has the turn, only the 打断 button takes it back. That covers
    // the tail of the sentence that started her thinking, a cough, and a
    // fully-formed sentence the user changed their mind and said out loud —
    // there is no transcript that can distinguish them well enough to bet a
    // turn on, so none of them get one.
    for (const heard of ['算了不用查了', '查一下天气', '嗯']) {
      const stage = harness()
      asr.commit.mockResolvedValue(heard)
      const handle = await startLocalCompanionSpeech(stage.options)

      handle.setCommitPaused(true)
      await vi.advanceTimersByTimeAsync(2000)
      onTranscript(heard, false)
      await vi.advanceTimersByTimeAsync(400)

      expect(stage.onFinal).not.toHaveBeenCalled()
      handle.stop()
    }
  })

  it('never commits while she is thinking or speaking, then or later', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('那算了')
    const handle = await startLocalCompanionSpeech(stage.options)

    handle.setCommitPaused(true)
    onTranscript('那算了', false)
    await vi.advanceTimersByTimeAsync(1000)
    expect(stage.onFinal).not.toHaveBeenCalled()

    // Nor once her turn ends. Holding it until then was how her own voice,
    // heard while she was talking, arrived as the user's next sentence.
    handle.setCommitPaused(false)
    await vi.advanceTimersByTimeAsync(300)
    expect(stage.onFinal).not.toHaveBeenCalled()

    // What the user actually says next commits normally.
    asr.commit.mockResolvedValue('那帮我改一下。')
    onTranscript('那帮我改一下。', false)
    await vi.advanceTimersByTimeAsync(400)
    expect(stage.onFinal).toHaveBeenCalledWith('那帮我改一下。')
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
