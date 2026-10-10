import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { probeViaTransport, probeWithCredentials, GATE_TIMEOUT_MS } from './gate'
import type { WebViewTransport } from './client'
import type { BridgeResponse } from '../generated/bridge'

const encoder = new TextEncoder()
const ULID = '01ARZ0N1K4J32Z4B7Q4T3R9W1M'

// 可注入的假传输：记录发出的请求帧，测试侧手动投递响应帧。
class FakeTransport implements WebViewTransport {
  sent: Array<Record<string, unknown>> = []
  private listeners = new Set<(event: MessageEvent<BridgeResponse>) => void>()
  postMessage(value: unknown): void { this.sent.push(value as Record<string, unknown>) }
  addEventListener(_type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void { this.listeners.add(listener) }
  removeEventListener(_type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void { this.listeners.delete(listener) }
  deliver(frame: unknown): void {
    const event = new MessageEvent('message', { data: frame }) as MessageEvent<BridgeResponse>
    for (const listener of [...this.listeners]) listener(event)
  }
  byMethod(method: string): Record<string, unknown> {
    const frame = this.sent.find(item => item.method === method)
    if (!frame) throw new Error(`no probe frame for ${method}`)
    return frame
  }
}

function okFrame(requestId: unknown, payload: unknown): unknown {
  return { v: '1.0', kind: 'response', id: ULID, requestId, ok: true, payload }
}

function ndjsonResponse(frames: unknown[]): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const frame of frames) controller.enqueue(encoder.encode(JSON.stringify(frame) + '\n'))
      controller.close()
    },
  })
  return new Response(body, { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } })
}

describe('availability gate', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('sends health + provider.list probes and passes when both answer ok', async () => {
    const transport = new FakeTransport()
    const pending = probeViaTransport(transport)
    await vi.waitFor(() => expect(transport.sent).toHaveLength(2))
    const health = transport.byMethod('system.health')
    const providers = transport.byMethod('provider.list')
    // 探活请求契约：桥 v1.0、request、8s deadline；双查并行发出。
    for (const frame of [health, providers]) {
      expect(frame.v).toBe('1.0')
      expect(frame.kind).toBe('request')
      expect(frame.deadlineMs).toBe(8_000)
    }
    transport.deliver(okFrame(health.id, { engine: 'lunitide', version: '0.17.8', protocol: '1.0' }))
    transport.deliver(okFrame(providers.id, { items: [] }))
    await expect(pending).resolves.toEqual({ ok: true, engine: 'lunitide', version: '0.17.8' })
  })

  it('fails when provider.list errors even though health passed', async () => {
    const transport = new FakeTransport()
    const pending = probeViaTransport(transport)
    await vi.waitFor(() => expect(transport.sent).toHaveLength(2))
    transport.deliver(okFrame(transport.byMethod('system.health').id, { engine: 'lunitide', version: '0.17.8' }))
    transport.deliver({
      v: '1.0', kind: 'response', id: ULID, requestId: transport.byMethod('provider.list').id, ok: false,
      error: { code: 'REMOTE_SCOPE_DENIED', message: '模型列表读取被拒绝', retryable: false, correlationId: ULID },
    })
    await expect(pending).resolves.toEqual({ ok: false, error: '模型列表读取被拒绝' })
  })

  it('fails with the server error message on an error response frame', async () => {
    const transport = new FakeTransport()
    const pending = probeViaTransport(transport)
    await vi.waitFor(() => expect(transport.sent).toHaveLength(2))
    transport.deliver({
      v: '1.0', kind: 'response', id: ULID, requestId: transport.byMethod('system.health').id, ok: false,
      error: { code: 'REMOTE_UNAUTHORIZED', message: '设备授权已失效，请重新扫码配对', retryable: false, correlationId: ULID },
    })
    await expect(pending).resolves.toEqual({ ok: false, error: '设备授权已失效，请重新扫码配对' })
  })

  it('ignores frames for other requests and fails on overall timeout', async () => {
    vi.useFakeTimers()
    const transport = new FakeTransport()
    const pending = probeViaTransport(transport)
    await vi.waitFor(() => expect(transport.sent).toHaveLength(2))
    // 其它请求的响应帧不得误判为探活结果。
    transport.deliver(okFrame('someone-else', {}))
    vi.advanceTimersByTime(GATE_TIMEOUT_MS)
    const outcome = await pending
    expect(outcome.ok).toBe(false)
    if (!outcome.ok) expect(outcome.error).toContain('超时')
  })

  it('probes real credentials through FetchTransport against /bridge/http', async () => {
    const fetchImpl = vi.fn(async (_url: string, init: RequestInit) => {
      const request = JSON.parse(String(init.body)) as { id: string; method: string }
      const payload = request.method === 'system.health'
        ? { engine: 'lunitide', version: '0.17.8', protocol: '1.0' }
        : { items: [] }
      return ndjsonResponse([{ v: '1.0', kind: 'response', id: ULID, requestId: request.id, ok: true, payload }])
    })
    vi.stubGlobal('fetch', fetchImpl as unknown as typeof fetch)
    const outcome = await probeWithCredentials({ wsUrl: 'wss://192.0.2.10:47651/bridge', token: 'tok123' })
    expect(outcome).toEqual({ ok: true, engine: 'lunitide', version: '0.17.8' })
    // 双查各自独立一次 POST。
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(String(fetchImpl.mock.calls[0][0])).toBe('https://192.0.2.10:47651/bridge/http')
    const headers = fetchImpl.mock.calls[0][1].headers as Record<string, string>
    expect(headers.Authorization).toBe('Bearer tok123')
  })

  it('fails through to the synthetic 401 frame when the token is dead', async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 401 }))
    vi.stubGlobal('fetch', fetchImpl as unknown as typeof fetch)
    const outcome = await probeWithCredentials({ wsUrl: 'wss://a/bridge', token: 'dead' })
    expect(outcome.ok).toBe(false)
    if (!outcome.ok) expect(outcome.error).toContain('重新扫码配对')
  })

  it('fails with the upgrade-desktop message on 404 (old desktop without /bridge/http)', async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 404 }))
    vi.stubGlobal('fetch', fetchImpl as unknown as typeof fetch)
    const outcome = await probeWithCredentials({ wsUrl: 'wss://a/bridge', token: 'tok123' })
    expect(outcome.ok).toBe(false)
    if (!outcome.ok) {
      expect(outcome.error).toContain('电脑端 Lunitide 版本过旧')
      expect(outcome.error).toContain('升级')
    }
  })
})
