import { describe, expect, test, vi } from 'vitest'
import { BridgeClientError, type TalkStreamEvent } from '../../bridge/client'
import type { ProviderDTO } from '../../generated/bridge'
import {
  companionCascadeSpeechBlocked,
  isCompanionIdleChat,
  shouldOfferCompanionTalk,
  startCompanionTalk,
  TALK_FALLBACK_BANNER,
} from './companionTalk'

const sessionId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const realtime: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'Chat',
  protocol: 'openai_compatible',
  baseUrl: 'https://example.com',
  status: 'enabled',
  credentialState: 'configured',
  createdAt: '',
  updatedAt: '',
  version: 1,
  models: [{ modelId: 'gpt-4o-realtime-preview', displayName: 'Realtime', isDefault: true, kind: 'llm' }],
}

const airOnly: ProviderDTO = {
  ...realtime,
  models: [{ modelId: 'glm-4-air', displayName: 'Air', isDefault: true, kind: 'llm' }],
}

describe('companionTalk helpers', () => {
  test('offers talk only on volc with a listed model and a session', () => {
    expect(shouldOfferCompanionTalk('volc', true, sessionId)).toBe(true)
    expect(shouldOfferCompanionTalk('cloud', true, sessionId)).toBe(false)
    expect(shouldOfferCompanionTalk('local', true, sessionId)).toBe(false)
    expect(shouldOfferCompanionTalk('volc', false, sessionId)).toBe(false)
    expect(shouldOfferCompanionTalk('volc', true, '')).toBe(false)
  })

  test('idle chat strings skip chat.start', () => {
    expect(isCompanionIdleChat('今晚月色如何')).toBe(true)
    expect(isCompanionIdleChat('你好')).toBe(true)
    expect(isCompanionIdleChat('继续聊')).toBe(true)
    expect(isCompanionIdleChat('帮我打开网易云')).toBe(false)
  })

  test('fallback banner is honest', () => {
    expect(TALK_FALLBACK_BANNER).toMatch(/用语模型/)
  })

  test('cascade TTS stays off while talk owns the speaker', () => {
    expect(companionCascadeSpeechBlocked({ talkLive: true, talkPending: false })).toBe(true)
    expect(companionCascadeSpeechBlocked({ talkLive: false, talkPending: true })).toBe(true)
    expect(companionCascadeSpeechBlocked({ talkLive: false, talkPending: false })).toBe(false)
    expect(companionCascadeSpeechBlocked({ talkLive: true, talkPending: false, talkSuppressPlay: true })).toBe(false)
    expect(companionCascadeSpeechBlocked({ talkLive: false, talkPending: true, talkSuppressPlay: true })).toBe(false)
  })
})

describe('startCompanionTalk', () => {
  test('returns undefined when no realtime model is listed', async () => {
    const handle = await startCompanionTalk(
      {
        sessionId,
        onAudio: () => {},
        onUserTranscript: () => {},
        onAssistantTranscript: () => {},
        onBarge: () => {},
        onToolHandoff: () => {},
        onError: () => {},
        onEnded: () => {},
      },
      { listProviders: async () => ({ items: [airOnly] }) },
    )
    expect(handle).toBeUndefined()
  })

  test('returns undefined when talk.start is unready', async () => {
    const handle = await startCompanionTalk(
      {
        sessionId,
        onAudio: () => {},
        onUserTranscript: () => {},
        onAssistantTranscript: () => {},
        onBarge: () => {},
        onToolHandoff: () => {},
        onError: () => {},
        onEnded: () => {},
      },
      {
        listProviders: async () => ({ items: [realtime] }),
        talk: {
          start: async () => {
            throw new BridgeClientError('通话核适配还没接通，这轮用语模型', 'TALK_ADAPTER_UNREADY', true, 'x')
          },
        },
      },
    )
    expect(handle).toBeUndefined()
  })

  test('keeps the session after first audio and hands off a complete tool line', async () => {
    const events: Array<(event: TalkStreamEvent) => void> = []
    const handed: string[] = []
    const handle = await startCompanionTalk(
      {
        sessionId,
        onAudio: () => {},
        onUserTranscript: () => {},
        onAssistantTranscript: () => {},
        onBarge: () => {},
        onToolHandoff: text => handed.push(text),
        onError: () => {},
        onEnded: () => {},
      },
      {
        listProviders: async () => ({ items: [realtime] }),
        firstAudioMs: 200,
        capture: async () => ({
          stop: async () => {},
          setMuted: () => {},
          contextSampleRate: () => 16000,
          flush: () => {},
          attachExtraStream: () => {},
        }),
        talk: {
          start: async (_payload, onEvent) => {
            events.push(onEvent)
            queueMicrotask(() => onEvent({ type: 'audio', audioBase64: 'AAAA', mime: 'audio/pcm' }))
            return {
              talkId: 'talk-1',
              streamId: sessionId,
              sessionId,
              done: Promise.resolve(),
              append: async () => true,
              cancel: async () => {},
            }
          },
        },
      },
    )
    expect(handle).toBeDefined()
    events[0]?.({ type: 'tool', name: 'handoff', text: '打开网页' })
    expect(handed).toEqual(['打开网页'])
    events[0]?.({ type: 'tool', name: 'handoff', text: '帮我' })
    expect(handed).toEqual(['打开网页'])
    await handle?.stop()
  })

  test('returns a live handle without waiting for the first assistant audio', async () => {
    const handle = await startCompanionTalk(
      {
        sessionId,
        onAudio: () => {},
        onUserTranscript: () => {},
        onAssistantTranscript: () => {},
        onBarge: () => {},
        onToolHandoff: () => {},
        onError: () => {},
        onEnded: () => {},
      },
      {
        listProviders: async () => ({ items: [realtime] }),
        capture: async () => ({
          stop: async () => {},
          setMuted: () => {},
          contextSampleRate: () => 16000,
          flush: () => {},
          attachExtraStream: () => {},
        }),
        talk: {
          start: async () => ({
            talkId: 'talk-1',
            streamId: sessionId,
            sessionId,
            done: Promise.resolve(),
            append: async () => true,
            cancel: async () => {},
          }),
        },
      },
    )
    expect(handle).toBeDefined()
    await handle?.stop()
  })

  test('gives up when the first audio never arrives', async () => {
    vi.useFakeTimers()
    const pending = startCompanionTalk(
      {
        sessionId,
        onAudio: () => {},
        onUserTranscript: () => {},
        onAssistantTranscript: () => {},
        onBarge: () => {},
        onToolHandoff: () => {},
        onError: () => {},
        onEnded: () => {},
      },
      {
        listProviders: async () => ({ items: [realtime] }),
        firstAudioMs: 80,
        capture: async () => ({
          stop: async () => {},
          setMuted: () => {},
          contextSampleRate: () => 16000,
          flush: () => {},
          attachExtraStream: () => {},
        }),
        talk: {
          start: async () => ({
            talkId: 'talk-1',
            streamId: sessionId,
            sessionId,
            done: Promise.resolve(),
            append: async () => true,
            cancel: async () => {},
          }),
        },
      },
    )
    const handle = await vi.advanceTimersByTimeAsync(120).then(() => pending)
    expect(handle).toBeUndefined()
    vi.useRealTimers()
  })
})
