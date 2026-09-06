// M10 queued-input hook and strip coverage: idempotent enqueue projection,
// queue-full notice mapping, withdraw refresh, and flushAfterStream replay
// (single passthrough or multi-item merge) per FR-28/FR-34.
import { act, cleanup, render, renderHook, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, runQueueBridge, type RunQueueBridge } from '../bridge/client'
import { FOLLOW_UP_QUEUE_NOTICE } from './turnControl'
import { QueueStrip, useInputQueue } from './inputQueue'

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  const bridge: RunQueueBridge = {
    input: vi.fn(),
    list: vi.fn().mockResolvedValue({ items: [] }),
    withdraw: vi.fn(),
    consume: vi.fn(),
  }
  return { ...actual, runQueueBridge: bridge, getRunQueueBridge: () => bridge }
})

afterEach(() => { cleanup(); vi.resetAllMocks(); vi.mocked(runQueueBridge.list).mockResolvedValue({ items: [] }) })

const queue = () => vi.mocked(runQueueBridge)
const DELIVERY_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAY', MESSAGE_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAZ'

const item = (seq: number, text: string) => ({ queuedId: `01ARZ3NDEKTSV4RRFFQ69G5FA${String(seq).padStart(2, '0')}`, seq, text, status: 'queued' as const, mark: 'turn_boundary' as const, createdAt: '2025-01-01T00:00:00Z' })

