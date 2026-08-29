// Task-turn UX for 月伴: 执行中 while tools run, 无法执行 when they
// fail, leftover 说话中 clears when playback ends, and a deaf local
// recognizer is restarted instead of hanging on 「还没有出字」.
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { BridgeClientError } from '../../bridge/client'
import type { TtsPlayerCallbacks } from './ttsPlayer'

interface CapturedSpeech {
  onFinal: (transcript: string) => void
  onInterim?: (transcript: string) => void
  onVoiceEnergy?: () => void
  onError: (error: unknown) => void
}

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  callbacks: undefined as CapturedSpeech | undefined,
  stop: vi.fn(),
  handle: () => ({
    stop: speech.stop,
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

const tts = vi.hoisted(() => ({
  speakCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  enqueueCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  interrupts: 0,
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
  FORCE_COMMIT_MS: 1800,
  INTERRUPT_ECHO_MS: 80,
  shouldShowSpeechSetupHint: () => false,
  startCompanionSpeech: (callbacks: CapturedSpeech) => {
    speech.callbacks = callbacks
    return speech.start(callbacks)
  },
}))

vi.mock('./localSpeech', () => ({
  startLocalCompanionSpeech: () => Promise.resolve(speech.handle()),
}))

vi.mock('./localAsr', async importOriginal => {
  const actual = await importOriginal<typeof import('./localAsr')>()
  return {
    ...actual,
    localAsrStatus: () => Promise.resolve({ supported: true, ready: false }),
  }
})

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
    interrupt() {
      tts.interrupts++
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

const stage = (container: HTMLElement) => container.firstChild as HTMLElement
const liveLog = (container: HTMLElement) => container.querySelector('.companion-subtitle-list') as HTMLElement
const statusRegion = (container: HTMLElement) => container.querySelector('.companion-status') as HTMLElement
const stateOf = (container: HTMLElement) => stage(container).getAttribute('data-state')

async function renderStage(overrides: Partial<CompanionStageProps> = {}) {
  const handle = speech.handle()
  speech.start.mockResolvedValue(handle)
  const props = { ...baseProps, ...overrides }
  const utils = render(<CompanionStage {...props} />)
  await waitFor(() => expect(stateOf(utils.container)).toBe('listening'), { timeout: 3000 })
  return { ...utils, props, handle }
}

beforeEach(() => {
  speech.callbacks = undefined
  speech.stop.mockReset()
  speech.start.mockReset()
  tts.speakCalls = []
  tts.enqueueCalls = []
  tts.interrupts = 0
  tts.playing = false
  vi.mocked(baseProps.onSend).mockClear()
  vi.mocked(baseProps.onExit).mockClear()
  localStorage.clear()
})

afterEach(cleanup)

test('tools show 执行中 and speak 正在执行 instead of idle chat', async () => {
  const onSend = vi.fn()
  const { container, rerender } = await renderStage({ onSend })
  await act(async () => {
    speech.callbacks!.onFinal('打开协议，在证件号码后面写')
  })
  expect(stateOf(container)).toBe('thinking')
  rerender(
    <CompanionStage
      {...baseProps}
      onSend={onSend}
      chatStatus="streaming"
      assistantText=""
      activityStatus="打开桌面文件中…"
    />,
  )
  await waitFor(() => expect(statusRegion(container).textContent).toContain('执行中'))
  expect(stateOf(container)).not.toBe('speaking')
  expect(liveLog(container).textContent).toContain('打开桌面文件中')
  await waitFor(() => expect(tts.enqueueCalls.some(call => call.segments.join('').includes('正在执行'))).toBe(true))
})

test('failed desktop work speaks 无法执行 and shows the reason', async () => {
  const onSend = vi.fn()
  const { container, rerender } = await renderStage({ onSend })
  await act(async () => {
    speech.callbacks!.onFinal('在证件号码后面填')
  })
  rerender(
    <CompanionStage
      {...baseProps}
      onSend={onSend}
      chatStatus="failed"
      assistantText=""
      error={new BridgeClientError('找不到证件号码', 'DESKTOP_TYPE_FAILED', true, 'engine')}
    />,
  )
  await waitFor(() => expect(liveLog(container).textContent).toContain('无法执行'))
  expect(liveLog(container).textContent).toContain('找不到证件号码')
  await waitFor(() => expect(tts.speakCalls.some(call => call.segments.join('').includes('无法执行'))).toBe(true))
})

test('playback end clears 说话中 and resumes listen for the rest of the reply', async () => {
  const onSend = vi.fn()
  const { container, rerender } = await renderStage({ onSend })
  await act(async () => {
    speech.callbacks!.onFinal('你好月汐')
  })
  const reply = '你好呀，我是月汐。今天想聊点什么呀？'
  rerender(
    <CompanionStage {...baseProps} onSend={onSend} chatStatus="streaming" assistantText={reply} />,
  )
  await waitFor(() => expect(stateOf(container)).toBe('speaking'))
  expect(statusRegion(container).textContent).toContain('说话中')
  rerender(<CompanionStage {...baseProps} onSend={onSend} chatStatus="done" assistantText={reply} />)
  await act(async () => {
    tts.playing = false
    tts.enqueueCalls.at(-1)?.callbacks.onFinished?.('completed')
  })
  await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 4000 })
  expect(statusRegion(container).textContent).toContain('聆听中')
  expect(statusRegion(container).textContent).not.toContain('说话中')
  expect(liveLog(container).textContent).toContain('今天想聊点什么')
})

test('heard-you-no-glyphs restarts the recognizer instead of hanging', async () => {
  const { container, handle } = await renderStage()
  expect(stateOf(container)).toBe('listening')
  const started = speech.start.mock.calls.length
  for (let i = 0; i < 12; i++) {
    await act(async () => {
      speech.callbacks!.onVoiceEnergy?.()
      await new Promise(resolve => setTimeout(resolve, 500))
    })
  }
  expect(liveLog(container).textContent).toMatch(/还没有出字/)
  expect(handle.pulseRecognition).toHaveBeenCalled()
  expect(speech.stop).toHaveBeenCalled()
  expect(speech.start.mock.calls.length).toBeGreaterThan(started)
}, 15_000)

test('打断 during a task resumes listen so the next utterance is accepted', async () => {
  const onSend = vi.fn()
  const onCancel = vi.fn()
  const { container, rerender, handle } = await renderStage({ onSend, onCancel })
  await act(async () => {
    speech.callbacks!.onFinal('打开协议')
  })
  rerender(
    <CompanionStage
      {...baseProps}
      onSend={onSend}
      onCancel={onCancel}
      chatStatus="streaming"
      assistantText=""
      activityStatus="输入文字中…"
    />,
  )
  await waitFor(() => expect(statusRegion(container).textContent).toContain('执行中'))
  fireEvent.click(container.querySelector('.companion-interrupt') as HTMLButtonElement)
  expect(onCancel).toHaveBeenCalled()
  await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
  expect(handle.resumeCapture).toHaveBeenCalled()
  await act(async () => {
    speech.callbacks!.onFinal('改填身份证号')
  })
  expect(onSend).toHaveBeenLastCalledWith('改填身份证号')
})
