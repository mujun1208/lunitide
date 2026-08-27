// Which recognizer a turn opens on.
//
// 'auto' means "the local model when it is installed", and answering that
// takes a bridge round trip. The stage starts listening the moment it mounts,
// so the answer arrives after the decision has already been made — and taking
// the default in the meantime chose the system recognizer every time, for
// good, however plainly the settings screen said the model was installed.
//
// That is worth a test of its own because nothing about it is visible: both
// recognizers wear the same handle, the stage looks identical either way, and
// the only symptom is that every fix made to the local path has no effect.
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

vi.mock('./localAsr', () => ({
  localAsrStatus: () => recognizers.probe,
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

test('waits for the local-model probe before choosing a recognizer', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  // The stage is already listening; the probe has not answered yet. Nothing
  // may be opened on the strength of not knowing.
  await flush(600)
  expect(recognizers.cloud).not.toHaveBeenCalled()
  expect(recognizers.local).not.toHaveBeenCalled()

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
  utils.unmount()
})