it('loads the queued projection on mount and after enqueue', async () => {
  const bridge = queue()
  vi.mocked(bridge.list).mockResolvedValue({ items: [item(1, 'first')] })
  const { result } = renderHook(() => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await waitFor(() => expect(result.current.items).toHaveLength(1))
  expect(bridge.list).toHaveBeenCalledWith({ sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV' })
  vi.mocked(bridge.list).mockResolvedValue({ items: [item(1, 'first'), item(2, 'second')] })
  await act(async () => { await result.current.enqueue('second') })
  expect(bridge.input).toHaveBeenCalledWith(expect.objectContaining({ sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', text: 'second' }))
  await waitFor(() => expect(result.current.items).toHaveLength(2))
  expect(result.current.notice).toBe(FOLLOW_UP_QUEUE_NOTICE)
})

it('maps queue-full failures to an actionable notice and keeps items', async () => {
  const bridge = queue()
  vi.mocked(bridge.list).mockResolvedValue({ items: [] })
  vi.mocked(bridge.input).mockRejectedValue(new BridgeClientError('queue full', 'M10-QI-005', false, 'renderer'))
  const { result } = renderHook(() => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await act(async () => { const ok = await result.current.enqueue('overflow'); expect(ok).toBe(false) })
  expect(result.current.notice).toBe('队列已满（5 条），请先撤回或等待注入')
})

it('withdraws a queued row and refreshes the projection', async () => {
  const bridge = queue()
  const first = item(1, 'keep'), gone = item(2, 'gone')
  vi.mocked(bridge.list).mockResolvedValue({ items: [first, gone] })
  const { result } = renderHook(() => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await waitFor(() => expect(result.current.items).toHaveLength(2))
  vi.mocked(bridge.list).mockResolvedValue({ items: [first] })
  await act(async () => { await result.current.withdraw(gone.queuedId) })
  expect(bridge.withdraw).toHaveBeenCalledWith({ sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', queuedId: gone.queuedId })
  await waitFor(() => expect(result.current.items.map(x => x.seq)).toEqual([1]))
})

it('flushes queued rows as one merged send after a stream completes', async () => {
  const bridge = queue()
  const rows = [{ queuedId: '01ARZ3NDEKTSV4RRFFQ69G5FA01', seq: 1, text: 'alpha', status: 'injected' as const, mark: 'turn_boundary' as const, createdAt: '2025-01-01T00:00:00Z' }, { queuedId: '01ARZ3NDEKTSV4RRFFQ69G5FA02', seq: 2, text: 'beta', status: 'injected' as const, mark: 'turn_boundary' as const, createdAt: '2025-01-01T00:00:00Z' }]
  vi.mocked(bridge.consume).mockResolvedValue({ count: 2, items: rows, delivery: { id: DELIVERY_ID, state: 'prepared', messageIds: [MESSAGE_ID], items: rows } })
  const { result } = renderHook(() => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  const send = vi.fn().mockResolvedValue(true)
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(send).toHaveBeenCalledOnce()
  expect(send.mock.calls[0][0]).toBe('[运行中补充 #1] alpha\n[运行中补充 #2] beta')
  expect(result.current.items).toHaveLength(0)
})

it('polls the queue projection while a stream is running', async () => {
  vi.useFakeTimers()
  const bridge = queue()
  vi.mocked(bridge.list).mockResolvedValue({ items: [] })
  const { rerender } = renderHook(({ streaming }: { streaming: boolean }) => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV', streaming), { initialProps: { streaming: true } })
  await act(async () => { await Promise.resolve() })
  const calls = vi.mocked(bridge.list).mock.calls.length
  vi.mocked(bridge.list).mockResolvedValue({ items: [item(1, 'mid-turn')] })
  await act(async () => { vi.advanceTimersByTime(1600) })
  expect(vi.mocked(bridge.list).mock.calls.length).toBeGreaterThan(calls)
  rerender({ streaming: false })
  const after = vi.mocked(bridge.list).mock.calls.length
  await act(async () => { vi.advanceTimersByTime(1600) })
  expect(vi.mocked(bridge.list).mock.calls.length).toBe(after)
  vi.useRealTimers()
})

it('keeps the composer untouched when nothing is queued at flush time', async () => {
  const bridge = queue()
  vi.mocked(bridge.consume).mockResolvedValue({ count: 0, items: [] })
  const { result } = renderHook(() => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  const send = vi.fn()
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(send).not.toHaveBeenCalled()
})

it('renders the strip with pending items, withdrawal, and failure notices', async () => {
  const onWithdraw = vi.fn(), user = userEvent.setup()
  const { rerender } = render(<QueueStrip items={[item(3, '等待注入的补充')]} notice="" onWithdraw={onWithdraw} />)
  expect(screenListLabel()).toBeTruthy()
  expect(document.querySelector('.input-queue-text')?.textContent).toBe('等待注入的补充')
  await user.click(document.querySelector('.input-queue-item button')!)
  expect(onWithdraw).toHaveBeenCalledWith('01ARZ3NDEKTSV4RRFFQ69G5FA03')
  rerender(<QueueStrip items={[]} notice="队列已满（5 条），请先撤回或等待注入" onWithdraw={onWithdraw} />)
  expect(document.querySelector('.input-queue-notice')?.getAttribute('role')).toBe('alert')
  rerender(<QueueStrip items={[]} notice="" onWithdraw={onWithdraw} />)
  expect(document.querySelector('.input-queue-wrap')).toBeNull()
})

function screenListLabel() {
  return document.querySelector('.input-queue[role="list"]')
}

it('retains the enqueue request identity when its commit acknowledgement is lost', async () => {
  const bridge = queue()
  vi.mocked(bridge.input).mockRejectedValueOnce(new Error('ACK lost')).mockResolvedValueOnce({ queuedId: MESSAGE_ID, seq: 1, status: 'queued', mark: 'turn_boundary' })
  const { result } = renderHook(() => useInputQueue(MESSAGE_ID))
  await act(async () => { expect(await result.current.enqueue('same input')).toBe(false) })
  await act(async () => { expect(await result.current.enqueue('same input')).toBe(true) })
  expect(vi.mocked(bridge.input).mock.calls[0][0]).toEqual(vi.mocked(bridge.input).mock.calls[1][0])
})

it('recovers a lost consume acknowledgement using the saved delivery without double sending', async () => {
  const bridge = queue(), rows = [item(1, 'saved supplement')]
  const delivery = { id: DELIVERY_ID, state: 'prepared' as const, messageIds: [MESSAGE_ID], items: rows }
  vi.mocked(bridge.consume).mockRejectedValueOnce(new Error('ACK lost')).mockResolvedValueOnce({ count: 1, items: rows, delivery })
  vi.mocked(bridge.list).mockResolvedValue({ items: [], delivery })
  const { result } = renderHook(() => useInputQueue(MESSAGE_ID))
  const send = vi.fn().mockResolvedValue(true)
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(send).not.toHaveBeenCalled()
  expect(result.current.delivery?.id).toBe(DELIVERY_ID)
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(send).toHaveBeenCalledExactlyOnceWith('saved supplement', DELIVERY_ID)
})

it('does not resend after a failed start response when the stored state says started', async () => {
  const bridge = queue(), rows = [item(1, 'saved supplement')]
  const prepared = { id: DELIVERY_ID, state: 'prepared' as const, messageIds: [MESSAGE_ID], items: rows }
  const started = { ...prepared, state: 'started' as const, streamId: MESSAGE_ID }
  vi.mocked(bridge.consume).mockResolvedValueOnce({ count: 1, items: rows, delivery: prepared }).mockResolvedValue({ count: 1, items: rows, delivery: started })
  vi.mocked(bridge.list).mockResolvedValue({ items: [], delivery: started })
  const { result } = renderHook(() => useInputQueue(MESSAGE_ID))
  const send = vi.fn().mockResolvedValue('error')
  await act(async () => { await result.current.flushAfterStream(send) })
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(send).toHaveBeenCalledOnce()
  expect(result.current.delivery?.state).toBe('started')
  expect(result.current.notice).toContain('不会重复发送')
})

it('requires explicit recovery for an uncertain delivery and preserves the same identity', async () => {
  const bridge = queue(), rows = [item(1, 'verify this batch')]
  const delivery = { id: DELIVERY_ID, state: 'unknown' as const, messageIds: [MESSAGE_ID], items: rows }
  vi.mocked(bridge.list).mockResolvedValue({ items: [], delivery })
  vi.mocked(bridge.consume).mockResolvedValue({ count: 1, items: rows, delivery })
  const { result } = renderHook(() => useInputQueue(MESSAGE_ID))
  await waitFor(() => expect(result.current.delivery?.state).toBe('unknown'))
  const send = vi.fn().mockResolvedValue(true)
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(send).not.toHaveBeenCalled()
  vi.mocked(bridge.consume).mockResolvedValue({ count: 1, items: rows, delivery: { ...delivery, state: 'prepared' } })
  await act(async () => { await result.current.recoverDelivery(send, 'resume') })
  expect(bridge.consume).toHaveBeenLastCalledWith({ sessionId: MESSAGE_ID, deliveryId: DELIVERY_ID, action: 'resume' })
  expect(send).toHaveBeenCalledExactlyOnceWith('verify this batch', DELIVERY_ID)
})

it('does not send a resolved old-session delivery into a newly selected session', async () => {
  const bridge = queue(), rows = [item(1, 'old session input')]
  let resolve!: (value: Awaited<ReturnType<RunQueueBridge['consume']>>) => void
  vi.mocked(bridge.consume).mockImplementation(() => new Promise(r => { resolve = r }))
  const { result, rerender } = renderHook(({ id }) => useInputQueue(id), { initialProps: { id: MESSAGE_ID } })
  const send = vi.fn()
  let pending!: Promise<void>
  act(() => { pending = result.current.flushAfterStream(send) })
  rerender({ id: DELIVERY_ID })
  await act(async () => { resolve({ count: 1, items: rows, delivery: { id: DELIVERY_ID, state: 'prepared', messageIds: [MESSAGE_ID], items: rows } }); await pending })
  expect(send).not.toHaveBeenCalled()
})

it('ignores a stale A projection after selecting A then B then A', async () => {
  const bridge = queue()
  let resolve!: (value: Awaited<ReturnType<RunQueueBridge['list']>>) => void
  vi.mocked(bridge.list).mockImplementationOnce(() => new Promise(r => { resolve = r })).mockResolvedValue({ items: [item(2, 'fresh A')] })
  const { result, rerender } = renderHook(({ id }) => useInputQueue(id), { initialProps: { id: MESSAGE_ID } })
  rerender({ id: DELIVERY_ID }); rerender({ id: MESSAGE_ID })
  await waitFor(() => expect(result.current.items[0]?.text).toBe('fresh A'))
  await act(async () => { resolve({ items: [item(1, 'stale A')] }) })
  expect(result.current.items[0]?.text).toBe('fresh A')
})

it('does not apply a late withdrawal failure to another selected session', async () => {
  const bridge = queue()
  let reject!: (error: unknown) => void
  vi.mocked(bridge.withdraw).mockImplementationOnce(() => new Promise((_r, fail) => { reject = fail }))
  const { result, rerender } = renderHook(({ id }) => useInputQueue(id), { initialProps: { id: MESSAGE_ID } })
  let pending!: Promise<void>
  act(() => { pending = result.current.withdraw(MESSAGE_ID) })
  rerender({ id: DELIVERY_ID })
  vi.mocked(bridge.input).mockRejectedValue(new BridgeClientError('full', 'M10-QI-005', false, 'renderer'))
  await act(async () => { await result.current.enqueue('new session input') })
  await act(async () => { reject(new Error('old failure')); await pending })
  expect(result.current.notice).toContain('队列已满')
})

it('keeps the new delivery gate when an older A operation settles after A B A', async () => {
  const bridge = queue()
  const resolvers: Array<(r: Awaited<ReturnType<RunQueueBridge['consume']>>) => void> = []
  vi.mocked(bridge.consume).mockImplementation(() => new Promise(r => { resolvers.push(r) }))
  const { result, rerender } = renderHook(({ id }) => useInputQueue(id), { initialProps: { id: MESSAGE_ID } })
  const send = vi.fn()
  let old!: Promise<void>, current!: Promise<void>
  act(() => { old = result.current.flushAfterStream(send) })
  rerender({ id: DELIVERY_ID }); rerender({ id: MESSAGE_ID })
  act(() => { current = result.current.flushAfterStream(send) })
  await act(async () => { resolvers[0]({ count: 0, items: [] }); await old })
  await act(async () => { await result.current.flushAfterStream(send) })
  expect(bridge.consume).toHaveBeenCalledTimes(2)
  await act(async () => { resolvers[1]({ count: 0, items: [] }); await current })
  expect(send).not.toHaveBeenCalled()
})

it('lets the user cancel a prepared batch without trying to send it', async () => {
  const bridge = queue(), rows = [item(1, 'first retained item'), item(2, 'second retained item')]
  const delivery = { id: DELIVERY_ID, state: 'prepared' as const, messageIds: [MESSAGE_ID], items: rows }
  vi.mocked(bridge.list).mockResolvedValue({ items: [], delivery })
  vi.mocked(bridge.consume).mockResolvedValue({ count: 2, items: rows, delivery: { ...delivery, state: 'confirmed' } })
  const { result } = renderHook(() => useInputQueue(MESSAGE_ID))
  await waitFor(() => expect(result.current.delivery?.id).toBe(DELIVERY_ID))
  const send = vi.fn()
  await act(async () => { await result.current.recoverDelivery(send, 'dismiss') })
  expect(bridge.consume).toHaveBeenCalledExactlyOnceWith({ sessionId: MESSAGE_ID, deliveryId: DELIVERY_ID, action: 'dismiss' })
  expect(send).not.toHaveBeenCalled()
})

it('does not start a saved delivery after the hook has unmounted', async () => {
  const bridge = queue(), rows = [item(1, 'retained on server')]
  let resolve!: (r: Awaited<ReturnType<RunQueueBridge['consume']>>) => void
  vi.mocked(bridge.consume).mockImplementation(() => new Promise(r => { resolve = r }))
  const { result, unmount } = renderHook(() => useInputQueue(MESSAGE_ID))
  const send = vi.fn()
  let pending!: Promise<void>
  act(() => { pending = result.current.flushAfterStream(send) })
  unmount()
  await act(async () => { resolve({ count: 1, items: rows, delivery: { id: DELIVERY_ID, state: 'prepared', messageIds: [MESSAGE_ID], items: rows } }); await pending })
  expect(send).not.toHaveBeenCalled()
})
