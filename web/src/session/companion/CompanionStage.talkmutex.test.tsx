// Done-flush used to enqueue cascade TTS while talk PCM was still on the
// same TtsPlayer (CompanionStage ~799). Talk owns the speaker; cascade
// must stay off unless tool handoff set talkSuppressPlay.
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { TtsPlayerCallbacks } from './ttsPlayer'
import { defaultCompanionSettings } from './companionSettings'
import type { EntryLight } from './companionLights'

const tts = vi.hoisted(() => ({
  enqueueCalls: [] as Array<{ segments: string[] }>,
  playing: false,
}))

const talk = vi.hoisted(() => ({
  start: vi.fn(),
}))

const lights: [EntryLight, EntryLight, EntryLight] = [
  { key: 'listen', title: '听', label: '火山', state: 'on' },
  { key: 'speak', title: '说', label: '通话核', state: 'on' },
  { key: 'think', title: '想', label: '闪', state: 'on' },
]

vi.mock('../../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../../bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () => Promise.resolve({
        voices: [{ voice_id: 'zh-female', display_name: '月汐温柔女声', gender: 'female' as const, lang: 'zh-CN' }],
      }),
      synthesize: vi.fn(),
      cancel: vi.fn(),
      ensureRefEngine: vi.fn().mockResolvedValue({ state: 'online' }),
      installOnnxEngine: vi.fn().mockResolvedValue({ state: 'ready', percent: 100, doneBytes: 0, totalBytes: 0 }),
    }),
    getProviderBridge: () => ({
      list: () => Promise.resolve({ items: [] }),
    }),
    automationBridge: { listRuns: () => Promise.resolve({ runs: [] }) },
  }
})

vi.mock('./speech', () => ({
  ECHO_GUARD_MS: 90,
  FORCE_COMMIT_MS: 1800,
  stageForceCommitMayBeginTurn: () => false,
  INTERRUPT_ECHO_MS: 80,
  shouldShowSpeechSetupHint: () => false,
  startCompanionSpeech: () => Promise.resolve({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: () => false,
    resumeCapture: vi.fn(),
  }),
}))

vi.mock('./ttsPlayer', () => ({
  unlockTtsAudio: vi.fn(() => Promise.resolve()),
  playCompanionAckPcm: vi.fn(),
  getTtsAudioState: () => 'running' as const,
  TtsPlayer: class {
    configure(): void {}
    async speak(): Promise<void> {}
    enqueue(segments: string[], _settings: unknown, _callbacks: TtsPlayerCallbacks) {
      tts.enqueueCalls.push({ segments })
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
    enqueueTalkPcm(): boolean {
      return true
    }
  },
}))

vi.mock('./prepareCompanionEntry', async importOriginal => {
  const actual = await importOriginal<typeof import('./prepareCompanionEntry')>()
  return {
    ...actual,
    prepareCompanionEntry: async () => ({
      settings: { ...defaultCompanionSettings(), voicePath: 'volc' as const, autoSpeak: true, instantAck: false },
      voicePath: 'volc' as const,
      lights,
      llmReady: true,
      listenReady: true,
      speakReady: true,
      hasVolc: true,
      hasVolcTts: true,
      hasTalkModel: true,
      allowListen: true,
      blockReason: '',
    }),
  }
})

vi.mock('./companionTalk', async importOriginal => {
  const actual = await importOriginal<typeof import('./companionTalk')>()
  return {
    ...actual,
    shouldOfferCompanionTalk: () => true,
    startCompanionTalk: (...args: unknown[]) => talk.start(...args),
  }
})

import { CompanionStage, type CompanionStageProps } from './CompanionStage'

const sessionId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const baseProps: CompanionStageProps = {
  sessionId,
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
  tts.enqueueCalls = []
  tts.playing = false
  talk.start.mockReset()
  talk.start.mockResolvedValue({
    talkId: 'talk-1',
    streamId: 'stream-1',
    cancelOutput: async () => {},
    stop: async () => {},
  })
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})

test('done-flush does not enqueue cascade TTS while talk is live', async () => {
  const utils = render(<CompanionStage {...baseProps} />)
  await flush(800)
  expect(utils.container.firstChild).toHaveAttribute('data-talk-live', 'true')

  await act(async () => {
    utils.rerender(<CompanionStage {...baseProps} chatStatus="done" assistantText="今天多云。气温二十六度。" />)
  })
  await flush(0)

  const spoken = tts.enqueueCalls.map(call => call.segments.join('')).join('')
  expect(spoken).not.toContain('今天多云')
  expect(spoken).not.toContain('二十六度')
})
