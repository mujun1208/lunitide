// 移动伴侣远程传输：把 WebViewTransport 语义（postMessage + message 事件）
// 承载到 WSS 网关连接上。桥接帧协议不变——WS 文本消息即一帧 JSON。
//
// 生命周期：
// - 构造即发起连接；connecting 期间的 postMessage 进入有界待发队列，
//   open 后按序 flush（上限 128 帧，超限丢弃并抛 BRIDGE_UNAVAILABLE 语义
//   错误，让上层按既有超时路径失败）。
// - 断线自动重连（指数退避 1s→30s 封顶，无限重试；visibilitychange 回前台
//   立即重试），移动网络切换后无需手动恢复。
// - 服务器 45s 应用层 ping / 浏览器自动 pong，无需前端心跳。
import type { BridgeResponse } from '../generated/bridge'
import { setTransportOverride, type WebViewTransport } from './client'

export interface RemoteCredentials {
  wsUrl: string
  token: string
  /** 网关候选裸地址（IPv4/IPv6，无 scheme/端口；配对结果 addresses 按直连
   *  可达性排序）：Wi-Fi ↔ 蜂窝切换断线后重连轮换用；旧凭据无此字段仍按
   *  单地址工作。 */
  candidates?: string[]
}

const STORAGE_KEY = 'lunitide:remote-bridge'
const PENDING_LIMIT = 128
const BACKEND_CAP_MS = 30_000

export function loadRemoteCredentials(): RemoteCredentials | undefined {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return undefined
    const value = JSON.parse(raw) as Partial<RemoteCredentials>
    if (typeof value.wsUrl !== 'string' || typeof value.token !== 'string' || !value.wsUrl || !value.token) return undefined
    const candidates = Array.isArray(value.candidates)
      ? value.candidates.filter((item): item is string => typeof item === 'string' && item !== '')
      : []
    return candidates.length > 0
      ? { wsUrl: value.wsUrl, token: value.token, candidates }
      : { wsUrl: value.wsUrl, token: value.token }
  } catch { return undefined }
}
export function saveRemoteCredentials(credentials: RemoteCredentials): void {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(credentials)) } catch { /* 存储满时静默：本次会话仍可用 */ }
}
export function clearRemoteCredentials(): void {
  try { localStorage.removeItem(STORAGE_KEY) } catch { /* ignore */ }
}

export type RemoteTransportState = 'connecting' | 'open' | 'reconnecting' | 'closed'
export type RemoteStateListener = (state: RemoteTransportState) => void

export class WsTransport implements WebViewTransport {
  /** 候选 wsUrl 列表：首选地址置顶，其余由网关候选裸地址换 host 派生。
   *  每次连接失败轮换到下一个（round-robin 循环覆盖全部地址）——手机在
   *  Wi-Fi ↔ 蜂窝之间切换导致断线时，重连自动落到可达地址；回前台重试
   *  （visibilitychange）不轮换，优先当前地址。 */
  private readonly candidateUrls: string[]
  private candidateIdx = 0
  private readonly token: string
  private ws: WebSocket | undefined
  private pending: string[] = []
  private listeners = new Set<(event: MessageEvent<BridgeResponse>) => void>()
  private stateListeners = new Set<RemoteStateListener>()
  private state: RemoteTransportState = 'connecting'
  private backoffMs = 1_000
  private retryTimer: number | undefined
  private disposed = false

  constructor(wsUrl: string, token: string, candidates?: string[], onStateChange?: RemoteStateListener) {
    this.candidateUrls = [wsUrl]
    for (const address of candidates ?? []) {
      const swapped = swapWsHost(wsUrl, address)
      if (swapped !== wsUrl && !this.candidateUrls.includes(swapped)) this.candidateUrls.push(swapped)
    }
    this.token = token
    // 状态监听必须在 connect 之前注册：构造即发起连接，'connecting' 状态
    // 会在 connect 内同步触发，晚注册会漏掉首态（壳层的连接失败计时依赖
    // 'connecting'/'reconnecting' 启动）。
    if (onStateChange) this.onStateChange(onStateChange)
    this.connect(false)
    document.addEventListener('visibilitychange', this.onVisibility)
  }

  get currentState(): RemoteTransportState { return this.state }

  onStateChange(listener: RemoteStateListener): () => void {
    this.stateListeners.add(listener)
    return () => { this.stateListeners.delete(listener) }
  }

  postMessage(value: unknown): void {
    if (this.disposed) throw new Error('remote transport disposed')
    const frame = JSON.stringify(value)
    if (this.ws && this.ws.readyState === WebSocket.OPEN) { this.ws.send(frame); return }
    if (this.ws && this.ws.readyState === WebSocket.CONNECTING) {
      if (this.pending.length >= PENDING_LIMIT) throw new Error('remote transport queue full')
      this.pending.push(frame); return
    }
    throw new Error('remote transport unavailable')
  }

  addEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void {
    if (type === 'message') this.listeners.add(listener)
  }

  removeEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void {
    if (type === 'message') this.listeners.delete(listener)
  }

  dispose(): void {
    this.disposed = true
    document.removeEventListener('visibilitychange', this.onVisibility)
    if (this.retryTimer !== undefined) { clearTimeout(this.retryTimer); this.retryTimer = undefined }
    this.ws?.close()
    this.ws = undefined
    this.pending = []
    this.setState('closed')
    this.listeners.clear()
    this.stateListeners.clear()
  }

  private onVisibility = () => {
    if (document.visibilityState !== 'visible' || this.disposed) return
    if (!this.ws || this.ws.readyState === WebSocket.CLOSED || this.ws.readyState === WebSocket.CLOSING) {
      if (this.retryTimer !== undefined) { clearTimeout(this.retryTimer); this.retryTimer = undefined }
      this.connect(true)
    }
  }

  private connect(reconnect: boolean): void {
    if (this.disposed) return
    this.setState(reconnect ? 'reconnecting' : 'connecting')
    // 浏览器 WebSocket 无法自定义 Authorization 头，令牌走 ?token= 查询参数。
    const url = appendToken(this.candidateUrls[this.candidateIdx], this.token)
    let ws: WebSocket
    try { ws = new WebSocket(url) } catch { this.scheduleRetry(); return }
    this.ws = ws
    ws.onopen = () => {
      if (this.disposed || this.ws !== ws) return
      this.backoffMs = 1_000
      this.setState('open')
      const queued = this.pending
      this.pending = []
      for (const frame of queued) {
        if (ws.readyState !== WebSocket.OPEN) { this.pending.unshift(frame); break }
        ws.send(frame)
      }
    }
    ws.onmessage = (event: MessageEvent<string>) => {
      if (this.disposed || this.ws !== ws) return
      let value: unknown
      try { value = JSON.parse(event.data) } catch { return }
      const synthetic = new MessageEvent('message', { data: value }) as MessageEvent<BridgeResponse>
      for (const listener of [...this.listeners]) {
        try { listener(synthetic) } catch (err) { console.error('[lunitide] remote bridge listener', err) }
      }
    }
    ws.onclose = () => { if (this.ws === ws) { this.ws = undefined; this.scheduleRetry() } }
    ws.onerror = () => { /* onclose 随后触发，统一走重连 */ }
  }

  private scheduleRetry(): void {
    if (this.disposed) return
    this.setState('reconnecting')
    // 单候选无从轮换；多候选时每次失败前进一个，循环覆盖全部地址。
    if (this.candidateUrls.length > 1) {
      this.candidateIdx = (this.candidateIdx + 1) % this.candidateUrls.length
    }
    if (this.retryTimer !== undefined) return
    this.retryTimer = window.setTimeout(() => {
      this.retryTimer = undefined
      this.connect(true)
    }, this.backoffMs)
    this.backoffMs = Math.min(BACKEND_CAP_MS, this.backoffMs * 2)
  }

  private setState(state: RemoteTransportState): void {
    if (this.state === state) return
    this.state = state
    for (const listener of [...this.stateListeners]) {
      try { listener(state) } catch { /* 状态监听器异常不影响传输 */ }
    }
  }
}

function appendToken(wsUrl: string, token: string): string {
  try {
    const url = new URL(wsUrl)
    url.searchParams.set('token', token)
    return url.toString()
  } catch { return wsUrl }
}

// 把 wsUrl 的 host 换成网关候选裸地址（IPv6 补方括号），端口/路径/协议
// 不动；解析失败返回原 wsUrl（该候选会在连接时自然失败并轮换到下一个）。
export function swapWsHost(wsUrl: string, address: string): string {
  try {
    const url = new URL(wsUrl)
    url.hostname = address.includes(':') ? `[${address}]` : address
    return url.toString()
  } catch { return wsUrl }
}

// activateRemoteTransport 激活远程模式：安装 WSS 传输 override，之后所有
// 单例 bridge（provider/project/session/...）自动改走远程网关。candidates
// 是网关候选裸地址，断线重连轮换用。重复调用会先停用旧连接。
let activeTransport: WsTransport | undefined
export function activateRemoteTransport(wsUrl: string, token: string, candidates?: string[], onStateChange?: RemoteStateListener): WsTransport {
  deactivateRemoteTransport()
  // onStateChange 直传构造器，确保首态 'connecting' 不被漏掉。
  const transport = new WsTransport(wsUrl, token, candidates, onStateChange)
  setTransportOverride(transport)
  activeTransport = transport
  return transport
}
export function deactivateRemoteTransport(): void {
  if (!activeTransport) return
  setTransportOverride(undefined)
  activeTransport.dispose()
  activeTransport = undefined
}
export function getRemoteTransport(): WsTransport | undefined { return activeTransport }
