// CompanionStage.headstart.test.tsx pins the two things that make a voice
// turn feel like a phone call rather than a walkie-talkie: the first
// finished sentence goes to the engine while the model is still writing
// (instead of waiting for the whole reply), and talking over her cuts the
// reply short and starts the user's turn.
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { TtsPlayerCallbacks } from './ttsPlayer'

interface CapturedSpeech {
  onFinal: (transcript: string) => void
  onInterim?: (transcript: string) => void
  onBargeIn?: (transcript: string) => void
  spokenText?: () => string
  onError: (error: unknown) => void
  onLevels?: (levels: number[]) => void
  onEndWithoutFinal?: () => void
}

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  callbacks: undefined as CapturedSpeech | undefined,
  stop: vi.fn(),
  setAssistantPlayback: vi.fn(),
  handle: () => ({
    stop: speech.stop,
    setAssistantPlayback: speech.setAssistantPlayback,
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

const tts = vi.hoisted(() => ({
  enqueueCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  playing: false,
}))

vi.mock('../../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../../bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () =>
        Promise.resolve({
          voices: [{ voice_id: 'zh-female', display_name: '月汐温柔女声', gender: 'female' as const, lang: 'zh-CN' }],
        }),
      synthesize: vi.fn(),
      cancel: vi.fn(),
      ensureRefEngine: vi.fn().mockResolvedValue({ state: 'online' }),
    }),
    getProviderBridge: () => ({
      list: () => Promise.resolve({
        items: [{
          id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
          name: 'Chat',
          protocol: 'openai_compatible',
          status: 'enabled',
          credentialState: 'configured',
          models: [{ modelId: 'chat', displayName: 'Chat', isDefault: true, kind: 'llm', kindDefault: true }],
        }],
      }),
    }),
    automationBridge: { listRuns: () => Promise.resolve({ runs: [] }) },
  }
})

vi.mock('./speech', () => ({
  ECHO_GUARD_MS: 90,
  FORCE_COMMIT_MS: 1800,
  INTERRUPT_ECHO_MS: 80,
  shouldShowSpeechSetupHint: () => false,
  startCompanionSpeech: (callbacks: CapturedSpeech) => {
    speech.callbacks = callbacks
    return speech.start(callbacks)
  },
}))

vi.mock('./ttsPlayer', () => ({
  unlockTtsAudio: vi.fn(() => Promise.resolve()),
  getTtsAudioState: () => 'running' as const,
  TtsPlayer: class {
    configure(): void {}
    async speak(): Promise<void> {}
    enqueue(segments: string[], _settings: unknown, callbacks: TtsPlayerCallbacks) {
      tts.enqueueCalls.push({ segments, callbacks })
      tts.playing = true
    }
    async flush(): Promise<void> {}
    isBusy() {
      return tts.playing
    }
    interrupt(_options?: { cancelEngine?: boolean }): void {
      tts.playing = false
    }
    dispose(): void {}
  },
}))

import { CompanionStage, type CompanionStageProps } from './CompanionStage'
import { defaultCompanionSettings, saveCompanionSettings } from './companionSettings'
import { COMPANION_PAD_SPEECH } from './companionText'

const baseProps: CompanionStageProps = {
  chatStatus: 'idle',
  assistantText: '',
  chatReady: true,
  onSend: vi.fn(),
  onExit: vi.fn(),
}

const stateOf = (container: HTMLElement) => (container.firstChild as HTMLElement).getAttribute('data-state')
const flush = async (ms: number) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}
const spokenReply = () =>
  tts.enqueueCalls.map(call => call.segments.join('')).filter(text => text !== COMPANION_PAD_SPEECH).join('')
const lastReplyCall = () =>
  [...tts.enqueueCalls].reverse().find(call => call.segments.join('') !== COMPANION_PAD_SPEECH)

