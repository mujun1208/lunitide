import {useEffect, useState} from 'react'
import {getOperationBridge, type OperationBridge} from '../bridge/client'
import type {OperationListResult} from '../generated/bridge'

type OperationItem = OperationListResult['items'][number]

function operationStateLabel(value: string, zh: boolean): string {
  if (!zh) return value
  switch (value) {
    case 'succeeded': return '已完成'
    case 'failed': return '失败'
    case 'cancelled': return '已取消'
    case 'intent': return '已登记'
    case 'pending': return '待执行'
    case 'partial': return '部分完成'
    case 'unknown': return '待核实'
    case 'running': return '执行中'
    default: return '待核实'
  }
}

function resumeActionLabel(value: string, zh: boolean): string {
  if (!zh) return value
  switch (value) {
    case 'show_existing': return '查看已有结果'
    case 'reread': return '重新读取'
    case 'compare_and_continue': return '对照后继续'
    case 'query_existing': return '查询已有结果'
    case 'verify_unknown': return '先核实'
    case 'reobserve': return '再观察'
    case 'keep_stopped': return '保持停止'
    default: return '先核实'
  }
}

function canCancel(state: string): boolean {
  return state === 'intent' || state === 'pending' || state === 'running' || state === 'unknown' || state === 'partial'
}

export function SessionOperationsBar({sessionId, zh, opsApi}: {sessionId: string; zh: boolean; opsApi?: OperationBridge}) {
  const [items, setItems] = useState<OperationItem[]>()
  const [busyId, setBusyId] = useState<string>()
  const [notice, setNotice] = useState<string>()
  useEffect(() => {
    let cancelled = false
    try {
      void (opsApi ?? getOperationBridge()).list({sessionId, limit: 20}).then(next => {
        if (!cancelled) setItems(next.items ?? [])
      }).catch(() => {
        if (!cancelled) setItems([])
      })
    } catch {
      setItems([])
    }
    return () => { cancelled = true }
  }, [sessionId, opsApi])
  if (!items || items.length === 0) return null
  const cancelRunning = async (item: OperationItem) => {
    if (!canCancel(item.state) || busyId) return
    const api = opsApi ?? getOperationBridge()
    setBusyId(item.id)
    setNotice(undefined)
    try {
      await api.cancel({sessionId, operationId: item.id, expectedVersion: item.expectedVersion})
      const next = await api.list({sessionId, limit: 20})
      setItems(next.items ?? [])
    } catch (err) {
      const detail = err instanceof Error ? err.message.trim() : ''
      const usable = /[\u4e00-\u9fff]/.test(detail) ? detail : ''
      setNotice(usable || (zh ? '取消失败，请重新查询后再试' : 'Cancel failed; refresh and try again'))
    } finally {
      setBusyId(undefined)
    }
  }
  return <div className="chat-usage token-usage" role="status" aria-label={zh ? '本会话操作回执' : 'Session operations'}>
    <span>{zh ? '文件批次 / 工具回执' : 'File batches / tool receipts'} ({items.length})</span>
    {items.map(item => (
      <div key={item.id}>
        {item.toolName} · {operationStateLabel(item.state, zh)} · {resumeActionLabel(item.resumeAction, zh)}
        {item.externalId ? ` · ${item.externalId}` : ''}
        {item.resumeHint ? <div>{item.resumeHint}</div> : null}
        {canCancel(item.state) ? <button type="button" disabled={busyId === item.id} onClick={() => void cancelRunning(item)}>{zh ? '停止' : 'Stop'}</button> : null}
      </div>
    ))}
    {notice ? <div>{notice}</div> : null}
  </div>
}
