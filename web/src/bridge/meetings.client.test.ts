import { expect, it, vi } from 'vitest'
import {
  capBridgeDeadlineMs,
  createMeetingsBridge,
  MEETING_APPEND_DEADLINE_MS,
  MEETING_SUMMARIZE_DEADLINE_MS,
  type WebViewTransport,
} from './client'

const U = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const segment = { segmentId: U, meetingId: U, seq: 1, startedMs: 0, text: '先对齐范围', createdAt: '2026-08-28T00:00:00.000Z' }

function meetingsHarness(reply?: (request: { id: string; method: string; deadlineMs: number }) => unknown) {
  let listener: (e: MessageEvent) => void = () => {}
  const sent: Array<{ id: string; method: string; deadlineMs: number }> = []
  const transport: WebViewTransport = {
    addEventListener: (_t, l) => { listener = l as (e: MessageEvent) => void },
    removeEventListener: vi.fn(),
    postMessage: m => {
      const request = m as { id: string; method: string; deadlineMs: number }
      sent.push(request)
      const payload = reply?.(request)
      if (payload === undefined) return
      queueMicrotask(() => listener(new MessageEvent('message', {
        data: { v: '1.0', kind: 'response', id: U, requestId: request.id, ok: true, payload },
      })))
    },
  }
  return { sent, listener: () => listener, transport, bridge: createMeetingsBridge(transport) }
}

it('caps ordinary methods at 30s and meeting append/summarize far above a 60s mock', () => {
  expect(capBridgeDeadlineMs('system.health', 120_000)).toBe(30_000)
  expect(capBridgeDeadlineMs('meetings.append', 120_000)).toBe(MEETING_APPEND_DEADLINE_MS)
  expect(capBridgeDeadlineMs('meetings.append', 1)).toBe(1)
  expect(MEETING_APPEND_DEADLINE_MS).toBeGreaterThan(60_000)
  expect(capBridgeDeadlineMs('meetings.summarize', 600_000)).toBe(MEETING_SUMMARIZE_DEADLINE_MS)
})

it('meetings.append does not fail at a 60s mock delay', async () => {
  vi.useFakeTimers()
  try {
    const { sent, listener, bridge } = meetingsHarness()
    const pending = bridge.append({ meetingId: U, text: '先对齐范围' })
    expect(sent[0]?.deadlineMs).toBe(MEETING_APPEND_DEADLINE_MS)
    await vi.advanceTimersByTimeAsync(60_000)
    listener()(new MessageEvent('message', {
      data: { v: '1.0', kind: 'response', id: U, requestId: sent[0]!.id, ok: true, payload: segment },
    }))
    await expect(pending).resolves.toEqual(segment)
  } finally {
    vi.useRealTimers()
  }
})

it('retries meetings.append after a retryable timeout', async () => {
  vi.useFakeTimers()
  try {
    let listener: (e: MessageEvent) => void = () => {}
    const sent: Array<{ id: string }> = []
    const transport: WebViewTransport = {
      addEventListener: (_t, l) => { listener = l as (e: MessageEvent) => void },
      removeEventListener: vi.fn(),
      postMessage: m => {
        const request = m as { id: string }
        sent.push(request)
        queueMicrotask(() => {
          if (sent.length === 1) {
            listener(new MessageEvent('message', {
              data: {
                v: '1.0', kind: 'response', id: U, requestId: request.id, ok: false,
                error: { code: 'REQUEST_DEADLINE_EXCEEDED', message: 'Bridge 请求超时', retryable: true, correlationId: 't' },
              },
            }))
            return
          }
          listener(new MessageEvent('message', {
            data: { v: '1.0', kind: 'response', id: U, requestId: request.id, ok: true, payload: segment },
          }))
        })
      },
    }
    const pending = createMeetingsBridge(transport).append({ meetingId: U, text: '先对齐范围' })
    await vi.advanceTimersByTimeAsync(400)
    await expect(pending).resolves.toEqual(segment)
    expect(sent).toHaveLength(2)
  } finally {
    vi.useRealTimers()
  }
})

it('gives meetings.summarize a 10-minute deadline so hour-scale notes can finish', async () => {
  const ready = { ...segment, meetingId: U, title: '长会', status: 'ready', summary: '背景：测试。', actions: '- 跟进', transcript: '逐字稿', audioSource: 'microphone', startedAt: segment.createdAt, endedAt: segment.createdAt, durationMs: 3_600_000, createdAt: segment.createdAt, updatedAt: segment.createdAt }
  const { sent, bridge } = meetingsHarness(() => ready)
  await bridge.summarize({ meetingId: U })
  expect(sent[0]?.deadlineMs).toBe(MEETING_SUMMARIZE_DEADLINE_MS)
})