beforeEach(() => {
  vi.useFakeTimers()
  speech.callbacks = undefined
  speech.start.mockReset()
  speech.setAssistantPlayback.mockReset()
  speech.start.mockResolvedValue(speech.handle())
  tts.enqueueCalls = []
  tts.playing = false
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})

test('speaks the first finished sentence while the model is still writing', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('今天天气怎么样')
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('thinking')
  expect(tts.enqueueCalls.map(call => call.segments.join(''))).toEqual([])

  // First sentence lands mid-stream: it must reach the engine now, not
  // after the whole reply has been generated.
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="今天多云。" />)
  })
  await flush(0)
  expect(spokenReply()).toBe('今天多云。')
  expect(stateOf(utils.container)).toBe('speaking')

  // The rest of the stream follows without re-speaking the opening.
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="今天多云。气温二十六度。" />)
  })
  await flush(0)
  expect(spokenReply()).toBe('今天多云。气温二十六度。')

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="done" assistantText="今天多云。气温二十六度。" />)
  })
  await flush(0)
  expect(spokenReply()).toBe('今天多云。气温二十六度。')
})

test('an unpunctuated tail still waits for the stream to stall', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('你好月汐')
  })
  await flush(0)

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="嗨我在呢" />)
  })
  await flush(0)
  expect(spokenReply()).toBe('')

  await flush(900)
  expect(spokenReply()).toBe('嗨我在呢')
})

test('nothing she hears mid-reply takes the turn back', async () => {
  // The only two ways out of her turn are the 打断 button and her finishing.
  // A transcript arriving mid-reply is speaker echo far more often than it is
  // the user, and there is no reading of the text that separates the two
  // reliably — so it is dropped rather than acted on, and the reply survives.
  const onSend = vi.fn()
  const onCancel = vi.fn()
  const props = { ...baseProps, onSend, onCancel }
  const utils = render(<CompanionStage {...props} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('讲个长故事')
  })
  await flush(0)
  await act(async () => {
    utils.rerender(<CompanionStage {...props} chatStatus="streaming" assistantText="很久很久以前。" />)
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('speaking')

  await act(async () => {
    speech.callbacks!.onFinal('等一下换个话题')
  })
  await flush(0)

  expect(onCancel).not.toHaveBeenCalled()
  expect(onSend).toHaveBeenCalledTimes(1)
  expect(stateOf(utils.container)).toBe('speaking')
  expect(tts.playing).toBe(true)
})

test('keeps the microphone shut across the gap between her sentences', async () => {
  // She finishes a sentence while the model is still writing the next one.
  // The engine drains, AWAIT_MORE drops the machine back to 'thinking', and
  // the microphone used to reopen right there — into a room still carrying
  // the sentence she just said out loud. On speakerphone that echo came
  // back as a transcript and was taken for the user starting a new turn, so
  // she answered herself. The reply is one turn; the microphone stays shut
  // for all of it.
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('讲讲今天的安排')
  })
  await flush(0)

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="上午没有会。" />)
  })
  await flush(0)
  expect(speech.setAssistantPlayback).toHaveBeenLastCalledWith(true, expect.any(Number))
  // Everything before this point is the microphone being set up for the
  // user's own turn; only what happens after she starts is at issue.
  const shutAt = speech.setAssistantPlayback.mock.calls.length

  // The engine runs dry with the stream still open.
  await act(async () => {
    tts.playing = false
    lastReplyCall()!.callbacks.onFinished?.('completed')
  })
  await flush(400)
  const reopened = speech.setAssistantPlayback.mock.calls.slice(shutAt).filter(call => call[0] === false)
  expect(reopened).toHaveLength(0)
})

