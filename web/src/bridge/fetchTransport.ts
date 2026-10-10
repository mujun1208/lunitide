// 移动伴侣远程传输（HTTPS NDJSON 流式版）：把 WebViewTransport 语义
//（postMessage + message 事件）承载到网关的 /bridge/http 端点上。
//
// 为什么弃用 WSS：0.17.6 实测安卓壳内 WebView 的 WebSocket 升级在部分
// 网络栈下静默失败（同一 WebView 里 HTTPS 配对 POST 成功、/bridge WSS 零
// 连接），系统全部报 BRIDGE_UNAVAILABLE；HTTPS fetch 则完全可用。浏览器
// PWA 与壳统一走本传输，单一代码路径、全部设备行为一致可审计。
//
// 协议：一次 postMessage = 一次 POST = 一条 NDJSON 流。响应帧先行，流事件
// 随后持续推送，流终结后服务端关流——与 WSS「一条 WS 消息即一帧 JSON」的
// 帧格式完全一致，客户端协议层（createChatBridge/createSimpleBridge）零
// 改动。多请求天然并行（每请求独立 fetch），无连接复用的队头阻塞。
//
// 生命周期：
// - 构造即 'connecting'；首个响应头到达置 'open'（壳层 15s 计时依赖该
//   状态机：connecting/reconnecting 15s 内未 open 判定电脑不可达）。
// - 网络层失败（连接建立失败/流中断且响应未达）轮换候选地址重试——
//   Wi-Fi ↔ 蜂窝切换后无需重新扫码；全部候选耗尽合成 BRIDGE_UNAVAILABLE
//   错误帧，请求按既有超时/重试路径失败。
// - 流式请求读到终结事件（completed/cancelled/failed/terminal_exit/
//   talk_ended）后 abort 连接；EOF 而未收到终结时合成 failed 事件（防
//   服务端异常关闭把 UI 挂在等事件的空转上）。
import { BRIDGE_VERSION, type BridgeResponse } from '../generated/bridge'
import { setTransportOverride, newBridgeULID, type WebViewTransport } from './client'
import { swapWsHost, type RemoteStateListener, type RemoteTransportState } from './wsTransport'

/** 终结事件：读到即 abort 本流（服务端写完终结帧也会主动关，双保险）。 */
const TERMINAL_EVENT_TYPES = new Set(['completed', 'cancelled', 'failed', 'terminal_exit', 'talk_ended'])
/** 读流空闲 abort：与服务端 httpStreamIdleTimeout(600s) 对齐外加余量。 */
const STREAM_IDLE_ABORT_MS = 610_000

interface OutgoingRequest { id?: unknown; method?: unknown; deadlineMs?: unknown }

// 响应头超时：黑洞性地址（防火墙静默丢包，如 Windows 防火墙对外网段
// SYN 的默认丢弃）fetch 既不 resolve 也不 reject，catch 轮换永远不会触发
// ——Wi-Fi 换网络/离家的旧 IPv4 正是这种地址，不设此守卫「在外走蜂窝
// 自动切换 IPv6」永远轮不到。服务端首帧（响应帧）在方法返回时即写，慢
// 网络下响应头也远早于 deadline；超时随请求 deadline 走（deadline+2s），
// 不会误杀 provider.model.sync 这类长 deadline 请求。无 deadline 的裸帧
// 兜底 15s。
function headerTimeoutMs(request: OutgoingRequest): number {
  const deadline = Number(request.deadlineMs)
  return Number.isFinite(deadline) && deadline > 0 ? deadline + 2_000 : 15_000
}

// wss://host:47651/bridge → https://host:47651/bridge/http。
function deriveHttpUrl(wsUrl: string): string {
  try {
    const url = new URL(wsUrl)
    url.protocol = 'https:'
    url.pathname = '/bridge/http'
    return url.toString()
  } catch { return wsUrl }
}

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)

export class FetchTransport implements WebViewTransport {
  private readonly candidateUrls: string[]
  private candidateIdx = 0
  private readonly token: string
  private listeners = new Set<(event: MessageEvent<BridgeResponse>) => void>()
  private stateListeners = new Set<RemoteStateListener>()
  private state: RemoteTransportState = 'connecting'
  private disposed = false
  private activeControllers = new Set<AbortController>()

  constructor(wsUrl: string, token: string, candidates?: string[], onStateChange?: RemoteStateListener) {
    const primary = deriveHttpUrl(wsUrl)
    this.candidateUrls = [primary]
    for (const address of candidates ?? []) {
      const swapped = swapWsHost(primary, address)
      if (swapped !== primary && !this.candidateUrls.includes(swapped)) this.candidateUrls.push(swapped)
    }
    this.token = token
    // 状态监听先于任何请求注册：壳层 15s 失败计时从首态 'connecting' 启动。
    if (onStateChange) this.onStateChange(onStateChange)
  }

