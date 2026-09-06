// M10 queued input (wave 2): durable supplements enqueued while a chat
// stream is running. The engine consumes the queue at a tool-loop
// boundary and folds it into the current turn; the renderer also
// prepares leftover rows after a stream settles using durable delivery receipts.
import { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, runQueueBridge } from '../bridge/client'
import { FOLLOW_UP_QUEUE_NOTICE } from './turnControl'
import type { RunQueueListResult } from '../generated/bridge'

export type QueueDelivery = NonNullable<RunQueueListResult['delivery']>
type QueueSender = (text: string, deliveryId: string) => unknown | Promise<unknown>

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
  delivery?: QueueDelivery
  enqueue: (text: string) => Promise<boolean>
  withdraw: (queuedId: string) => Promise<void>
  refresh: () => Promise<void>
  flushAfterStream: (send: QueueSender) => Promise<void>
  recoverDelivery: (send: QueueSender, action: 'resume' | 'dismiss') => Promise<void>
}

export function useInputQueue(sessionId: string, streaming = false): InputQueueState {
  const [items, setItems] = useState<QueuedItem[]>([])
  const [notice, setNotice] = useState('')
  const [delivery, setDelivery] = useState<QueueDelivery>()
  const flushing = useRef<{ epoch: number } | undefined>(undefined)
  const pendingEnqueue = useRef<{ sessionId: string; text: string; requestId: string } | undefined>(undefined)
  const sessionRef = useRef(sessionId)
  const epoch = useRef(0)
  const mounted = useRef(true)
  if (sessionRef.current !== sessionId) { sessionRef.current = sessionId; epoch.current++ }
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; epoch.current++ } }, [])

  const refresh = useCallback(async () => {
    const id = sessionRef.current
    const generation = epoch.current
    const current = () => mounted.current && generation === epoch.current && sessionRef.current === id
    if (!id) { setItems([]); setDelivery(undefined); return }
    try {
      const r = await runQueueBridge.list({ sessionId: id })
      if (current()) { setItems(r.items); setDelivery(r.delivery) }
    } catch { if (current()) setNotice('补充输入状态暂时无法读取，请重试核对') }
  }, [])

  useEffect(() => {
    setItems([]); setNotice(''); setDelivery(undefined)
    void refresh()
  }, [sessionId, refresh])

  useEffect(() => {
    if (!streaming && delivery?.state !== 'started') return
    const timer = window.setInterval(() => { void refresh() }, 1500)
    return () => window.clearInterval(timer)
  }, [streaming, delivery?.state, refresh])

  const enqueue = useCallback(async (text: string) => {
    const trimmed = text.trim()
    if (!trimmed) return false
    const id = sessionRef.current
    const generation = epoch.current
    const current = () => mounted.current && generation === epoch.current && sessionRef.current === id
    const request = pendingEnqueue.current?.sessionId === id && pendingEnqueue.current.text === trimmed
      ? pendingEnqueue.current : { sessionId: id, text: trimmed, requestId: `ui-${crypto.randomUUID()}` }
    pendingEnqueue.current = request
    try {
      await runQueueBridge.input(request)
      if (!current()) return false
      pendingEnqueue.current = undefined
      setNotice(FOLLOW_UP_QUEUE_NOTICE)
      await refresh()
      return true
    } catch (e) {
      if (current()) setNotice(queueNotice(e))
      return false
    }
  }, [refresh])

  const withdraw = useCallback(async (queuedId: string) => {
    const id = sessionRef.current, generation = epoch.current
    const current = () => mounted.current && generation === epoch.current && sessionRef.current === id
    try {
      await runQueueBridge.withdraw({ sessionId: id, queuedId })
      if (current()) setNotice('')
    } catch (e) {
      if (current()) setNotice(queueNotice(e))
    }
    if (current()) await refresh()
  }, [refresh])

  const deliver = useCallback(async (send: QueueSender, action?: 'resume' | 'dismiss') => {
    if (flushing.current?.epoch === epoch.current) return
    const id = sessionRef.current
    const generation = epoch.current
    const current = () => mounted.current && generation === epoch.current && sessionRef.current === id
    if (!id) return
    const token = { epoch: generation }
    flushing.current = token
    try {
      const resolve = action && delivery && (action === 'dismiss' || delivery.state === 'unknown') ? { deliveryId: delivery.id, action } : {}
      const r = await runQueueBridge.consume({ sessionId: id, ...resolve })
      if (!current()) return
      setDelivery(r.delivery)
      if (!r.count) return
      if (r.delivery?.state === 'confirmed') { setNotice('已记录你的核对结果'); await refresh(); return }
      if (!r.delivery || r.delivery.state !== 'prepared') {
        setNotice(r.delivery?.state === 'started' ? '补充输入已启动，请等待结果；不会重复发送' : '补充输入的执行结果待核对，请查看对话后决定是否继续')
        await refresh()
        return
      }
      const merged = r.items.length === 1
        ? r.items[0].text
        : r.items.map(m => `[运行中补充 #${m.seq}] ${m.text}`).join('\n')
      const started = await send(merged, r.delivery.id)
      if (!current()) return
      setNotice(started === true ? '' : '补充说明已保存，启动尚未确认；请核对实际状态后重试')
      await refresh()
    } catch {
      if (current()) { setNotice('补充输入的交付尚未确认，已保留记录；请核对后重试'); await refresh() }
    } finally { if (flushing.current === token) flushing.current = undefined }
  }, [delivery, refresh])
  const flushAfterStream = useCallback((send: QueueSender) => deliver(send), [deliver])
  const recoverDelivery = useCallback((send: QueueSender, action: 'resume' | 'dismiss') => deliver(send, action), [deliver])

  return { items, notice, delivery, enqueue, withdraw, refresh, flushAfterStream, recoverDelivery }
}

function queueNotice(e: unknown): string {
  const code = e instanceof BridgeClientError ? e.code : ''
  switch (code) {
    case 'M10-QI-001': return '补充内容为空或超过 8000 字符'
    case 'M10-QI-002': return '会话不可用，请刷新'
    case 'M10-QI-004': return '这条说明已处理或内容已变化，请核对对话与交付记录'
    case 'M10-QI-005': return '队列已满（5 条），请先撤回或等待注入'
    case 'M10-QI-007': return '排队过于频繁，请稍候再试'
    default: return '排队暂不可用，请稍后重试'
  }
}

export function QueueStrip({ items, notice, onWithdraw, disabled, delivery, onResume, onDismiss }: {
  items: QueuedItem[]
  notice: string
  onWithdraw: (queuedId: string) => void
  disabled?: boolean
  delivery?: QueueDelivery
  onResume?: () => void
  onDismiss?: () => void
}) {
  if (!items.length && !notice && !delivery) return null
  return <div className="input-queue-wrap">
    {notice && <div className="input-queue-notice" role="alert">{notice}</div>}
    {delivery && <div className="input-queue-notice" role="status">
      <span>{delivery.state === 'started' ? '已交付，等待执行回执' : delivery.state === 'unknown' ? '执行结果待核对' : '补充说明已保存，等待启动'}</span>
      <p>{delivery.items.map(item => item.text).join('\n')}</p>
      {delivery.state !== 'started' && <button type="button" disabled={disabled} onClick={onResume}>{delivery.state === 'unknown' ? '核对后重新执行' : '继续发送已保存说明'}</button>}
      {delivery.state !== 'started' && <button type="button" disabled={disabled} onClick={onDismiss}>{delivery.state === 'unknown' ? '已核对，不再执行' : '取消这批说明'}</button>}
    </div>}
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
