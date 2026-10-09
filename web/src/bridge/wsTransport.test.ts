import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WsTransport, activateRemoteTransport, deactivateRemoteTransport, clearRemoteCredentials, loadRemoteCredentials, saveRemoteCredentials, getRemoteTransport, swapWsHost } from './wsTransport'
import { resolveHostTransport } from './client'

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  url: string
  readyState = FakeWebSocket.CONNECTING
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  sent: string[] = []
  constructor(url: string) { this.url = url; FakeWebSocket.instances.push(this) }
  send(frame: string): void { this.sent.push(frame) }
  close(): void {
    if (this.readyState === FakeWebSocket.CLOSED) return
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({} as Event)
  }
  // 测试辅助：模拟服务器侧行为。
  open(): void { this.readyState = FakeWebSocket.OPEN; this.onopen?.({} as Event) }
  deliver(value: unknown): void { this.onmessage?.({ data: JSON.stringify(value) }) }
  drop(): void {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({} as Event)
  }
}

describe('WsTransport', () => {
  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
    localStorage.clear()
  })
  afterEach(() => {
    deactivateRemoteTransport()
    vi.unstubAllGlobals()
    vi.useRealTimers()
    localStorage.clear()
  })

  it('connects with the token query parameter and round-trips frames', () => {
    const transport = new WsTransport('wss://192.0.2.10:47651/bridge', 'tok123')
    const socket = FakeWebSocket.instances[0]
    expect(socket.url).toContain('wss://192.0.2.10:47651/bridge')
    expect(socket.url).toContain('token=tok123')
    socket.open()
    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))
    transport.postMessage({ hello: 'world' })
    expect(socket.sent).toEqual(['{"hello":"world"}'])
    socket.deliver({ ok: true, requestId: 'r1' })
    expect(seen).toEqual([{ ok: true, requestId: 'r1' }])
    transport.removeEventListener('message', () => {})
    transport.dispose()
  })

  it('queues frames while connecting and flushes in order on open', () => {
    const transport = new WsTransport('wss://host/bridge', 't')
    const socket = FakeWebSocket.instances[0]
    transport.postMessage({ seq: 1 })
    transport.postMessage({ seq: 2 })
    expect(socket.sent).toEqual([])
    socket.open()
    expect(socket.sent).toEqual(['{"seq":1}', '{"seq":2}'])
    transport.dispose()
  })

  it('rejects postMessage once disposed or when no connection is pending', () => {
    const transport = new WsTransport('wss://host/bridge', 't')
    transport.dispose()
    expect(() => transport.postMessage({})).toThrow()
    const second = new WsTransport('wss://host/bridge', 't')
    const socket = FakeWebSocket.instances.at(-1)!
    socket.drop()
    expect(() => second.postMessage({})).toThrow(/unavailable|queue full/)
    second.dispose()
  })

  it('reconnects with exponential backoff after a drop', () => {
    vi.useFakeTimers()
    const transport = new WsTransport('wss://host/bridge', 't')
    const first = FakeWebSocket.instances[0]
    first.open()
    expect(transport.currentState).toBe('open')
    first.drop()
    expect(transport.currentState).toBe('reconnecting')
    expect(FakeWebSocket.instances).toHaveLength(1)
    vi.advanceTimersByTime(1_000)
    expect(FakeWebSocket.instances).toHaveLength(2)
    FakeWebSocket.instances[1].open()
    expect(transport.currentState).toBe('open')
    transport.dispose()
  })

  it('reports state transitions to listeners', () => {
    vi.useFakeTimers()
    const states: string[] = []
    const transport = new WsTransport('wss://host/bridge', 't')
    const off = transport.onStateChange(state => states.push(state))
    FakeWebSocket.instances[0].open()
    FakeWebSocket.instances[0].drop()
    vi.advanceTimersByTime(1_000)
    FakeWebSocket.instances[1].open()
    off()
    FakeWebSocket.instances[1].drop()
    vi.advanceTimersByTime(60_000)
    expect(states).toEqual(['open', 'reconnecting', 'open'])
    vi.useRealTimers()
    transport.dispose()
  })

  it('rotates through candidate ws urls (v4 → IPv6 → tailnet) on each reconnect', () => {
    vi.useFakeTimers()
    // 候选含与当前 host 相同的地址（配对结果 addresses 含首选地址）：必须去重。
    const transport = new WsTransport('wss://192.0.2.10:47651/bridge', 't', ['2409:8900::1', '100.95.14.34', '192.0.2.10'])
    expect(FakeWebSocket.instances[0].url).toContain('wss://192.0.2.10:47651/bridge')
    FakeWebSocket.instances[0].open()
    FakeWebSocket.instances[0].drop()
    vi.advanceTimersByTime(1_000)
    expect(FakeWebSocket.instances[1].url).toContain('wss://[2409:8900::1]:47651/bridge')
    FakeWebSocket.instances[1].open()
    FakeWebSocket.instances[1].drop()
    vi.advanceTimersByTime(2_000)
    expect(FakeWebSocket.instances[2].url).toContain('wss://100.95.14.34:47651/bridge')
    FakeWebSocket.instances[2].drop()
    vi.advanceTimersByTime(4_000)
    // 轮换回到首选地址（去重后共 3 个候选）。
    expect(FakeWebSocket.instances[3].url).toContain('wss://192.0.2.10:47651/bridge')
    transport.dispose()
  })

  it('keeps round-robin cycling across repeated drops and reopens', () => {
    vi.useFakeTimers()
    const transport = new WsTransport('wss://192.0.2.10:47651/bridge', 't', ['2409:8900::1'])
    FakeWebSocket.instances[0].open()
    FakeWebSocket.instances[0].drop()
    vi.advanceTimersByTime(1_000)
    expect(FakeWebSocket.instances[1].url).toContain('wss://[2409:8900::1]:47651/bridge')
    FakeWebSocket.instances[1].open()
    // IPv6 瞬断（如服务器重启）：round-robin 轮回首选 v4；v4 在蜂窝下失败
    // 后会再轮回 IPv6——循环保证每个地址都被周期性重试。
    FakeWebSocket.instances[1].drop()
    vi.advanceTimersByTime(2_000)
    expect(FakeWebSocket.instances[2].url).toContain('wss://192.0.2.10:47651/bridge')
    transport.dispose()
  })
})

