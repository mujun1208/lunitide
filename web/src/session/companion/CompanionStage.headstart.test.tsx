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
  handle: () => ({
    stop: speech.stop,
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    setBargeInActive: vi.fn(),
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
    }),
    automationBridge: { listRuns: () => Promise.resolve({ runs: [] }) },
  }
})

vi.mock('./speech', () => ({
  ECHO_GUARD_MS: 90,
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
    interrupt(): void {
      tts.playing = false
    }
    dispose(): void {}
  },
}))

import { CompanionStage, type CompanionStageProps } from './CompanionStage'

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
const spokenText = () => tts.enqueueCalls.map(call => call.segments.join('')).join('')

beforeEach(() => {
  vi.useFakeTimers()
  speech.callbacks = undefined
  speech.start.mockReset()
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

  // First sentence lands mid-stream: it must reach the engine now, not
  // after the whole reply has been generated.
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="今天多云。" />)
  })
  await flush(0)
  expect(tts.enqueueCalls.length).toBe(1)
  expect(tts.enqueueCalls[0].segments.join('')).toBe('今天多云。')
  expect(stateOf(utils.container)).toBe('speaking')

  // The rest of the stream follows without re-speaking the opening.
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="streaming" assistantText="今天多云。气温二十六度。" />)
  })
  await flush(0)
  expect(spokenText()).toBe('今天多云。气温二十六度。')

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="done" assistantText="今天多云。气温二十六度。" />)
  })
  await flush(0)
  expect(spokenText()).toBe('今天多云。气温二十六度。')
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
  expect(tts.enqueueCalls.length).toBe(0)

  await flush(900)
  expect(spokenText()).toBe('嗨我在呢')
})

test('talking over her cuts the reply and starts the new turn', async () => {
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
    speech.callbacks!.onBargeIn!('等一下换个话题')
  })
  await flush(0)

  expect(onCancel).toHaveBeenCalled()
  expect(onSend).toHaveBeenCalledWith('等一下换个话题')
  expect(stateOf(utils.container)).toBe('thinking')
  expect(tts.playing).toBe(false)
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