  get currentState(): RemoteTransportState { return this.state }

  onStateChange(listener: RemoteStateListener): () => void {
    this.stateListeners.add(listener)
    listener(this.state)
    return () => { this.stateListeners.delete(listener) }
  }

  postMessage(value: unknown): void {
    if (this.disposed) throw new Error('remote transport disposed')
    const frame = JSON.stringify(value)
    void this.roundTrip(frame, value as OutgoingRequest, 0)
  }

  addEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void {
    if (type === 'message') this.listeners.add(listener)
  }

  removeEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void {
    if (type === 'message') this.listeners.delete(listener)
  }

  dispose(): void {
    this.disposed = true
    for (const controller of this.activeControllers) controller.abort()
    this.activeControllers.clear()
    this.setState('closed')
    this.listeners.clear()
    this.stateListeners.clear()
  }

  private dispatch(frame: unknown): void {
    const synthetic = new MessageEvent('message', { data: frame }) as MessageEvent<BridgeResponse>
    for (const listener of [...this.listeners]) {
      try { listener(synthetic) } catch (err) { console.error('[lunitide] remote bridge listener', err) }
    }
  }

  /** 合成错误响应帧：请求送不出去（全候选不可达/网关拒绝）时让上层按
   *  正常错误路径 reject，而不是静默挂到 deadline 超时。 */
  private emitSyntheticError(request: OutgoingRequest, code: string, message: string, retryable: boolean): void {
    if (typeof request.id !== 'string') return
    this.dispatch({
      v: BRIDGE_VERSION,
      kind: 'response',
      id: newBridgeULID(),
      requestId: request.id,
      ok: false,
      error: { code, message, retryable, correlationId: newBridgeULID() },
    })
  }

