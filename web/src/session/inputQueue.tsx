// M10 queued input (wave 2): durable supplements enqueued while a chat
// stream is running. The engine consumes the queue at a tool-loop
// boundary and folds it into the current turn; the renderer also
// consumes leftover rows after a stream settles (zero loss).
import { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, runQueueBridge } from '../bridge/client'

export interface QueuedItem {
  queuedId: string
  seq: number
  text: string
  status: string
  mark: string
  createdAt: string
}

export interface InputQueueState {
  items: QueuedItem[]
  notice: string
  enqueue: (text: string) => Promise<boolean>
  withdraw: (queuedId: string) => Promise<void>
  refresh: () => Promise<void>
  flushAfterStream: (send: (text: string) => void) => Promise<void>
}

export function useInputQueue(sessionId: string, streaming = false): InputQueueState {
  const [items, setItems] = useState<QueuedItem[]>([])
  const [notice, setNotice] = useState('')
  const sessionRef = useRef(sessionId)
  sessionRef.current = sessionId

  const refresh = useCallback(async () => {
    const id = sessionRef.current
    if (!id) { setItems([]); return }
    try {
      const r = await runQueueBridge.list({ sessionId: id })
      if (sessionRef.current === id) setItems(r.items)
    } catch { /* offline bridge: keep last projection */ }
  }, [])

  useEffect(() => {
    setItems([]); setNotice('')
    void refresh()
  }, [sessionId, refresh])

  useEffect(() => {
    if (!streaming) return
    const timer = window.setInterval(() => { void refresh() }, 1500)
    return () => window.clearInterval(timer)
  }, [streaming, refresh])

  const enqueue = useCallback(async (text: string) => {
    const trimmed = text.trim()
    if (!trimmed) return false
    try {
      await runQueueBridge.input({ sessionId: sessionRef.current, text: trimmed, requestId: `ui-${crypto.randomUUID()}` })
      setNotice('')
      await refresh()
      return true
    } catch (e) {
      setNotice(queueNotice(e))
      return false
    }
  }, [refresh])

  const withdraw = useCallback(async (queuedId: string) => {
    try {
      await runQueueBridge.withdraw({ sessionId: sessionRef.current, queuedId })
      setNotice('')
    } catch (e) {
      setNotice(queueNotice(e))
    }
    await refresh()
  }, [refresh])

  const flushAfterStream = useCallback(async (send: (text: string) => void) => {
    try {
      const r = await runQueueBridge.consume({ sessionId: sessionRef.current })
      if (!r.count) return
      const merged = r.items.length === 1
        ? r.items[0].text
        : r.items.map(m => `[运行中补充 #${m.seq}] ${m.text}`).join('\n')
      setItems([])
      send(merged)
    } catch { /* consume failed: rows stay queued and resurface on next list */ }
  }, [])

  return { items, notice, enqueue, withdraw, refresh, flushAfterStream }
}

function queueNotice(e: unknown): string {
  const code = e instanceof BridgeClientError ? e.code : ''
  switch (code) {
    case 'M10-QI-001': return '补充内容为空或超过 8000 字符'
    case 'M10-QI-002': return '会话不可用，请刷新'
    case 'M10-QI-005': return '队列已满（5 条），请先撤回或等待注入'
    case 'M10-QI-007': return '排队过于频繁，请稍候再试'
    default: return '排队暂不可用，请稍后重试'
  }
}

export function QueueStrip({ items, notice, onWithdraw, disabled }: {
  items: QueuedItem[]
  notice: string
  onWithdraw: (queuedId: string) => void
  disabled?: boolean
}) {
  if (!items.length && !notice) return null
  return <div className="input-queue-wrap">
    {notice && <div className="input-queue-notice" role="alert">{notice}</div>}
    <div className="input-queue" role="list" aria-label="排队中的补充输入">
      {items.map(m => <div className="input-queue-item" role="listitem" key={m.queuedId}>
        <span className="input-queue-badge" aria-hidden="true">⏳</span>
        <span className="input-queue-badge">#{m.seq} 等待插入</span>
        <span className="input-queue-text">{m.text}</span>
        <button type="button" disabled={disabled} onClick={() => onWithdraw(m.queuedId)}>撤回</button>
      </div>)}
    </div>
    <span className="sr-only" aria-live="polite">{items.length ? `${items.length} 条补充排队中，将在合适时机并入当前任务` : notice}</span>
  </div>
}
