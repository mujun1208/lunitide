import React from 'react'
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { MroWorkbenchPage } from './MroWorkbenchPage'
import {
  bindMroPage,
  mergeMroPage,
  readAllMroPages,
  useMroPagination,
  type MroPage,
  type MroPageRequest,
} from './pagination'

afterEach(cleanup)
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

it('continues utilization results through the reader without repeating the write', async () => {
  const dueList = vi
    .fn()
    .mockResolvedValueOnce({ items: [] })
    .mockResolvedValueOnce({
      items: [{ id: 'd1', kind: 'hours', state: 'ok', label: 'first due' }],
      nextCursor: 'due2',
    })
    .mockResolvedValueOnce({ items: [{ id: 'd2', kind: 'cycles', state: 'ok', label: 'last due' }] })
  const onRecordUtil = vi.fn().mockResolvedValue({ items: [], nextCursor: 'write-preview' })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled initialRail="due" dueList={dueList} onRecordUtil={onRecordUtil} />
    </LanguageProvider>,
  )
  await waitFor(() => expect(dueList).toHaveBeenCalledTimes(1))
  fireEvent.click(screen.getByRole('button', { name: '录利用率' }))
  fireEvent.change(screen.getByLabelText('范围 ID'), { target: { value: 'B-1234' } })
  fireEvent.change(screen.getByLabelText('小时'), { target: { value: '2' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  fireEvent.click(await screen.findByRole('button', { name: '继续读取到期' }))
  await screen.findByText('last due')
  expect(screen.getByText('first due')).toBeInTheDocument()
  expect(onRecordUtil).toHaveBeenCalledTimes(1)
  expect(dueList).toHaveBeenLastCalledWith({ cursor: 'due2' })
})

it('joins explicit parent fragments without losing long event notes or unequal dual lists', () => {
  const note = '部件🙂<&'.repeat(500)
  const first = { items: [{ id: 'c1', sn: 'SN', events: [{ note }] }], alternates: [{ pn: 'A' }], nextCursor: 'next' }
  const second = {
    items: [
      { sn: 'SN', id: 'c1', events: [{ note: note + '尾' }] },
      { id: 'c2', sn: 'SN2', events: [] },
    ],
    alternates: [],
    continuedFields: ['items'],
  }
  const joined = mergeMroPage<MroPage>(first, second)
  expect(joined.items).toEqual([{ id: 'c1', sn: 'SN', events: [{ note }, { note: note + '尾' }] }, second.items[1]])
  expect(joined.alternates).toEqual(first.alternates)
  expect(joined.nextCursor).toBeUndefined()
  expect(first.items[0].events).toHaveLength(1)
})

it.each([
  { items: [{ id: 'other', tails: ['B2'] }], continuedFields: ['items'] },
  { items: [{ id: 'lot', qty: 2, tails: ['B2'] }], continuedFields: ['items'] },
  { items: [], continuedFields: ['items'] },
  { items: [], continuedFields: ['unknown'] },
])('rejects malformed or changed continuation instead of mixing snapshots: %j', (page) => {
  expect(() => mergeMroPage<MroPage>({ items: [{ id: 'lot', tails: ['B1'] }] }, page)).toThrow()
})

it('ignores delayed next page after refresh and keeps rows on snapshot rejection', async () => {
  const late = deferred<MroPage>(),
    accept = vi.fn()
  const fetch = vi
    .fn()
    .mockResolvedValueOnce({ items: ['old'], nextCursor: 'p2' })
    .mockReturnValueOnce(late.promise)
    .mockResolvedValueOnce({ items: ['fresh'], nextCursor: 'new2' })
    .mockRejectedValueOnce(new Error('MRO_PAGE_CHANGED'))
  const { result } = renderHook(() => useMroPagination({ tools: bindMroPage(fetch, accept) }, 'org'))
  await act(async () => {
    await result.current.refresh('tools')
  })
  let pending!: Promise<void>
  act(() => {
    pending = result.current.next('tools')
  })
  await act(async () => {
    await result.current.refresh('tools')
  })
  await act(async () => {
    late.resolve({ items: ['obsolete'] })
    await pending
  })
  expect(accept).toHaveBeenCalledTimes(2)
  expect(result.current.states.tools.data?.items).toEqual(['fresh'])
  await act(async () => {
    await expect(result.current.next('tools')).rejects.toThrow('MRO_PAGE_CHANGED')
  })
  expect(result.current.states.tools.data?.items).toEqual(['fresh'])
  expect(result.current.states.tools.error).toBe('MRO_PAGE_CHANGED')
})

it('blocks duplicate reads and rejects a non-advancing cursor', async () => {
  const late = deferred<MroPage>(),
    accept = vi.fn()
  const fetch = vi
    .fn()
    .mockResolvedValueOnce({ items: [1], nextCursor: 'p2' })
    .mockReturnValueOnce(late.promise)
  const { result } = renderHook(() => useMroPagination({ tools: bindMroPage(fetch, accept) }, 'org'))
  await act(async () => {
    await result.current.refresh('tools')
  })
  let pending!: Promise<void>
  act(() => {
    pending = result.current.next('tools')
  })
  await act(async () => {
    await result.current.next('tools')
  })
  expect(fetch).toHaveBeenCalledTimes(2)
  await act(async () => {
    late.resolve({ items: [2], nextCursor: 'p2' })
    await expect(pending).rejects.toThrow('did not advance')
  })
  expect(accept).toHaveBeenCalledTimes(1)
  expect(result.current.states.tools.data?.items).toEqual([1])
})

it('ignores both A→B→A and unmounted late replies', async () => {
  const old = deferred<MroPage>(),
    abandoned = deferred<MroPage>(),
    accept = vi.fn()
  const fetch = vi
    .fn()
    .mockReturnValueOnce(old.promise)
    .mockResolvedValueOnce({ items: ['new A'] })
    .mockReturnValueOnce(abandoned.promise)
  const { result, rerender, unmount } = renderHook(
    ({ scope }) => useMroPagination({ tools: bindMroPage(fetch, accept) }, scope),
    { initialProps: { scope: 'A' } },
  )
  let pending!: Promise<void>
  act(() => {
    pending = result.current.refresh('tools')
  })
  rerender({ scope: 'B' })
  rerender({ scope: 'A' })
  await act(async () => {
    await result.current.refresh('tools')
    old.resolve({ items: ['old A'] })
    await pending
  })
  expect(accept).toHaveBeenCalledTimes(1)
  act(() => {
    pending = result.current.refresh('tools')
  })
  unmount()
  await act(async () => {
    abandoned.resolve({ items: ['unmounted'] })
    await pending
  })
  expect(accept).toHaveBeenCalledTimes(1)
})

it('reads a complete lot using only the original filtered reader and stops after leaving the view', async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce({ items: [{ id: 'lot', tails: ['B1'] }], nextCursor: 'tail2' })
    .mockResolvedValueOnce({ items: [{ id: 'lot', tails: ['B2'] }], continuedFields: ['items'] })
  const result = await readAllMroPages(fetch)
  expect(result.items).toEqual([{ id: 'lot', tails: ['B1', 'B2'] }])
  expect(fetch.mock.calls).toEqual([[undefined], [{ cursor: 'tail2' }]])
  await expect(readAllMroPages(fetch, () => false)).rejects.toThrow('view changed')
  expect(fetch).toHaveBeenCalledTimes(2)
})

it('makes remaining inventory and alternate records reachable in the workbench', async () => {
  const partsList = vi.fn(async (input?: MroPageRequest) =>
    input?.cursor
      ? {
          items: [{ pn: 'PN-200', qty: 2, source: 'local' }],
          alternates: [{ pnFrom: 'PN-100', pnTo: 'ALT-200', certOk: true, accepted: true }],
        }
      : { items: [{ pn: 'PN-100', qty: 1, source: 'local' }], alternates: [], nextCursor: 'stock2' },
  )
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled initialRail="parts" partsList={partsList} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '继续读取库存与替代件' }))
  await screen.findByText('PN-200')
  expect(screen.getByText('PN-100')).toBeInTheDocument()
  expect(partsList).toHaveBeenLastCalledWith({ cursor: 'stock2' })
  fireEvent.click(screen.getByRole('tab', { name: '替代件' }))
  expect(screen.getByText('ALT-200')).toBeInTheDocument()
})