it('gives meetings.catchup the same hour-scale deadline as summarize', async () => {
  const ready = { ...segment, meetingId: U, title: '长会', status: 'transcribed', summary: '', actions: '', transcript: '补转写', audioSource: 'microphone', startedAt: segment.createdAt, endedAt: segment.createdAt, durationMs: 3_600_000, createdAt: segment.createdAt, updatedAt: segment.createdAt }
  const { sent, bridge } = meetingsHarness(() => ready)
  await bridge.catchup({ meetingId: U })
  expect(sent[0]?.method).toBe('meetings.catchup')
  expect(sent[0]?.deadlineMs).toBe(MEETING_SUMMARIZE_DEADLINE_MS)
})

it('retries meetings.audio.append after a retryable timeout', async () => {
  vi.useFakeTimers()
  try {
    let listener: (e: MessageEvent) => void = () => {}
    const sent: Array<{ id: string }> = []
    const transport: WebViewTransport = {
      addEventListener: (_t, l) => { listener = l as (e: MessageEvent) => void },
      removeEventListener: vi.fn(),
      postMessage: m => {
        const request = m as { id: string }
        sent.push(request)
        queueMicrotask(() => {
          if (sent.length === 1) {
            listener(new MessageEvent('message', {
              data: {
                v: '1.0', kind: 'response', id: U, requestId: request.id, ok: false,
                error: { code: 'REQUEST_DEADLINE_EXCEEDED', message: 'Bridge 请求超时', retryable: true, correlationId: 't' },
              },
            }))
            return
          }
          listener(new MessageEvent('message', {
            data: { v: '1.0', kind: 'response', id: U, requestId: request.id, ok: true, payload: { meetingId: U, audioMs: 1200 } },
          }))
        })
      },
    }
    const pending = createMeetingsBridge(transport).audioAppend({ meetingId: U, pcm: 'AAAA' })
    await vi.advanceTimersByTimeAsync(400)
    await expect(pending).resolves.toEqual({ meetingId: U, audioMs: 1200 })
    expect(sent).toHaveLength(2)
  } finally {
    vi.useRealTimers()
  }
})

it('retries meetings.stop after a retryable timeout', async () => {
  vi.useFakeTimers()
  try {
    let listener: (e: MessageEvent) => void = () => {}
    const sent: Array<{ id: string }> = []
    const ready = { ...segment, meetingId: U, title: '长会', status: 'transcribed', summary: '', actions: '', transcript: '逐字稿', audioSource: 'microphone', startedAt: segment.createdAt, endedAt: segment.createdAt, durationMs: 3_600_000, createdAt: segment.createdAt, updatedAt: segment.createdAt }
    const transport: WebViewTransport = {
      addEventListener: (_t, l) => { listener = l as (e: MessageEvent) => void },
      removeEventListener: vi.fn(),
      postMessage: m => {
        const request = m as { id: string }
        sent.push(request)
        queueMicrotask(() => {
          if (sent.length === 1) {
            listener(new MessageEvent('message', {
              data: {
                v: '1.0', kind: 'response', id: U, requestId: request.id, ok: false,
                error: { code: 'REQUEST_DEADLINE_EXCEEDED', message: 'Bridge 请求超时', retryable: true, correlationId: 't' },
              },
            }))
            return
          }
          listener(new MessageEvent('message', {
            data: { v: '1.0', kind: 'response', id: U, requestId: request.id, ok: true, payload: ready },
          }))
        })
      },
    }
    const pending = createMeetingsBridge(transport).stop({ meetingId: U })
    await vi.advanceTimersByTimeAsync(400)
    await expect(pending).resolves.toMatchObject({ status: 'transcribed' })
    expect(sent).toHaveLength(2)
  } finally {
    vi.useRealTimers()
  }
})

it('sends meetings.loopback.poll', async () => {
  const { sent, bridge } = meetingsHarness(() => ({ meetingId: U, active: false, pcm: '' }))
  await expect(bridge.loopbackPoll({ meetingId: U })).resolves.toEqual({ meetingId: U, active: false, pcm: '' })
  expect(sent[0]?.method).toBe('meetings.loopback.poll')
})
