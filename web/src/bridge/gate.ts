// 可用性闸门（桥探活）：配对放行进入系统 / 远程模式启动前，用最轻量的
// 非流式方法 system.health 验证「Bearer 令牌 + 桥协议 + 引擎应答」整条
// 链路真实可用。
//
// 为什么需要闸门（0.17.6 实测教训）：配对 POST 成功 ≠ 桥可用——当时壳内
// HTTPS 完全可用而桥全部报 BRIDGE_UNAVAILABLE，用户进了系统只看到全屏
// 报错（用户标准：进入正式系统就必须可用，否则退回重新扫描配对）。
// 闸门把失败拦在进入系统之前，给出明确的「重新扫码配对」出口。
import { BRIDGE_VERSION, type BridgeResponse } from '../generated/bridge'
import { newBridgeULID, type WebViewTransport } from './client'
import { FetchTransport } from './fetchTransport'

export type GateOutcome = { ok: true; engine: string; version: string } | { ok: false; error: string }

/** 闸门总预算：12s。探活 deadline 8s + 候选轮换余量；必须短于壳层 15s
 *  连接失败计时（闸门先给出结论，壳层错误页不抢跑）。 */
export const GATE_TIMEOUT_MS = 12_000
/** system.health 探活请求自身 deadline。 */
const GATE_DEADLINE_MS = 8_000

// probeViaTransport 在给定传输上发一条 system.health 并等待响应帧。
// 独立于传输构造（可注入任意 WebViewTransport / 已激活的 FetchTransport），
// 便于 pairApp（新建临时传输）与 main.tsx（复用已激活传输）共用同一探活。
export function probeViaTransport(transport: WebViewTransport, timeoutMs: number = GATE_TIMEOUT_MS): Promise<GateOutcome> {
  const id = newBridgeULID()
  return new Promise<GateOutcome>(resolve => {
    let settled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const onMessage = (event: MessageEvent<BridgeResponse>) => {
      const frame = event.data
      if (!frame || frame.kind !== 'response' || frame.requestId !== id) return
      if (frame.ok) {
        const payload = frame.payload as { engine?: unknown; version?: unknown } | undefined
        finish({ ok: true, engine: String(payload?.engine ?? ''), version: String(payload?.version ?? '') })
      } else {
        finish({ ok: false, error: frame.error.message || '连接电脑验证失败，请重试。' })
      }
    }
    const finish = (outcome: GateOutcome) => {
      if (settled) return
      settled = true
      if (timer !== undefined) clearTimeout(timer)
      transport.removeEventListener('message', onMessage)
      resolve(outcome)
    }
    timer = setTimeout(() => {
      finish({ ok: false, error: '连接电脑超时，请确认电脑端 Lunitide 正在运行、手机网络可达后重试。' })
    }, timeoutMs)
    transport.addEventListener('message', onMessage)
    try {
      transport.postMessage({
        v: BRIDGE_VERSION, kind: 'request', id, traceId: newBridgeULID(),
        method: 'system.health', sentAt: new Date().toISOString(),
        payload: {}, deadlineMs: GATE_DEADLINE_MS,
      })
    } catch {
      finish({ ok: false, error: '连接电脑失败，请确认电脑端 Lunitide 正在运行后重试。' })
    }
  })
}

// probeWithCredentials 用刚保存的配对凭据新建临时 FetchTransport 探活，
// 结束即销毁（配对页场景：不放行「进入系统」前的验证）。
export async function probeWithCredentials(
  credentials: { wsUrl: string; token: string; candidates?: string[] },
  timeoutMs: number = GATE_TIMEOUT_MS,
): Promise<GateOutcome> {
  const transport = new FetchTransport(credentials.wsUrl, credentials.token, credentials.candidates)
  try {
    return await probeViaTransport(transport, timeoutMs)
  } finally {
    transport.dispose()
  }
}
