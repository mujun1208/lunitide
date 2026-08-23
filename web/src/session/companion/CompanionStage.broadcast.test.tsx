// CompanionStage.broadcast.test.tsx pins the P3-4 end-to-end wiring:
// a run that reaches a terminal state while the moon stage idles is
// spoken through the normal TTS pipeline as a proactive assistant
// round (machine matrix preserved via the retrySegment dispatch
// chain), while the baseline history on mount stays silent.
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { AutomationRunListResult } from '../../generated/bridge'
import type { TtsPlayerCallbacks } from './ttsPlayer'

interface CapturedSpeech {
  onFinal: (transcript: string) => void
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
  }),
}))

const tts = vi.hoisted(() => ({
  speakCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  enqueueCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  playing: false,
}))

const automation = vi.hoisted(() => ({
  runs: [] as AutomationRunListResult['runs'],
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
    automationBridge: {
      listRuns: () => Promise.resolve({ runs: [...automation.runs] }),
    },
  }
})

vi.mock('./speech', () => ({
  ECHO_GUARD_MS: 700,
  INTERRUPT_ECHO_MS: 160,
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
    async speak(segments: string[], _settings: unknown, callbacks: TtsPlayerCallbacks) {
      tts.speakCalls.push({ segments, callbacks })
    }
    enqueue(segments: string[], _settings: unknown, callbacks: TtsPlayerCallbacks) {
      tts.enqueueCalls.push({ segments, callbacks })
      tts.playing = true
    }
    async flush(callbacks: TtsPlayerCallbacks) {
      if (!tts.playing) {
        callbacks.onFinished?.('completed')
        return
      }
      await new Promise<void>(resolve => {
        const finish = () => {
          callbacks.onFinished?.('completed')
          resolve()
        }
        const check = () => {
          if (!tts.playing) finish()
          else setTimeout(check, 40)
        }
        check()
      })
    }
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

const run = (overrides: Partial<AutomationRunListResult['runs'][number]>): AutomationRunListResult['runs'][number] => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  jobId: '01ARZ3NDEKTSV4RRFFQ69G5FAK',
  jobName: '每日站会摘要',
  state: 'succeeded',
  trigger: 'cron',
  summary: '今日待办 3 项',
  startedAt: '2026-08-16T00:30:00Z',
  finishedAt: '2026-08-16T00:30:12Z',
  ...overrides,
})

const stateOf = (container: HTMLElement) => (container.firstChild as HTMLElement).getAttribute('data-state')
const flush = async (ms: number) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  speech.callbacks = undefined
  speech.start.mockReset()
  speech.start.mockRejectedValue(new Error('麦克风不可用'))
  tts.speakCalls = []
  tts.enqueueCalls = []
  automation.runs = []
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})

test('speaks a run that finishes while the stage idles, as a proactive assistant round', async () => {
  automation.runs = [run({})] // Baseline history present on mount.
  const { container } = render(<CompanionStage {...baseProps} />)
  await flush(600) // Mount: voices probe + baseline poll + failed mic auto-start.
  expect(stateOf(container)).toBe('idle')
  expect(tts.speakCalls.length).toBe(0)

  automation.runs = [run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F99', jobName: '周报生成', summary: '本周进展已汇总' }), ...automation.runs]
  await flush(30_000) // One poll cadence later the linkage fires.

  expect(tts.speakCalls.length).toBe(1)
  const spoken = tts.speakCalls[0].segments.join('')
  expect(spoken).toContain('自动化任务「周报生成」已完成')
  expect(spoken).toContain('本周进展已汇总')
  expect(stateOf(container)).toBe('speaking')
  const log = container.querySelector('.companion-subtitle-list') as HTMLElement
  expect(log.textContent).toContain('自动化任务「周报生成」已完成')
})

test('keeps the mount-time baseline silent', async () => {
  automation.runs = [run({ state: 'failed', error: '模型网关超时' })]
  const { container } = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await flush(30_000)
  expect(tts.speakCalls.length).toBe(0)
  expect(stateOf(container)).toBe('idle')
})

test('drops the broadcast when the stage is busy speaking a reply', async () => {
  automation.runs = [run({})]
  // Open the microphone so the reply chain is legal: listening →
  // thinking (final transcript) → speaking (reply completed).
  speech.start.mockResolvedValue(speech.handle())
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  expect(stateOf(utils.container)).toBe('listening')

  await act(async () => {
    speech.callbacks!.onFinal('看看自动化')
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('thinking')

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="done" assistantText="这是答复内容。" />)
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('speaking')
  expect(tts.enqueueCalls.length).toBe(1) // streaming reply only — no filler ack

  automation.runs = [run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F98', jobName: '并发任务' }), ...automation.runs]
  await flush(30_000)
  const broadcasts = [...tts.speakCalls, ...tts.enqueueCalls].filter(call => call.segments.join('').includes('自动化任务'))
  expect(broadcasts.length).toBe(0)
})

test('returns to idle when chat fails while the stage is speaking', async () => {
  speech.start.mockResolvedValue(speech.handle())
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  expect(stateOf(utils.container)).toBe('listening')
  await act(async () => {
    speech.callbacks!.onFinal('你好')
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('thinking')
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="done" assistantText="这是答复内容。" />)
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('speaking')
  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="failed" assistantText="这是答复内容。" error={undefined} />)
  })
  await flush(0)
  expect(stateOf(utils.container)).toBe('idle')
})
