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
  volc: vi.fn(),
  listHang: false,
  /** Resolves the local-model probe, so a test can hold it open. */
  settleProbe: (() => {}) as (ready: boolean) => void,
  probe: undefined as Promise<{ supported: boolean; ready: boolean }> | undefined,
  providers: [] as Array<{
    id: string
    name: string
    protocol: string
    status: string
    credentialState: string
    models: Array<{ modelId: string; displayName: string; isDefault: boolean; kind: string; kindDefault: boolean }>
  }>,
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
      ensureRefEngine: vi.fn().mockResolvedValue({ state: 'online' }),
    }),
    getProviderBridge: () => ({
      list: () =>
        recognizers.listHang
          ? new Promise<{ items: typeof recognizers.providers }>(() => {})
          : Promise.resolve({ items: recognizers.providers }),
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

vi.mock('./volc/volcSpeech', () => ({
  startVolcCompanionSpeech: (...args: unknown[]) => recognizers.volc(...args),
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
import { applyVoicePath, defaultCompanionSettings, saveCompanionSettings } from './companionSettings'

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
  recognizers.volc.mockReset()
  recognizers.volc.mockResolvedValue(recognizers.handle())
  recognizers.listHang = false
  recognizers.providers = [chatProvider]
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
  expect(utils.container.querySelector('[data-asr-route="cloud"]')).toBeTruthy()
  expect(utils.container.textContent).toMatch(/系统识别/)
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
  expect(utils.container.querySelector('[data-asr-route="local"]')).toBeTruthy()
  expect(utils.container.querySelector('[data-light="listen"]')?.textContent).toMatch(/本机 sherpa/)
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
  expect(utils.container.querySelector('[data-asr-route="cloud"]')).toBeTruthy()
  expect(utils.container.textContent).toMatch(/离开本机/)
  expect(() => utils.unmount()).not.toThrow()
})

test('does not arm the hands-free loop when full duplex is off', async () => {
  saveCompanionSettings({ ...defaultCompanionSettings(), fullDuplex: false })
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(LOCAL_ASR_DECISION_MS + 50)
  expect(recognizers.cloud).toHaveBeenCalled()
  expect(utils.container.querySelector('.companion-stage')?.getAttribute('data-hands-free')).toBe('false')
  utils.unmount()
})

test('arms the hands-free loop by default', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(LOCAL_ASR_DECISION_MS + 50)
  expect(recognizers.cloud).toHaveBeenCalled()
  expect(utils.container.querySelector('.companion-stage')?.getAttribute('data-hands-free')).toBe('true')
  utils.unmount()
})

test('sends the wake remainder as the first user turn', async () => {
  const onSend = vi.fn()
  const utils = render(<CompanionStage {...baseProps} seedPrompt="帮我查天气" onSend={onSend} />)
  await flush(50)
  expect(onSend).toHaveBeenCalledWith('帮我查天气')
  utils.unmount()
})

const chatProvider = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  name: 'Chat',
  protocol: 'openai_compatible',
  status: 'enabled',
  credentialState: 'configured',
  models: [{ modelId: 'chat', displayName: 'Chat', isDefault: true, kind: 'llm', kindDefault: true }],
}

const volcProvider = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'Volc',
  protocol: 'volc_speech',
  status: 'enabled',
  credentialState: 'configured',
  models: [{ modelId: 'seed-asr-2.0', displayName: 'seed-asr 2.0', isDefault: true, kind: 'voice', kindDefault: true }],
}

test('opens volc seed-asr when the 火山 path is saved', async () => {
  saveCompanionSettings(applyVoicePath(defaultCompanionSettings(), 'volc'))
  recognizers.providers = [chatProvider, volcProvider]
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(50)

  expect(recognizers.volc).toHaveBeenCalled()
  expect(recognizers.cloud).not.toHaveBeenCalled()
  expect(recognizers.local).not.toHaveBeenCalled()
  expect(utils.container.querySelector('[data-asr-route="volc"]')).toBeTruthy()
  expect(utils.container.textContent).toMatch(/火山听写/)
  utils.unmount()
})

