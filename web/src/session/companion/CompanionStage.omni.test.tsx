// Entering 对话模式 must not hard-fail when MiniCPM-o is missing.
// Omni is optional: fall back to the existing companion ASR path and show
// a calm install hint instead of a fatal OMNI_UNAVAILABLE banner.
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

interface CapturedSpeech {
  onFinal: (transcript: string) => void
  onInterim?: (transcript: string) => void
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

const omniAudio = vi.hoisted(() => ({
  probe: vi.fn(),
  start: vi.fn(),
}))

const omni = vi.hoisted(() => ({
  status: vi.fn(),
  install: vi.fn(),
  ensure: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
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
    getOmniBridge: () => omni,
    automationBridge: { listRuns: () => Promise.resolve({ runs: [] }) },
  }
})

vi.mock('../omni/omniAudio', async importOriginal => {
  const actual = await importOriginal<typeof import('../omni/omniAudio')>()
  return {
    ...actual,
    probeOmniChannel: () => omniAudio.probe(),
    startOmniCompanion: (...args: unknown[]) => omniAudio.start(...args),
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

vi.mock('./localAsr', () => ({
  localAsrStatus: () => Promise.resolve({ supported: true, ready: false }),
}))

vi.mock('./ttsPlayer', () => ({
  unlockTtsAudio: vi.fn(() => Promise.resolve()),
  getTtsAudioState: () => 'running' as const,
  TtsPlayer: class {
    configure(): void {}
    async speak(): Promise<void> {}
    enqueue(): void {}
    async flush(): Promise<void> {}
    isBusy() {
      return false
    }
    interrupt(): void {}
    dispose(): void {}
  },
}))

import { CompanionStage, type CompanionStageProps } from './CompanionStage'
import { defaultCompanionSettings } from './companionSettings'

const baseProps: CompanionStageProps = {
  chatStatus: 'idle',
  assistantText: '',
  chatReady: true,
  onSend: vi.fn(),
  onExit: vi.fn(),
}

const flush = async (ms: number) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  speech.callbacks = undefined
  speech.start.mockReset()
  speech.stop.mockReset()
  speech.start.mockResolvedValue(speech.handle())
  omniAudio.probe.mockReset()
  omniAudio.start.mockReset()
  omni.status.mockReset()
  omni.install.mockReset()
  omni.status.mockResolvedValue({
    supported: true,
    ready: false,
    installed: false,
    runtimeFound: false,
    hostState: 'missing_runtime',
    downloadBytes: 9_000_000_000,
    title: 'MiniCPM-o 4.5 Q4',
    percent: 0,
    doneBytes: 0,
    totalBytes: 0,
  })
  localStorage.clear()
  localStorage.setItem(
    'lunitide:companion',
    JSON.stringify({ ...defaultCompanionSettings(), voicePath: 'omni', rev: 9 }),
  )
  vi.mocked(baseProps.onSend).mockClear()
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})

test('entering companion without omni runtime starts speech and skips the fatal banner', async () => {
  omniAudio.probe.mockResolvedValue(false)
  omniAudio.start.mockRejectedValue(new Error('本机 MiniCPM-o 推理进程未能展开，请重装月汐后再试'))
  const { container } = render(<CompanionStage {...baseProps} />)
  await flush(80)

  expect(omniAudio.start).not.toHaveBeenCalled()
  expect(speech.start).toHaveBeenCalled()
  expect(container.querySelector('.companion-banner.error')).toBeNull()
  expect(container.textContent).not.toContain('OMNI_UNAVAILABLE')
  expect(container.querySelector('.companion-omni-hint')).toBeTruthy()
  expect(container.textContent).toContain('已用现有语音通道继续对话')

  await act(async () => {
    speech.callbacks!.onFinal('你好月汐')
  })
  expect(baseProps.onSend).toHaveBeenCalledWith('你好月汐')
})

test('a MiniCPM-o start failure falls back to ASR instead of a blocking error', async () => {
  omniAudio.probe.mockResolvedValue(true)
  omniAudio.start.mockRejectedValue(new Error('本机 MiniCPM-o 推理进程未能展开，请重装月汐后再试'))
  const { container } = render(<CompanionStage {...baseProps} />)
  await flush(80)

  expect(omniAudio.start).toHaveBeenCalled()
  expect(speech.start).toHaveBeenCalled()
  expect(container.querySelector('.companion-banner.error')).toBeNull()
  expect(container.textContent).not.toContain('OMNI_UNAVAILABLE')
  expect(container.querySelector('.companion-omni-hint')).toBeTruthy()
})

test('never treats OMNI_UNAVAILABLE copy as the first user turn', async () => {
  omniAudio.probe.mockResolvedValue(false)
  const { container } = render(<CompanionStage {...baseProps} />)
  await flush(80)
  await act(async () => {
    speech.callbacks!.onFinal('本机 MiniCPM-o 推理进程未能展开，请重装月汐后再试 代码 OMNI_UNAVAILABLE')
  })
  expect(baseProps.onSend).not.toHaveBeenCalled()
  expect(container.textContent).not.toContain('OMNI_UNAVAILABLE')
})
