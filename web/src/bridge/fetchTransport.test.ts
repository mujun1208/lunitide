import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FetchTransport, activateRemoteTransport, deactivateRemoteTransport, getRemoteTransport } from './fetchTransport'
import { resolveHostTransport } from './client'

const encoder = new TextEncoder()
const ULID = '01ARZ0N1K4J32Z4B7Q4T3R9W1M'

function ndjsonResponse(frames: unknown[], status = 200): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const frame of frames) controller.enqueue(encoder.encode(JSON.stringify(frame) + '\n'))
      controller.close()
    },
  })
  return new Response(body, { status, headers: { 'Content-Type': 'application/x-ndjson' } })
}

const responseFrame = (requestId: string, payload: Record<string, unknown> = {}) => ({
  v: '1.0', kind: 'response', id: ULID, requestId, ok: true, payload,
})
const eventFrameOf = (streamId: string, sequence: number, type: string) => ({
  v: '1.0', kind: 'event', id: ULID, streamId, sequence, type,
})

describe('FetchTransport', () => {
  let fetchImpl: ReturnType<typeof vi.fn>
  let calls: Array<{ url: string; init: RequestInit }>

  beforeEach(() => {
    calls = []
    fetchImpl = vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, init })
      return ndjsonResponse([responseFrame((JSON.parse(String(init.body)) as { id: string }).id)])
    })
    vi.stubGlobal('fetch', fetchImpl as unknown as typeof fetch)
  })
  afterEach(() => {
    deactivateRemoteTransport()
    vi.unstubAllGlobals()
  })

  it('derives the /bridge/http endpoint, sends the Bearer token and dispatches response frames', async () => {
    const transport = new FetchTransport('wss://192.0.2.10:47651/bridge', 'tok123')
    expect(transport.currentState).toBe('connecting')
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'project.list', payload: {} })
    await vi.waitFor(() => expect(seen).toHaveLength(1))
    expect(calls[0].url).toBe('https://192.0.2.10:47651/bridge/http')
    expect((calls[0].init.headers as Record<string, string>).Authorization).toBe('Bearer tok123')
    expect((seen[0] as Record<string, unknown>).requestId).toBe('req1')
    expect(transport.currentState).toBe('open')
    transport.dispose()
  })

  it('keeps stream events flowing on one fetch and aborts after the terminal frame', async () => {
    const transport = new FetchTransport('wss://host/bridge', 't')
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    fetchImpl.mockImplementationOnce(async () => ndjsonResponse([
      responseFrame('req1', { streamId: ULID }),
      eventFrameOf(ULID, 1, 'delta'),
      eventFrameOf(ULID, 2, 'completed'),
    ]))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'chat.start', payload: {} })
    await vi.waitFor(() => expect(seen).toHaveLength(3))
    expect((seen.at(-1) as Record<string, unknown>).type).toBe('completed')
    transport.dispose()
  })

  it('synthesizes a failed event when the stream closes without a terminal frame', async () => {
    const transport = new FetchTransport('wss://host/bridge', 't')
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    fetchImpl.mockImplementationOnce(async () => ndjsonResponse([
      responseFrame('req1', { streamId: ULID }),
      eventFrameOf(ULID, 1, 'delta'),
    ]))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'chat.start', payload: {} })
    await vi.waitFor(() => expect(seen).toHaveLength(3))
    const synthetic = seen.at(-1) as Record<string, unknown>
    expect(synthetic.type).toBe('failed')
    expect(synthetic.sequence).toBe(2)
    expect((synthetic.error as Record<string, unknown>).code).toBe('REMOTE_STREAM_DISCONNECTED')
    transport.dispose()
  })

  it('rotates candidates when the network layer fails and recovers on the next address', async () => {
    const transport = new FetchTransport('wss://192.0.2.10:47651/bridge', 't', ['2409:8900::1'])
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    fetchImpl.mockRejectedValueOnce(new TypeError('network down'))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'project.list', payload: {} })
    await vi.waitFor(() => expect(seen).toHaveLength(1))
    // 第一次调用（v4，被 mockRejectedValueOnce 拒绝）→ 轮换重发到 IPv6 候选。
    expect(String(fetchImpl.mock.calls[0][0])).toContain('192.0.2.10')
    expect(calls[0].url).toContain('https://[2409:8900::1]:47651/bridge/http')
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(transport.currentState).toBe('open')
    transport.dispose()
  })

  it('aborts a black-holed candidate after the deadline and rotates to the next address', async () => {
    vi.useFakeTimers()
    const transport = new FetchTransport('wss://192.0.2.10:47651/bridge', 't', ['2409:8900::1'])
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    // 黑洞候选：fetch 永不落定（防火墙静默丢包），只有响应头超时 abort
    // 才能触发轮换——不设此守卫，离家后旧 Wi-Fi 地址永远卡死传输。
    fetchImpl.mockImplementationOnce((_url: string, init: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
      }))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'project.list', payload: {}, deadlineMs: 8_000 })
    // deadline 8s + 2s 余量 = 10s 响应头超时 → abort → 轮换重发到 IPv6。
    await vi.advanceTimersByTimeAsync(10_000)
    await vi.waitFor(() => expect(seen).toHaveLength(1))
    expect(String(fetchImpl.mock.calls[0][0])).toContain('192.0.2.10')
    expect(String(fetchImpl.mock.calls[1][0])).toContain('2409:8900::1')
    expect(transport.currentState).toBe('open')
    vi.useRealTimers()
    transport.dispose()
  })

  it('synthesizes BRIDGE_UNAVAILABLE when every candidate is unreachable', async () => {
    const transport = new FetchTransport('wss://a/bridge', 't', ['b'])
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    fetchImpl.mockRejectedValue(new TypeError('network down'))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'project.list', payload: {} })
    await vi.waitFor(() => expect(seen).toHaveLength(1))
    const frame = seen[0] as Record<string, unknown>
    expect(frame.ok).toBe(false)
    expect((frame.error as Record<string, unknown>).code).toBe('BRIDGE_UNAVAILABLE')
    expect(transport.currentState).toBe('reconnecting')
    transport.dispose()
  })

  it('maps gateway 401 to REMOTE_UNAUTHORIZED without rotating candidates', async () => {
    const transport = new FetchTransport('wss://a/bridge', 't', ['b'])
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    fetchImpl.mockResolvedValue(new Response(null, { status: 401 }))
    transport.postMessage({ v: '1.0', kind: 'request', id: 'req1', method: 'project.list', payload: {} })
    await vi.waitFor(() => expect(seen).toHaveLength(1))
    const frame = seen[0] as Record<string, unknown>
    expect((frame.error as Record<string, unknown>).code).toBe('REMOTE_UNAUTHORIZED')
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    transport.dispose()
  })

  it('rejects postMessage after dispose and reports closed', () => {
    const transport = new FetchTransport('wss://host/bridge', 't')
    const states: string[] = []
    transport.onStateChange(state => states.push(state))
    transport.dispose()
    expect(() => transport.postMessage({})).toThrow()
    expect(transport.currentState).toBe('closed')
    expect(states).toEqual(['connecting', 'closed'])
  })

  it('activates the transport override for every bridge singleton', () => {
    const transport = activateRemoteTransport('wss://host/bridge', 'tok')
    expect(getRemoteTransport()).toBe(transport)
    expect(resolveHostTransport()).toBe(transport)
    deactivateRemoteTransport()
    expect(getRemoteTransport()).toBeUndefined()
    expect(resolveHostTransport()).toBeUndefined()
  })
})
