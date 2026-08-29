import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BridgeClientError } from '../../bridge/client'
import { ENDPOINT_BACKSTOP_MS } from './localSpeech'
import { INCOMPLETE_HARD_MS, MEETING_TURN_END_SILENCE_MS } from './speech'

const asr = {
  finish: vi.fn(),
  cancel: vi.fn(),
  commit: vi.fn(),
  setMuted: vi.fn(),
  restart: vi.fn(),
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
  it('commits when the recognizer says the speaker stopped', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('今天天气很好')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('今天天气很好', false)
    expect(stage.onInterim).toHaveBeenCalledWith('今天天气很好')
    await vi.advanceTimersByTimeAsync(350)
    expect(stage.onFinal).not.toHaveBeenCalled()

    onTranscript('今天天气很好', true)
    await vi.advanceTimersByTimeAsync(1400)
    expect(stage.onFinal).toHaveBeenCalledWith('今天天气很好')
  })

  it('does not end a turn while the user is still adding to it', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('你可以')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('你可以', false)
    for (let i = 0; i < 15; i++) {
      onLevel(0.2)
      await vi.advanceTimersByTimeAsync(100)
    }
    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('hard-commits a frozen incomplete caption even if the analyser stays busy', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('你可以')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('你可以', false)
    await vi.advanceTimersByTimeAsync(INCOMPLETE_HARD_MS + 200)
    expect(stage.onFinal).toHaveBeenCalledWith('你可以')
  })

  it('does not wait forever on a recognizer that stops reporting endpoints', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('帮我看看明天的安排')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('帮我看看明天的安排', false)
    await vi.advanceTimersByTimeAsync(ENDPOINT_BACKSTOP_MS + 300)
    expect(stage.onFinal).toHaveBeenCalledWith('帮我看看明天的安排')
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
    onTranscript('就按这个来。', true)
    await vi.advanceTimersByTimeAsync(1400)
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

  it('lets the rest of the sentence arrive before committing it', async () => {
    // 「我说你之前的问题也」 — the half of a sentence that used to be answered
    // as though it were the whole of it, because the transcript had paused.
    const stage = harness()
    asr.commit.mockResolvedValue('我说你之前的问题也不能全怪我')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('我说你之前的问题也', false)
    for (let tick = 0; tick < 12; tick++) {
      onLevel(0.3)
      await vi.advanceTimersByTimeAsync(60)
    }
    expect(stage.onFinal).not.toHaveBeenCalled()

    onTranscript('我说你之前的问题也不能全怪我', true)
    await vi.advanceTimersByTimeAsync(1400)
    expect(stage.onFinal).toHaveBeenCalledWith('我说你之前的问题也不能全怪我')
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
    onTranscript('那帮我订个会议室。', true)
    await vi.advanceTimersByTimeAsync(1400)

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
    onTranscript('那帮我改一下。', true)
    await vi.advanceTimersByTimeAsync(1400)
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

  it('stays fluent across eight listen → 1.2s → answer → next-listen rounds', async () => {
    const stage = harness()
    const handle = await startLocalCompanionSpeech(stage.options)
    const lines = [
      '你好月汐',
      '今晚天气怎么样',
      '帮我打开桌面',
      '打开网易云音乐',
      '搜索周杰伦放一首',
      '下一句你好吗',
      '谢谢',
      '再见',
    ]
    for (const line of lines) {
      const before = stage.onFinal.mock.calls.length
      asr.commit.mockResolvedValue(line)
      onTranscript(line, false)
      onTranscript(line, true)
      await vi.advanceTimersByTimeAsync(400)
      expect(stage.onFinal).toHaveBeenCalledTimes(before)
      await vi.advanceTimersByTimeAsync(1000)
      expect(stage.onFinal).toHaveBeenCalledTimes(before + 1)
      expect(stage.onFinal).toHaveBeenLastCalledWith(line)
      stage.say(`好的，${line}`)
      handle.setAssistantPlayback(true)
      onTranscript(`好的，${line}`, true)
      await vi.advanceTimersByTimeAsync(200)
      handle.setAssistantPlayback(false, 0)
      await vi.advanceTimersByTimeAsync(50)
    }
    expect(stage.onFinal).toHaveBeenCalledTimes(lines.length)
    handle.stop()
  })

  it('does not treat「打开网」as finished on a fast engine endpoint', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('你帮我打开网')
    await startLocalCompanionSpeech(stage.options)

    onTranscript('你帮我打开网', false)
    onTranscript('你帮我打开网', true)
    await vi.advanceTimersByTimeAsync(400)
    expect(stage.onFinal).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(ENDPOINT_BACKSTOP_MS)
    expect(stage.onFinal).toHaveBeenCalledWith('你帮我打开网')
  })

  it('holds meeting clauses past the engine endpoint so the refiner sees the whole thought', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('第一步应该先写BRD。第二步再做相关工作。')
    await startLocalCompanionSpeech({ ...stage.options, holdUtterance: true })

    onTranscript('第一步应该先写BRD', false)
    onTranscript('第一步应该先写BRD', true)
    await vi.advanceTimersByTimeAsync(50)
    expect(stage.onFinal).not.toHaveBeenCalled()

    onTranscript('第二步再做相关工作。', true)
    await vi.advanceTimersByTimeAsync(400)
    expect(stage.onFinal).not.toHaveBeenCalled()
    expect(stage.onInterim).toHaveBeenCalledWith('第一步应该先写BRD第二步再做相关工作。')

    await vi.advanceTimersByTimeAsync(MEETING_TURN_END_SILENCE_MS + 400)
    expect(stage.onFinal).toHaveBeenCalledWith('第一步应该先写BRD。第二步再做相关工作。')
  })

  it('shows the next user caption during the echo guard without committing yet', async () => {
    const stage = harness()
    const handle = await startLocalCompanionSpeech(stage.options)
    handle.setAssistantPlayback(false, 800)
    onTranscript('下一句你好吗', false)
    expect(stage.onInterim).toHaveBeenCalledWith('下一句你好吗')
    await vi.advanceTimersByTimeAsync(400)
    expect(stage.onFinal).not.toHaveBeenCalled()
  })

  it('restarts a silent local recognizer on pulse instead of waiting forever', async () => {
    const stage = harness()
    asr.restart.mockResolvedValue(undefined)
    const handle = await startLocalCompanionSpeech(stage.options)
    handle.pulseRecognition()
    await Promise.resolve()
    expect(asr.restart).toHaveBeenCalled()
  })
})