it('never marks a constraint passed before every violations page has arrived', async () => {
  const constraintList = vi.fn(async (input?: MroPageRequest) =>
    input?.cursor
      ? { violations: [{ code: 'C7', detail: 'late missing source' }] }
      : { violations: [], nextCursor: 'check2' },
  )
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled initialRail="plan" constraintList={constraintList} />
    </LanguageProvider>,
  )
  fireEvent.click(screen.getByRole('tab', { name: '窗口与约束' }))
  const next = await screen.findByRole('button', { name: '继续读取排程检查' })
  expect(screen.queryByText('当前约束检查通过；发布时仍会重新检查。')).not.toBeInTheDocument()
  expect(screen.getByText('C7')).toHaveClass('is-warn')
  fireEvent.click(next)
  await screen.findByText('late missing source')
  expect(screen.queryByText('当前约束检查通过；发布时仍会重新检查。')).not.toBeInTheDocument()
})

it('retains rows and offers refresh when the data changes between pages', async () => {
  const toolList = vi
    .fn()
    .mockResolvedValueOnce({ items: [{ id: '1', toolNo: 'first tool' }], nextCursor: 'p2' })
    .mockRejectedValueOnce(new Error('列表已变化，请刷新'))
    .mockResolvedValueOnce({ items: [{ id: '2', toolNo: 'fresh tool' }] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled initialRail="tools" toolList={toolList} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '继续读取工具' }))
  await screen.findByText('列表已变化，请刷新')
  expect(screen.getByText('first tool')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '刷新工具' }))
  await screen.findByText('fresh tool')
  expect(screen.queryByText('first tool')).not.toBeInTheDocument()
  await waitFor(() => expect(toolList).toHaveBeenLastCalledWith(undefined))
})
