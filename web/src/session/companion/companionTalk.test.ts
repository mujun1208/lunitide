import { describe, expect, test, vi } from 'vitest'
import { BridgeClientError, type TalkStreamEvent, type TalkStreamHandle } from '../../bridge/client'
import type { ProviderDTO } from '../../generated/bridge'
import {
  companionCascadeSpeechBlocked,
  isCompanionIdleChat,
  MAX_QUEUED_FRAMES,
  newTalkRetryState,
  noteTalkFailure,
  shouldOfferCompanionTalk,
  startCompanionTalk,
  TALK_FALLBACK_BANNER,
  TALK_MAX_FAILURES,
  TALK_RETRY_COOLDOWN_MS,
  talkRetryBlocked,
} from './companionTalk'

describe('talk retry backoff', () => {
  test('a fresh state offers talk', () => {
    expect(talkRetryBlocked(newTalkRetryState(), 1_000)).toBe(false)
  })

  test('a single failure backs off during the cooldown, then re-offers', () => {
    const after = noteTalkFailure(newTalkRetryState(), 1_000)
    expect(talkRetryBlocked(after, 1_000 + TALK_RETRY_COOLDOWN_MS - 1)).toBe(true)
    expect(talkRetryBlocked(after, 1_000 + TALK_RETRY_COOLDOWN_MS + 1)).toBe(false)
  })

  test('a run of failures latches for the session', () => {
    let state = newTalkRetryState()
    for (let i = 0; i < TALK_MAX_FAILURES; i += 1) state = noteTalkFailure(state, i * 1_000_000)
    expect(state.failures).toBe(TALK_MAX_FAILURES)
    // Even far past the cooldown, a latched state stays blocked.
    expect(talkRetryBlocked(state, 9_999_999_999)).toBe(true)
  })
})

const sessionId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const realtime: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'Chat',
  protocol: 'openai_compatible',
  baseUrl: 'https://example.com',
  status: 'enabled',
  credentialState: 'configured',
  credentialBackupCount: 0,
  createdAt: '',
  updatedAt: '',
  version: 1,
  models: [{ modelId: 'gpt-4o-realtime-preview', displayName: 'Realtime', isDefault: true, kind: 'llm' }],
}

const airOnly: ProviderDTO = {
  ...realtime,
  models: [{ modelId: 'glm-4-air', displayName: 'Air', isDefault: true, kind: 'llm' }],
}

test.each(['ended', 'error'] as const)('releases realtime capture exactly once when the server sends %s', async type => {
  let event!: (event: TalkStreamEvent) => void
  let frame!: (frame: { base64: string; samples: Int16Array; peak: number }) => void
  const stop = vi.fn().mockResolvedValue(undefined)
  const cancel = vi.fn().mockResolvedValue(undefined)
  const append = vi.fn().mockResolvedValue(true)
  const onEnded = vi.fn()
  const handle = await startCompanionTalk({
    sessionId, onAudio: vi.fn(), onUserTranscript: vi.fn(), onAssistantTranscript: vi.fn(),
    onBarge: vi.fn(), onToolHandoff: vi.fn(), onError: vi.fn(), onEnded,
  }, {
    listProviders: async () => ({ items: [realtime] }),
    capture: async options => {
      frame = options.onFrame
      return { stop, setMuted: vi.fn(), contextSampleRate: () => 16000, flush: vi.fn(), attachExtraStream: vi.fn() }
    },
    talk: { start: async (_payload, callback) => {
      event = callback
      return { talkId: 'audit', streamId: 'audit', sessionId, done: Promise.resolve(), append, cancel }
    } },
  })
  event(type === 'ended' ? { type } : { type, code: 'TALK_SESSION_FAILED', message: 'remote failure' })
  event({ type: 'ended' })
  expect(stop).toHaveBeenCalledTimes(1)
  frame({ base64: 'AAAA', samples: new Int16Array(1600), peak: 0 })
  await vi.waitFor(() => expect(onEnded).toHaveBeenCalledTimes(1))
  expect(append).not.toHaveBeenCalled()
  expect(cancel).toHaveBeenCalledTimes(1)
  await handle?.stop()
  expect(stop).toHaveBeenCalledTimes(1)
})