  private async roundTrip(frame: string, request: OutgoingRequest, candidateAttempt: number): Promise<void> {
    const url = this.candidateUrls[this.candidateIdx]
    const controller = new AbortController()
    this.activeControllers.add(controller)
    // 响应头超时：黑洞性地址上 fetch 永不落定，abort 才能触发 catch 轮换。
    // 响应头一旦到达即解除（流内读超时由 STREAM_IDLE_ABORT_MS 负责）。
    let headerTimedOut = false
    const headerTimer = setTimeout(() => { headerTimedOut = true; controller.abort() }, headerTimeoutMs(request))
    try {
      const response = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${this.token}` },
        body: frame,
        signal: controller.signal,
        cache: 'no-store',
      })
      clearTimeout(headerTimer)
      if (!response.ok || !response.body) {
        // 404 = 服务端没有 /bridge/http 端点（0.17.6 及更早的电脑端）：
        // 协议不存在的明确信号，轮换候选无意义。文案直指升级电脑端——
        // 0.17.7 实测：桌面端未更新时 gate 只显示「远程网关错误（404）」，
        // 用户不知道问题出在电脑端版本。
        if (response.status === 404) {
          this.emitSyntheticError(request, 'REMOTE_PROTOCOL_UNSUPPORTED', '电脑端 Lunitide 版本过旧，不支持新版连接协议。请先将电脑端升级到最新版本，再重新扫码配对。', false)
          return
        }
        // 网关可达但拒绝（401 令牌失效/405/5xx）：轮换无意义，转错误帧。
        const code = response.status === 401 ? 'REMOTE_UNAUTHORIZED' : 'REMOTE_GATEWAY_ERROR'
        const message = response.status === 401 ? '设备授权已失效，请重新扫码配对' : `远程网关错误（${response.status}）`
        this.emitSyntheticError(request, code, message, response.status !== 401)
        return
      }
      this.setState('open')
      const interrupted = await this.drainStream(response.body, controller, request)
      if (interrupted && candidateAttempt + 1 < this.candidateUrls.length && !this.disposed) {
        // 响应帧未达即流中断：请求在服务端未受理，轮换候选重发。
        this.candidateIdx = (this.candidateIdx + 1) % this.candidateUrls.length
        this.setState('reconnecting')
        await this.roundTrip(frame, request, candidateAttempt + 1)
      }
    } catch {
      clearTimeout(headerTimer)
      // dispose/读完终结帧的主动 abort 静默退出；黑洞超时的 abort 与网络
      // 失败同路径——轮换候选重发。
      if (this.disposed || (controller.signal.aborted && !headerTimedOut)) return
      if (candidateAttempt + 1 < this.candidateUrls.length) {
        // 连接建立失败：轮换候选重试（Wi-Fi ↔ 蜂窝切换的可达性兜底）。
        this.candidateIdx = (this.candidateIdx + 1) % this.candidateUrls.length
        this.setState('reconnecting')
        await this.roundTrip(frame, request, candidateAttempt + 1)
        return
      }
      this.setState('reconnecting')
      this.emitSyntheticError(request, 'BRIDGE_UNAVAILABLE', '无法连接电脑，请确认电脑端 Lunitide 已打开且网络可达', true)
    } finally {
      this.activeControllers.delete(controller)
    }
  }

  /** 读 NDJSON 流到 EOF。返回 true 表示「响应帧未达即中断」（调用方轮换
   *  候选重发）；响应已达后的中断由本方法合成 failed 事件兜底。 */
  private async drainStream(body: ReadableStream<Uint8Array>, controller: AbortController, request: OutgoingRequest): Promise<boolean> {
    const reader = body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let streamId: string | undefined
    let lastSeq = 0
    let terminalSeen = false
    let idleTimer: ReturnType<typeof setTimeout> | undefined
    const armIdle = () => {
      if (idleTimer !== undefined) clearTimeout(idleTimer)
      idleTimer = setTimeout(() => { void reader.cancel('idle').catch(() => {}) }, STREAM_IDLE_ABORT_MS)
    }
    armIdle()
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let nl: number
        while ((nl = buffer.indexOf('\n')) >= 0) {
          const line = buffer.slice(0, nl).trim()
          buffer = buffer.slice(nl + 1)
          if (!line) continue
          let frame: Record<string, unknown> | undefined
          try { const parsed = JSON.parse(line); if (isObj(parsed)) frame = parsed } catch { continue }
          if (!frame) continue
          armIdle()
          this.dispatch(frame)
          if (frame.kind === 'response' && isObj(frame.payload)) {
            const candidate = frame.payload.streamId
            if (typeof candidate === 'string' && candidate) streamId = candidate
          } else if (frame.kind === 'event') {
            if (typeof frame.streamId === 'string') lastSeq = Math.max(lastSeq, Number(frame.sequence) || 0)
            if (typeof frame.type === 'string' && TERMINAL_EVENT_TYPES.has(frame.type)) terminalSeen = true
          }
        }
      }
      // EOF：服务端已关流。流式请求未收到终结帧（异常关闭/空闲回收）时
      // 合成 failed——sequence 接续已收事件，createChatBridge 的顺序校验
      // 可通过，UI 得到可重试的明确失败而非无限等待。
      if (streamId && !terminalSeen) {
        this.dispatch({
          v: BRIDGE_VERSION, kind: 'event', id: newBridgeULID(), streamId,
          sequence: lastSeq + 1, type: 'failed',
          error: { code: 'REMOTE_STREAM_DISCONNECTED', message: '与电脑的连接中断，请重试', retryable: true },
        })
      }
      return false
    } catch {
      // 读流异常：响应帧未达 = 请求丢失（可轮换重发）；已达 = 流中断，
      // 合成 failed（与 EOF 同语义）。
      if (streamId && !terminalSeen) {
        this.dispatch({
          v: BRIDGE_VERSION, kind: 'event', id: newBridgeULID(), streamId,
          sequence: lastSeq + 1, type: 'failed',
          error: { code: 'REMOTE_STREAM_DISCONNECTED', message: '与电脑的连接中断，请重试', retryable: true },
        })
      }
      return !streamId && !this.disposed && !controller.signal.aborted
    } finally {
      if (idleTimer !== undefined) clearTimeout(idleTimer)
      if (terminalSeen || streamId) controller.abort()
    }
  }

  private setState(state: RemoteTransportState): void {
    if (this.state === state) return
    this.state = state
    for (const listener of [...this.stateListeners]) {
      try { listener(state) } catch { /* 状态监听器异常不影响传输 */ }
    }
  }
}

// activateRemoteTransport 激活远程模式：安装 HTTPS 传输 override，之后
// 所有单例 bridge（provider/project/session/chat/...）自动改走远程网关。
// wsUrl 是配对时保存的 WSS 形式地址（凭据结构兼容旧版本），内部派生
// /bridge/http；candidates 是网关候选裸地址，网络层失败轮换用。
// 重复调用会先停用旧传输。
let activeTransport: FetchTransport | undefined
export function activateRemoteTransport(wsUrl: string, token: string, candidates?: string[], onStateChange?: RemoteStateListener): FetchTransport {
  deactivateRemoteTransport()
  const transport = new FetchTransport(wsUrl, token, candidates, onStateChange)
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
export function getRemoteTransport(): FetchTransport | undefined { return activeTransport }
