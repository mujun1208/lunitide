// M10 wave-4 computer-control session status: a floating bar over the chat
// surface projecting the live CC state (idle/running/paused/stopped/blocked).
// Derivation merges the in-flight cc.* tool activity from the chat stream
// with the persisted config (emergency-stop latch) and the latest audit row
// for this session, refreshed on a slow poll so a stop issued from the
// settings page still lands here.
import { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, ccBridge } from '../bridge/client'
import type { CcGetAuditLogResult, CcGetConfigResult } from '../generated/bridge'

export type CcSessionStatus = 'idle' | 'running' | 'paused' | 'stopped' | 'blocked'

type AuditRow = CcGetAuditLogResult['items'][number]

export interface CcStatusState {
  status: CcSessionStatus
  enabled: boolean
  detail: string
  lastAction?: AuditRow
  emergencyStop: (reason?: string) => Promise<boolean>
}

const POLL_MS = 5000

export function useCcStatus(sessionId: string, liveTool?: string, liveToolStatus?: string): CcStatusState {
  const [config, setConfig] = useState<CcGetConfigResult | null>(null)
  const [lastAction, setLastAction] = useState<AuditRow | undefined>(undefined)
  const sessionRef = useRef(sessionId)
  sessionRef.current = sessionId

  const refresh = useCallback(async () => {
    const id = sessionRef.current
    if (!id) return
    try {
      const cfg = await ccBridge.getConfig()
      let latest: AuditRow | undefined
      try {
        const log = await ccBridge.getAuditLog({ sessionId: id, limit: 1 })
        latest = log.items[0]
      } catch { /* audit projection is best-effort */ }
      if (sessionRef.current === id) { setConfig(cfg); setLastAction(latest) }
    } catch (e) {
      // Offline bridge (tests, non-WebView2): keep the last projection.
      if (e instanceof BridgeClientError && sessionRef.current === id) setConfig(prev => prev)
    }
  }, [])

  useEffect(() => {
    setConfig(null); setLastAction(undefined)
    void refresh()
    const timer = window.setInterval(() => void refresh(), POLL_MS)
    return () => window.clearInterval(timer)
  }, [sessionId, refresh])

  const emergencyStop = useCallback(async (reason?: string) => {
    try {
      const cfg = await ccBridge.emergencyStop({ reason })
      if (sessionRef.current === sessionId) setConfig(cfg)
      return true
    } catch { return false }
  }, [sessionId])

  const status = deriveStatus(config, liveTool, liveToolStatus, lastAction)
  const detail = statusDetail(status, config, lastAction, liveTool)
  return { status, enabled: config?.enabled ?? false, detail, lastAction, emergencyStop }
}

function deriveStatus(
  config: CcGetConfigResult | null,
  liveTool?: string,
  liveToolStatus?: string,
  lastAction?: AuditRow,
): CcSessionStatus {
  if (config?.emergencyStopped) return 'stopped'
  if (liveTool?.startsWith('cc.')) {
    if (liveToolStatus === 'approval_required') return 'paused'
    if (liveToolStatus === 'tool_started') return 'running'
  }
  if (!config?.enabled) return 'idle'
  if (lastAction?.status === 'blocked') return 'blocked'
  return 'idle'
}

function statusDetail(status: CcSessionStatus, config: CcGetConfigResult | null | undefined, lastAction?: AuditRow, liveTool?: string): string {
  if (status === 'stopped') return '已紧急停止，全部电脑控制操作被熔断'
  if (status === 'paused') return '高危操作等待你的确认'
  if (status === 'running') {
    if (liveTool === 'cc.observe_dialog' || liveTool === 'cc.confirm_dialog') return '正在用无障碍接口观察或确认对话框'
    if (liveTool === 'cc.screen_capture') return '正在截取整个桌面'
    return '电脑控制操作执行中'
  }
  if (status === 'blocked' && lastAction) {
    const layer = lastAction.layer ? `（${layerLabel(lastAction.layer)}）` : ''
    return `最近一次操作被安全拦截${layer}：${lastAction.detail}`
  }
  if (!config?.enabled) return '电脑控制未启用'
  return '空闲'
}

function layerLabel(layer: string): string {
  switch (layer) {
    case 'intent': return '意图识别'
    case 'input-filter': return '输入过滤'
    case 'process-monitor': return '进程监控'
    default: return layer
  }
}

export const CC_STATUS_META: Record<CcSessionStatus, { label: string; tone: string }> = {
  idle: { label: '空闲', tone: 'idle' },
  running: { label: '运行中', tone: 'running' },
  paused: { label: '待确认', tone: 'paused' },
  stopped: { label: '已紧急停止', tone: 'stopped' },
  blocked: { label: '已拦截', tone: 'blocked' },
}

export function CcStatusBar({ state, onStop }: { state: CcStatusState; onStop?: () => void }) {
  const { status } = state
  // The bar stays hidden while CC is off and nothing has happened; once the
  // emergency-stop latch is set or an operation runs/pauses/blocks, it must
  // stay visible so the user always sees the machine-actuation state.
  if (status === 'idle' && !state.enabled) return null
  if (status === 'idle') return null
  const meta = CC_STATUS_META[status]
  return <div className={`cc-status-bar cc-${meta.tone}`} role="status" aria-label={`电脑控制 ${meta.label}`}>
    <span className="cc-status-dot" aria-hidden="true" />
    <span className="cc-status-label">电脑控制 · {meta.label}</span>
    <span className="cc-status-detail">{state.detail}</span>
    {status !== 'stopped' && <button type="button" className="cc-status-stop" onClick={onStop}>紧急停止</button>}
  </div>
}