test('does not fall back to system recognition when volc handshake fails', async () => {
  saveCompanionSettings(applyVoicePath(defaultCompanionSettings(), 'volc'))
  recognizers.providers = [chatProvider, volcProvider]
  recognizers.volc.mockRejectedValueOnce(new Error('handshake'))
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(LOCAL_ASR_DECISION_MS + 50)

  expect(recognizers.volc).toHaveBeenCalled()
  expect(recognizers.cloud).not.toHaveBeenCalled()
  expect(utils.container.querySelector('[data-asr-route="cloud"]')).toBeNull()
  expect(utils.container.textContent).toMatch(/VOICE-004/)
  utils.unmount()
})

test('does not fall back to system recognition when the volc provider list hangs', async () => {
  saveCompanionSettings(applyVoicePath(defaultCompanionSettings(), 'volc'))
  recognizers.listHang = true
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(850)

  expect(recognizers.volc).not.toHaveBeenCalled()
  expect(recognizers.cloud).not.toHaveBeenCalled()
  expect(utils.container.querySelector('[data-asr-route="cloud"]')).toBeNull()
  expect(utils.container.textContent).toMatch(/VOICE-004/)
  utils.unmount()
})

test('blocks 本地 entry when the sherpa probe hangs', async () => {
  saveCompanionSettings(applyVoicePath(defaultCompanionSettings(), 'local'))
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(850)

  expect(recognizers.local).not.toHaveBeenCalled()
  expect(recognizers.cloud).not.toHaveBeenCalled()
  expect(recognizers.volc).not.toHaveBeenCalled()
  expect(utils.container.querySelector('[data-asr-route="local"]')).toBeNull()
  expect(utils.container.textContent).toMatch(/sherpa/)
  expect(utils.container.querySelector('[data-light="listen"]')?.className).toMatch(/off/)
  utils.unmount()
})

test('shows 听 说 想 lights after a cloud entry', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(LOCAL_ASR_DECISION_MS + 50)

  expect(utils.container.querySelectorAll('[data-light]')).toHaveLength(3)
  expect(utils.container.querySelector('[data-light="listen"]')?.textContent).toMatch(/系统识别/)
  expect(utils.container.querySelector('[data-light="speak"]')?.textContent).toMatch(/晓晓/)
  expect(utils.container.querySelector('[data-light="think"]')?.textContent).toMatch(/chat/)
  const chrome = utils.container.querySelector('.companion-chrome')
  expect(chrome?.contains(utils.container.querySelector('.companion-exit'))).toBe(true)
  expect(chrome?.contains(utils.container.querySelector('.companion-interrupt'))).toBe(true)
  expect(chrome?.contains(utils.container.querySelector('.companion-lights'))).toBe(true)
  expect(
    [...(chrome?.querySelectorAll('.companion-exit, .companion-interrupt, .companion-pause, [data-light]') ?? [])].map(el =>
      el.classList.contains('companion-exit')
        ? 'exit'
        : el.classList.contains('companion-interrupt')
          ? 'interrupt'
          : el.classList.contains('companion-pause')
            ? 'pause'
            : el.getAttribute('data-light'),
    ),
  ).toEqual(['exit', 'interrupt', 'pause', 'listen', 'speak', 'think'])
  utils.unmount()
})

test('does not open speech when 本地 sherpa is not ready', async () => {
  saveCompanionSettings(applyVoicePath(defaultCompanionSettings(), 'local'))
  recognizers.probe = Promise.resolve({ supported: true, ready: false })
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(50)

  expect(recognizers.local).not.toHaveBeenCalled()
  expect(recognizers.cloud).not.toHaveBeenCalled()
  expect(utils.container.textContent).toMatch(/sherpa/)
  utils.unmount()
})
