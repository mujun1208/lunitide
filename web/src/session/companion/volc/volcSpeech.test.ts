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

let onTranscript: (text: string, final: boolean, timestamped?: boolean) => void = () => {}
let onLevel: (peak: number) => void = () => {}

vi.mock('./volcAsr', () => ({
  startVolcAsr: vi.fn(async (_providerId: string, callbacks: Record<string, (...args: never[]) => void>) => {
    onTranscript = callbacks.onTranscript as typeof onTranscript
    onLevel = callbacks.onLevel as typeof onLevel
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
  it('submits at 1.2s actual silence despite a late correction and a slow ASR flush', async () => {
    const stage = harness()
    let finish!: (value: string) => void
    asr.commit.mockReturnValueOnce(new Promise<string>(resolve => { finish = resolve }))
    const handle = await startVolcCompanionSpeech(stage.options, PROVIDER)
    onLevel(0.3)
    onTranscript('今天合肥天气怎么样？', false, true)
    await vi.advanceTimersByTimeAsync(1140)
    onTranscript('今天合肥市的天气怎么样？', false, true)
    expect(stage.onFinal).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(60)
    expect(stage.onFinal).toHaveBeenCalledExactlyOnceWith('今天合肥市的天气怎么样？')
    expect(asr.commit).toHaveBeenCalledWith({ useStreamed: true })
    finish('怎么样？')
    await vi.advanceTimersByTimeAsync(100)
    expect(stage.onFinal).toHaveBeenCalledTimes(1)
    handle.stop()
  })

  it('uses fresh actual silence for every round and never commits while speech continues', async () => {
    const stage = harness()
    const handle = await startVolcCompanionSpeech(stage.options, PROVIDER)
    for (const text of ['今天合肥天气怎么样？', '今天上海到合肥的火车。', '查一下明天的天气。']) {
      onLevel(0.3)
      onTranscript(text, false, true)
      const previousCalls = stage.onFinal.mock.calls.length
      for (let i = 0; i < 10; i++) {
        await vi.advanceTimersByTimeAsync(240)
        onLevel(0.3)
      }
      expect(stage.onFinal).toHaveBeenCalledTimes(previousCalls)
      await vi.advanceTimersByTimeAsync(1140)
      expect(stage.onFinal).toHaveBeenCalledTimes(previousCalls)
      await vi.advanceTimersByTimeAsync(120)
      expect(stage.onFinal).toHaveBeenLastCalledWith(text)
      expect(stage.onFinal).toHaveBeenCalledTimes(previousCalls + 1)
    }
    handle.stop()
  })

  it('retains the longer meeting hold after actual microphone silence', async () => {
    const stage = harness()
    const handle = await startVolcCompanionSpeech({ ...stage.options, holdUtterance: true }, PROVIDER)
    onLevel(0.3)
    onTranscript('今天合肥天气怎么样？', true, true)
    await vi.advanceTimersByTimeAsync(1260)
    expect(stage.onFinal).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)
    expect(stage.onFinal).toHaveBeenCalledExactlyOnceWith('今天合肥天气怎么样？')
    expect(asr.commit).toHaveBeenCalledWith()
    handle.stop()
  })
  it('commits when seed-asr says the speaker stopped', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('今天天气很好')
    await startVolcCompanionSpeech(stage.options, PROVIDER)

    onTranscript('今天天气很好', false)
    expect(stage.onInterim).toHaveBeenCalledWith('今天天气很好')
    await vi.advanceTimersByTimeAsync(200)
    expect(stage.onFinal).not.toHaveBeenCalled()

    onTranscript('今天天气很好', true)
    await vi.advanceTimersByTimeAsync(220)
    expect(stage.onFinal).toHaveBeenCalledWith('今天天气很好')
  })

  it('commits a complete greeting within 400ms', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('你好')
    await startVolcCompanionSpeech(stage.options, PROVIDER)

    onTranscript('你好', false)
    await vi.advanceTimersByTimeAsync(200)
    expect(stage.onFinal).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(220)
    expect(stage.onFinal).toHaveBeenCalledWith('你好')
  })

  it('does not commit「打开网」within 500ms', async () => {
    const stage = harness()
    asr.commit.mockResolvedValue('打开网')
    await startVolcCompanionSpeech(stage.options, PROVIDER)

    onTranscript('打开网', false)
    await vi.advanceTimersByTimeAsync(500)
    expect(stage.onFinal).not.toHaveBeenCalled()
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

  it('takes already isolated turns from the ASR cursor without clipping shared words again', async () => {
    const stage = harness()
    asr.commit
      .mockResolvedValueOnce('今天天气怎么样')
      .mockResolvedValueOnce('算了放首歌')
    await startVolcCompanionSpeech(stage.options, PROVIDER)
    onTranscript('今天天气怎么样', true)
    await vi.advanceTimersByTimeAsync(1300)
    expect(stage.onFinal).toHaveBeenLastCalledWith('今天天气怎么样')
    onTranscript('算了放首歌', false)
    expect(stage.onInterim.mock.calls.at(-1)?.[0]).toBe('算了放首歌')
    onTranscript('算了放首歌', true)
    await vi.advanceTimersByTimeAsync(1300)
    expect(stage.onFinal.mock.calls.at(-1)?.[0]).toBe('算了放首歌')
  })

  it.each([false, true])('replaces non-prefix revisions instead of accumulating whole sentences (meeting=%s)', async holdUtterance => {
    const stage = harness()
    const handle = await startVolcCompanionSpeech({ ...stage.options, holdUtterance }, PROVIDER)
    for (let i = 0; i < 40; i++) {
      const revised = `今天${i % 2 ? '合肥' : '合肥市'}的天气很好，我们一起出去散步。`
      onTranscript(revised, false, true)
      expect(stage.onInterim).toHaveBeenLastCalledWith(revised)
      await vi.advanceTimersByTimeAsync(40)
    }
    const complete = '今天合肥的天气很好，我们一起出去散步。'
    onTranscript(complete, true, true)
    asr.commit.mockResolvedValue(complete)
    await handle.flush?.()
    expect(stage.onFinal).toHaveBeenCalledExactlyOnceWith(complete)
    handle.stop()
  })

  it('keeps the complete user caption when finish returns only its last syllable', async () => {
    const stage = harness()
    const handle = await startVolcCompanionSpeech(stage.options, PROVIDER)
    const complete = '今天合肥天气怎么样呢？'
    onTranscript(complete, false, true)
    onTranscript('呢？', true, true)
    asr.commit.mockResolvedValue('呢？')
    await handle.flush?.()
    expect(stage.onInterim).toHaveBeenLastCalledWith(complete)
    expect(stage.onFinal).toHaveBeenCalledExactlyOnceWith(complete)
    handle.stop()
  })

  it('waits for the current timestamped segment to settle, then replies promptly', async () => {
    const stage = harness()
    const handle = await startVolcCompanionSpeech(stage.options, PROVIDER)
    onTranscript('你好', false, true)
    await vi.advanceTimersByTimeAsync(420)
    expect(stage.onFinal).not.toHaveBeenCalled()
    onTranscript('你好。', true, true)
    asr.commit.mockResolvedValue('你好。')
    await vi.advanceTimersByTimeAsync(300)
    expect(stage.onFinal).toHaveBeenCalledExactlyOnceWith('你好。')
    handle.stop()
  })

  it('does not submit a pending finish after the stage is closed', async () => {
    const stage = harness()
    let finish!: (text: string) => void
    asr.commit.mockImplementation(() => new Promise<string>(resolve => { finish = resolve }))
    const handle = await startVolcCompanionSpeech(stage.options, PROVIDER)
    onTranscript('先不要发出去。', true, true)
    const pending = handle.flush?.()
    handle.stop()
    finish('先不要发出去。')
    await pending
    expect(stage.onFinal).not.toHaveBeenCalled()
  })
})