describe('companionTalk helpers', () => {
  test('offers talk only on volc with a listed model, a session, and the opt-in on', () => {
    // Talk-realtime is opt-in: default (no opt-in) always stays on cascade.
    expect(shouldOfferCompanionTalk('volc', true, sessionId)).toBe(false)
    expect(shouldOfferCompanionTalk('volc', true, sessionId, false)).toBe(false)
    expect(shouldOfferCompanionTalk('volc', true, sessionId, true)).toBe(true)
    expect(shouldOfferCompanionTalk('cloud', true, sessionId, true)).toBe(false)
    expect(shouldOfferCompanionTalk('local', true, sessionId, true)).toBe(false)
    expect(shouldOfferCompanionTalk('volc', false, sessionId, true)).toBe(false)
    expect(shouldOfferCompanionTalk('volc', true, '', true)).toBe(false)
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
    const messageIds: Array<string | undefined> = []
    const handle = await startCompanionTalk(
      {
        sessionId,
        onAudio: () => {},
        onUserTranscript: () => {},
        onAssistantTranscript: () => {},
        onBarge: () => {},
        onToolHandoff: (text, messageId) => { handed.push(text); messageIds.push(messageId) },
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
    events[0]?.({ type: 'tool', name: 'handoff', text: '打开网页', messageId: sessionId })
    expect(handed).toEqual(['打开网页'])
    expect(messageIds).toEqual([sessionId])
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

describe('startCompanionTalk send backpressure', () => {
  type FrameCb = (frame: { base64: string; samples: Int16Array; peak: number }) => void
  const frame = (base64: string) => ({ base64, samples: new Int16Array(1600), peak: 0 })

  const baseCallbacks = () => ({
    sessionId,
    onAudio: () => {},
    onUserTranscript: () => {},
    onAssistantTranscript: () => {},
    onBarge: () => {},
    onToolHandoff: () => {},
    onError: () => {},
    onEnded: () => {},
  })

  test('caps the queue at MAX_QUEUED_FRAMES, drops the oldest, and warns', async () => {
    let emit!: FrameCb
    const appended: string[] = []
    let releaseFirst!: () => void
    const firstAppend = new Promise<void>(resolve => {
      releaseFirst = resolve
    })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const handle = await startCompanionTalk(baseCallbacks(), {
      listProviders: async () => ({ items: [realtime] }),
      capture: async opts => {
        emit = opts.onFrame
        return {
          stop: async () => {},
          setMuted: () => {},
          contextSampleRate: () => 16000,
          flush: () => {},
          attachExtraStream: () => {},
        }
      },
      talk: {
        start: async () => ({
          talkId: 'talk-1',
          streamId: sessionId,
          sessionId,
          done: Promise.resolve(),
          // First append never resolves until released: this pins one frame in
          // flight so every later frame stacks up in the bounded send queue.
          append: async (pcm: string) => {
            appended.push(pcm)
            await firstAppend
            return true
          },
          cancel: async () => {},
        }),
      },
    })
    expect(handle).toBeDefined()

    // 200 frames captured while the single in-flight append is stalled.
    const total = 200
    for (let i = 0; i < total; i += 1) emit(frame(`f${i}`))
    await Promise.resolve()

    // Exactly one frame reached append (in flight); the queue holds at most 64.
    expect(appended).toEqual(['f0'])
    // The overflow warning fired at least once and reports a running total.
    expect(warn).toHaveBeenCalled()
    expect(warn.mock.calls.at(-1)?.[0]).toMatch(/^\[talk\] send queue overflow, dropped \d+ frames$/)

    // Release the stall and let the queue drain; the newest frames survive,
    // the oldest (beyond f0 + the 64 kept) were dropped.
    releaseFirst()
    for (let guard = 0; guard < total + 10; guard += 1) await Promise.resolve()

    // f0 was sent first; the tail kept is the most recent MAX_QUEUED_FRAMES.
    expect(appended[0]).toBe('f0')
    expect(appended.at(-1)).toBe(`f${total - 1}`)
    // Total appended = 1 (in flight) + the last MAX_QUEUED_FRAMES retained.
    expect(appended.length).toBe(1 + MAX_QUEUED_FRAMES)
    // The gap proves drop-oldest: f1..f(total-64-1) were discarded.
    expect(appended).not.toContain('f1')

    warn.mockRestore()
    await handle?.stop()
  })

  test('sends frame by frame with no drops when appends keep up', async () => {
    let emit!: FrameCb
    const appended: string[] = []
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const handle = await startCompanionTalk(baseCallbacks(), {
      listProviders: async () => ({ items: [realtime] }),
      capture: async opts => {
        emit = opts.onFrame
        return {
          stop: async () => {},
          setMuted: () => {},
          contextSampleRate: () => 16000,
          flush: () => {},
          attachExtraStream: () => {},
        }
      },
      talk: {
        start: async () => ({
          talkId: 'talk-1',
          streamId: sessionId,
          sessionId,
          done: Promise.resolve(),
          append: async (pcm: string) => {
            appended.push(pcm)
            return true
          },
          cancel: async () => {},
        }),
      },
    })
    expect(handle).toBeDefined()

    for (let i = 0; i < 10; i += 1) {
      emit(frame(`f${i}`))
      // Let the in-flight append settle before the next frame arrives.
      await Promise.resolve()
      await Promise.resolve()
    }

    expect(appended).toEqual(Array.from({ length: 10 }, (_, i) => `f${i}`))
    expect(warn).not.toHaveBeenCalled()
    warn.mockRestore()
    await handle?.stop()
  })
})

describe('startCompanionTalk capture-first ordering', () => {
  type FrameCb = (frame: { base64: string; samples: Int16Array; peak: number }) => void
  const frame = (base64: string) => ({ base64, samples: new Int16Array(1600), peak: 0 })

  const baseCallbacks = () => ({
    sessionId,
    onAudio: () => {},
    onUserTranscript: () => {},
    onAssistantTranscript: () => {},
    onBarge: () => {},
    onToolHandoff: () => {},
    onError: () => {},
    onEnded: () => {},
  })

  // 回归（开头吞字）：capture 必须先于 talk 连接启动；连接握手期间麦克风
  // 已在收音，期间到达的帧只能排队，连接就绪后按序冲洗——否则用户开口
  // 的前 1-2 个字被静音丢弃（"今天上海的天气怎么样"→"的天气怎么样"）。
  test('captures before connecting, queues frames during the handshake, and flushes them in order once live', async () => {
    const order: string[] = []
    let emit!: FrameCb
    const appended: string[] = []
    let goLive!: (stream: TalkStreamHandle) => void
    const live = new Promise<TalkStreamHandle>(resolve => {
      goLive = resolve
    })
    const pending = startCompanionTalk(baseCallbacks(), {
      listProviders: async () => ({ items: [realtime] }),
      capture: async opts => {
        order.push('capture')
        emit = opts.onFrame
        return {
          stop: async () => {},
          setMuted: () => {},
          contextSampleRate: () => 16000,
          flush: () => {},
          attachExtraStream: () => {},
        }
      },
      talk: {
        start: async () => {
          order.push('talk')
          // 连接挂起：模拟 WebSocket 握手期间麦克风已经在收音。
          return await live
        },
      },
    })
    for (let guard = 0; guard < 100 && order.length < 2; guard += 1) await Promise.resolve()
    // 麦克风先启动，连接后启动。
    expect(order).toEqual(['capture', 'talk'])

    // 连接尚未就绪时说出的开头帧：排队等待，不允许发送。
    emit(frame('f0'))
    emit(frame('f1'))
    emit(frame('f2'))
    for (let guard = 0; guard < 10; guard += 1) await Promise.resolve()
    expect(appended).toEqual([])

    // 连接建立：排队帧全部按序冲洗，开头不丢字。
    goLive({
      talkId: 'talk-1',
      streamId: sessionId,
      sessionId,
      done: Promise.resolve(),
      append: async (pcm: string) => {
        appended.push(pcm)
        return true
      },
      cancel: async () => {},
    })
    const handle = await pending
    expect(handle).toBeDefined()
    for (let guard = 0; guard < 10; guard += 1) await Promise.resolve()
    expect(appended).toEqual(['f0', 'f1', 'f2'])
    await handle?.stop()
  })
})
