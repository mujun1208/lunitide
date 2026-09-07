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
  pcmCalls: 0,
}))

const talk = vi.hoisted(() => ({
  start: vi.fn(),
  providerList: vi.fn().mockResolvedValue({ items: [] }),
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
      list: () => talk.providerList(),
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
      tts.pcmCalls++
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
  tts.pcmCalls = 0
  talk.start.mockReset()
  talk.providerList.mockClear()
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

test('tool handoff forwards the acknowledged user message to the parent sender', async () => {
  const onSend = vi.fn().mockResolvedValue(true)
  render(<CompanionStage {...baseProps} onSend={onSend} />)
  await flush(800)
  const callbacks = talk.start.mock.calls[0][0]
  await act(async () => { callbacks.onToolHandoff('打开网页', sessionId) })
  await flush(0)
  expect(onSend).toHaveBeenCalledWith('打开网页', sessionId)
})

test('late native callbacks cannot play audio or dispatch tasks after leaving the stage',async()=>{
  const onSend=vi.fn().mockResolvedValue(true)
  const ui=render(<CompanionStage {...baseProps} onSend={onSend}/> )
  await flush(800)
  const callbacks=talk.start.mock.calls[0][0]
  ui.unmount()
  await act(async()=>{
    callbacks.onAudio('late-audio','audio/pcm;rate=24000')
    callbacks.onUserTranscript('旧连接的文字')
    callbacks.onToolHandoff('不要执行这条旧指令',sessionId)
    callbacks.onEnded()
  })
  expect(tts.pcmCalls).toBe(0)
  expect(onSend).not.toHaveBeenCalled()
})

test('native startup ending before the handle still reaches the selected ASR fallback', async () => {
  talk.start.mockImplementationOnce(async callbacks => {
    callbacks.onEnded()
    return undefined
  })
  const ui = render(<CompanionStage {...baseProps} />)
  await flush(800)
  expect(talk.providerList).toHaveBeenCalled()
  // No configured ASR in this fixture: fallback must reach its normal
  // configuration message instead of silently abandoning startup.
  expect(ui.container.textContent).toContain('VOICE-004')
})

test('failed native startup retires late callbacks after ASR fallback takes over', async () => {
  talk.start.mockResolvedValueOnce(undefined)
  const onSend = vi.fn().mockResolvedValue(true)
  const ui = render(<CompanionStage {...baseProps} onSend={onSend} />)
  await flush(800)
  const callbacks = talk.start.mock.calls[0][0]
  await act(async () => {
    callbacks.onAudio('old-audio', 'audio/pcm;rate=24000')
    callbacks.onToolHandoff('旧连接不应执行', sessionId)
    callbacks.onEnded()
  })
  expect(tts.pcmCalls).toBe(0)
  expect(onSend).not.toHaveBeenCalled()
  expect(ui.container.textContent).toContain('VOICE-004')
})

test('ended native connection ignores late audio and tasks before another connection starts', async () => {
  const onSend = vi.fn().mockResolvedValue(true)
  render(<CompanionStage {...baseProps} onSend={onSend} />)
  await flush(800)
  const callbacks = talk.start.mock.calls[0][0]
  await act(async () => {
    callbacks.onEnded()
    callbacks.onAudio('late-audio', 'audio/pcm;rate=24000')
    callbacks.onToolHandoff('旧连接不应再执行', sessionId)
  })
  expect(tts.pcmCalls).toBe(0)
  expect(onSend).not.toHaveBeenCalled()
})

test('native speech resumes after a tool turn and ignores the old native answer during handoff', async () => {
  const onSend = vi.fn().mockResolvedValue(true)
  const onCancel = vi.fn()
  const ui = render(<CompanionStage {...baseProps} onSend={onSend} onCancel={onCancel} />)
  await flush(800)
  const callbacks = talk.start.mock.calls[0][0]
  await act(async () => {
    callbacks.onUserTranscript('查询天气')
    callbacks.onToolHandoff('查询天气', sessionId)
    callbacks.onAudio('old-pcm', 'audio/pcm;rate=24000')
    callbacks.onAssistantTranscript('旧通话回答不应盖住工具结果')
  })
  expect(tts.pcmCalls).toBe(0)
  expect(ui.container.textContent).not.toContain('旧通话回答')
  await act(async () => {
    ui.rerender(<CompanionStage {...baseProps} onSend={onSend} onCancel={onCancel} chatStatus="done" assistantText="合肥多云，二十六度。" />)
  })
  await flush(0)
  expect(tts.enqueueCalls.flatMap(call => call.segments).join('')).toContain('二十六度')
  await act(async () => {
    callbacks.onUserTranscript('再讲一个笑话')
    callbacks.onAudio('new-pcm', 'audio/pcm;rate=24000')
    callbacks.onAssistantTranscript('这是一轮新的回答。')
  })
  expect(onCancel).toHaveBeenCalled()
  expect(tts.pcmCalls).toBe(1)
  expect(ui.container.textContent).toContain('这是一轮新的回答')
  expect(ui.container.textContent).not.toContain('合肥多云')
})