test('reopens the microphone once the machine says the turn is the user\u2019s again', async () => {
  // The third turn used to arrive with the stage reading 聆听中 and nothing
  // underneath it listening. speakingRef is set wherever audio is handed to
  // the engine and cleared wherever one finishes, which is more places than
  // reliably agree, and one missed reset was permanent: it kept the reply
  // "in progress" forever, which shut the microphone, stopped the recognizer
  // and disabled both repairs that would have noticed — each of them declines
  // to act during her turn.
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('讲讲今天的安排')
  })
  await flush(0)
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="上午没有会。" />)
  })
  await flush(0)
  expect(speech.setAssistantPlayback).toHaveBeenLastCalledWith(true, expect.any(Number))

  // The engine runs dry while the model is still writing, which is the case
  // that skips the reset: it is deliberately not done here, because more of
  // the reply is still coming and the microphone must stay shut across the
  // gap.
  await act(async () => {
    tts.playing = false
    tts.enqueueCalls.at(-1)!.callbacks.onFinished?.('completed')
  })
  await flush(400)

  // Then the reply ends without ever speaking again — cancelled, failed, or
  // finished with nothing further to say — and the turn returns to the user
  // by a route that never touches the flag.
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="cancelled" assistantText="上午没有会。" />)
  })
  await flush(1200)

  expect(stateOf(utils.container)).toBe('listening')
  expect(speech.setAssistantPlayback).toHaveBeenLastCalledWith(false, expect.any(Number))
})

test('does not paint echo crumbs as the user line while she is speaking', async () => {
  const onSend = vi.fn()
  const props = { ...baseProps, onSend }
  const utils = render(<CompanionStage {...props} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('打开桌面协议')
  })
  await flush(0)
  await act(async () => {
    utils.rerender(<CompanionStage {...props} chatStatus="streaming" assistantText="好，我来执行。" />)
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('speaking')

  await act(async () => {
    speech.callbacks!.onInterim?.('谢你见')
    speech.callbacks!.onFinal('谢你见')
  })
  await flush(0)

  expect(onSend).toHaveBeenCalledTimes(1)
  expect(utils.container.textContent).not.toMatch(/我[\s\S]*谢你见/)
})

test('hands the speech layer what is currently being spoken, for echo rejection', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('你好')
  })
  await flush(0)
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="嗨，我在呢。" />)
  })
  await flush(0)
  expect(speech.callbacks!.spokenText?.()).toContain('嗨，我在呢。')
})

test('the pad finishing does not end her turn while the model is still writing', async () => {
  saveCompanionSettings({ ...defaultCompanionSettings(), instantAck: true })
  const onSend = vi.fn()
  const utils = render(<CompanionStage {...baseProps} onSend={onSend} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('今天天气怎么样')
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('thinking')
  expect(tts.enqueueCalls[0]?.segments.join('')).toBe(COMPANION_PAD_SPEECH)

  await act(async () => {
    tts.playing = false
    tts.enqueueCalls[0].callbacks.onFinished?.('completed')
  })
  await flush(400)
  expect(stateOf(utils.container)).toBe('thinking')
  expect(onSend).toHaveBeenCalledTimes(1)

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} onSend={onSend} chatStatus="streaming" assistantText="今天多云。" />)
  })
  await flush(0)
  expect(spokenReply()).toBe('今天多云。')
  expect(stateOf(utils.container)).toBe('speaking')
})

test('a barge-in from local ASR cuts the reply and starts the next user turn', async () => {
  const onSend = vi.fn()
  const onCancel = vi.fn()
  const props = { ...baseProps, onSend, onCancel }
  const utils = render(<CompanionStage {...props} />)
  await flush(600)
  await act(async () => {
    speech.callbacks!.onFinal('讲个长故事')
  })
  await flush(0)
  await act(async () => {
    utils.rerender(<CompanionStage {...props} chatStatus="streaming" assistantText="很久很久以前。" />)
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('speaking')
  expect(onSend).toHaveBeenCalledTimes(1)

  await act(async () => {
    speech.callbacks!.onBargeIn?.('不是这个')
  })
  await flush(0)

  expect(onCancel).toHaveBeenCalled()
  expect(onSend).toHaveBeenCalledTimes(2)
  expect(onSend).toHaveBeenLastCalledWith('不是这个')
  expect(stateOf(utils.container)).toBe('thinking')
})