describe('remote credentials and activation', () => {
  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
    localStorage.clear()
  })
  afterEach(() => {
    deactivateRemoteTransport()
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('round-trips credentials through localStorage and rejects garbage', () => {
    expect(loadRemoteCredentials()).toBeUndefined()
    saveRemoteCredentials({ wsUrl: 'wss://host/bridge', token: 'tok' })
    expect(loadRemoteCredentials()).toEqual({ wsUrl: 'wss://host/bridge', token: 'tok' })
    clearRemoteCredentials()
    expect(loadRemoteCredentials()).toBeUndefined()
    localStorage.setItem('lunitide:remote-bridge', 'not-json')
    expect(loadRemoteCredentials()).toBeUndefined()
    localStorage.setItem('lunitide:remote-bridge', JSON.stringify({ wsUrl: 1, token: null }))
    expect(loadRemoteCredentials()).toBeUndefined()
  })

  it('persists candidate addresses with credentials and sanitizes them on load', () => {
    // 正常往返：候选裸地址原样保存（语义与壳保存的候选、深链接 addresses 一致）。
    saveRemoteCredentials({ wsUrl: 'wss://192.0.2.10:47651/bridge', token: 'tok', candidates: ['2409:8900::1', '100.95.14.34'] })
    expect(loadRemoteCredentials()).toEqual({ wsUrl: 'wss://192.0.2.10:47651/bridge', token: 'tok', candidates: ['2409:8900::1', '100.95.14.34'] })
    clearRemoteCredentials()
    // 脏数据防御：非字符串/空串候选剔除，全无效则退化为单地址凭据。
    localStorage.setItem('lunitide:remote-bridge', JSON.stringify({ wsUrl: 'wss://host/bridge', token: 'tok', candidates: [42, '', '2409:8900::1'] }))
    expect(loadRemoteCredentials()).toEqual({ wsUrl: 'wss://host/bridge', token: 'tok', candidates: ['2409:8900::1'] })
    localStorage.setItem('lunitide:remote-bridge', JSON.stringify({ wsUrl: 'wss://host/bridge', token: 'tok', candidates: 'not-an-array' }))
    expect(loadRemoteCredentials()).toEqual({ wsUrl: 'wss://host/bridge', token: 'tok' })
    clearRemoteCredentials()
  })

  it('swaps the ws host for bare gateway addresses (IPv6 gets brackets)', () => {
    expect(swapWsHost('wss://192.0.2.10:47651/bridge', '2409:8900:1::a')).toBe('wss://[2409:8900:1::a]:47651/bridge')
    expect(swapWsHost('wss://192.0.2.10:47651/bridge', '100.95.14.34')).toBe('wss://100.95.14.34:47651/bridge')
    // 与当前 host 相同的候选：返回原串（构造器据此去重）。
    expect(swapWsHost('wss://192.0.2.10:47651/bridge', '192.0.2.10')).toBe('wss://192.0.2.10:47651/bridge')
    // 非法 wsUrl：原样返回，连接时自然失败并轮换下一个。
    expect(swapWsHost('::not-a-url', '1.2.3.4')).toBe('::not-a-url')
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
