import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WsTransport, activateRemoteTransport, deactivateRemoteTransport, clearRemoteCredentials, loadRemoteCredentials, saveRemoteCredentials, getRemoteTransport } from './wsTransport'
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

  it('activates the transport override for every bridge singleton', () => {
    const transport = activateRemoteTransport('wss://host/bridge', 'tok')
    expect(getRemoteTransport()).toBe(transport)
    expect(resolveHostTransport()).toBe(transport)
    deactivateRemoteTransport()
    expect(getRemoteTransport()).toBeUndefined()
    expect(resolveHostTransport()).toBeUndefined()
  })
})
