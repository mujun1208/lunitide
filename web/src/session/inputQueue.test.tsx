// M10 queued-input hook and strip coverage: idempotent enqueue projection,
// queue-full notice mapping, withdraw refresh, and flushAfterStream replay
// (single passthrough or multi-item merge) per FR-28/FR-34.
import { act, cleanup, render, renderHook, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, runQueueBridge, type RunQueueBridge } from '../bridge/client'
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

afterEach(() => { cleanup(); vi.clearAllMocks() })

const queue = () => vi.mocked(runQueueBridge)

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
  expect(result.current.notice).toBe('')
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
  vi.mocked(bridge.consume).mockResolvedValue({ count: 2, items: [{ queuedId: '01ARZ3NDEKTSV4RRFFQ69G5FA01', seq: 1, text: 'alpha', status: 'injected' as const, mark: 'turn_boundary' as const, createdAt: '2025-01-01T00:00:00Z' }, { queuedId: '01ARZ3NDEKTSV4RRFFQ69G5FA02', seq: 2, text: 'beta', status: 'injected' as const, mark: 'turn_boundary' as const, createdAt: '2025-01-01T00:00:00Z' }] })
  const { result } = renderHook(() => useInputQueue('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  const send = vi.fn()
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
