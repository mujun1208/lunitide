// Which recognizer a turn opens on.
//
// 'auto' prefers the local model when it is installed. The probe is a
// bridge round trip and used to be awaited with no budget, so a hung
// voice.status left 对话模式 showing 聆听中 with no recognizer at all.
// The system recognizer must open once that budget expires; a prompt
// local-ready answer still wins.
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

const recognizers = vi.hoisted(() => ({
  cloud: vi.fn(),
  local: vi.fn(),
  /** Resolves the local-model probe, so a test can hold it open. */
  settleProbe: (() => {}) as (ready: boolean) => void,
  probe: undefined as Promise<{ supported: boolean; ready: boolean }> | undefined,
  handle: () => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

vi.mock('../../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../../bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () => Promise.resolve({ voices: [] }),
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
  startCompanionSpeech: (...args: unknown[]) => {
    recognizers.cloud(...args)
    return Promise.resolve(recognizers.handle())
  },
}))

vi.mock('./localSpeech', () => ({
  startLocalCompanionSpeech: (...args: unknown[]) => {
    recognizers.local(...args)
    return Promise.resolve(recognizers.handle())
  },
}))

vi.mock('./localAsr', async importOriginal => {
  const actual = await importOriginal<typeof import('./localAsr')>()
  return {
    ...actual,
    localAsrStatus: () => recognizers.probe,
  }
})

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
import { LOCAL_ASR_DECISION_MS } from './localAsr'

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
  recognizers.cloud.mockReset()
  recognizers.local.mockReset()
  recognizers.probe = new Promise(resolve => {
    recognizers.settleProbe = (ready: boolean) => resolve({ supported: true, ready })
  })
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})

test('opens the system recognizer when the local-model probe hangs', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(LOCAL_ASR_DECISION_MS + 50)

  expect(recognizers.cloud).toHaveBeenCalled()
  expect(recognizers.local).not.toHaveBeenCalled()
  utils.unmount()
})

test('prefers the local recognizer when the probe answers ready in time', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await act(async () => {
    recognizers.settleProbe(true)
  })
  await flush(50)

  expect(recognizers.local).toHaveBeenCalled()
  expect(recognizers.cloud).not.toHaveBeenCalled()
  utils.unmount()
})

test('falls back to the system recognizer when the model is not installed', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(600)
  await act(async () => {
    recognizers.settleProbe(false)
  })
  await flush(50)

  expect(recognizers.cloud).toHaveBeenCalled()
  expect(recognizers.local).not.toHaveBeenCalled()
  expect(() => utils.unmount()).not.toThrow()
})
