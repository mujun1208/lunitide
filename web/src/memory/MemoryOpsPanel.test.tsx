import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MemoryOpsBridge } from '../bridge/client'
import { MemoryOpsPanel } from './MemoryOpsPanel'

afterEach(cleanup)

const F = '01ARZ3NDEKTSV4RRFFQ69G5FAV', now = '2025-01-01T00:00:00Z'

const opsApi = (o: Partial<MemoryOpsBridge> = {}): MemoryOpsBridge => ({
  stats: vi.fn().mockResolvedValue({
    factsByState: { active: 3 }, factsBySensitivity: { private: 3 },
    candidatesByState: { pending: 1 }, growthByStatus: { observing: 2 },
    tracesTotal: 5, tracesLast7Days: 2, memoriesTotal: 4,
  }),
  listFacts: vi.fn().mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 }),
  flagFact: vi.fn().mockResolvedValue({ factId: F, flag: 'pinned', on: true }),
  listTraces: vi.fn().mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 }),
  listGrowth: vi.fn().mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 }),
  decideGrowth: vi.fn().mockResolvedValue({ factId: F, decision: 'promoted' }),
  getSettings: vi.fn().mockResolvedValue({ subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14, updatedAt: now }),
  updateSettings: vi.fn().mockResolvedValue({ subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14, updatedAt: now }),
  export: vi.fn().mockResolvedValue({ facts: [], leaves: [], candidates: [], traces: [], growth: [], flags: [], settings: [] }),
  purge: vi.fn().mockResolvedValue({ factsTombstoned: 3, candidates: 1, growthRows: 2, flags: 1, traces: 5, memories: 4 }),
  ...o,
})

const fact = {
  factId: F, scopeId: 'local', version: 2, sensitivity: 'private' as const,
  state: 'active' as const, createdAt: now, pinned: false, hidden: false, note: '',
}

const growth = {
  factId: F, scopeId: 'local', status: 'observing' as const, referenceCount: 1,
  lastReferencedAt: null, reviewAt: now, decidedAt: null, createdAt: now,
}

it('renders the statistics strip from memory.stats', async () => {
  render(<MemoryOpsPanel ops={opsApi()} />)
  expect(await screen.findByText('3')).toBeInTheDocument()
  expect(screen.getByText('生效事实')).toBeInTheDocument()
  expect(screen.getByText('待确认候选')).toBeInTheDocument()
  expect(screen.getByText('成长箱观察')).toBeInTheDocument()
  expect(screen.getByText('召回记录')).toBeInTheDocument()
  expect(screen.getByText('四层记忆')).toBeInTheDocument()
})

it('lists facts and toggles the pinned flag', async () => {
  const flagFact = vi.fn().mockResolvedValue({ factId: F, flag: 'pinned', on: true })
  const ops = opsApi({
    listFacts: vi.fn().mockResolvedValue({ items: [{ ...fact }], total: 1, limit: 20, offset: 0 }),
    flagFact,
  })
  render(<MemoryOpsPanel ops={ops} />)
  expect(await screen.findByTestId('memory-fact-row')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '置顶' }))
  await waitFor(() => expect(flagFact).toHaveBeenCalledOnce())
  expect(flagFact.mock.calls[0][0]).toMatchObject({ factId: F, flag: 'pinned', on: true })
})

it('toggles the hidden flag on a fact', async () => {
  const flagFact = vi.fn().mockResolvedValue({ factId: F, flag: 'hidden', on: true })
  const ops = opsApi({
    listFacts: vi.fn().mockResolvedValue({ items: [{ ...fact }], total: 1, limit: 20, offset: 0 }),
    flagFact,
  })
  render(<MemoryOpsPanel ops={ops} />)
  await screen.findByTestId('memory-fact-row')
  fireEvent.click(screen.getByRole('button', { name: '隐藏' }))
  await waitFor(() => expect(flagFact).toHaveBeenCalledWith(expect.objectContaining({ flag: 'hidden', on: true })))
})

it('filters facts by state through the select', async () => {
  const listFacts = vi.fn().mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 })
  render(<MemoryOpsPanel ops={opsApi({ listFacts })} />)
  await screen.findByText('暂无事实')
  fireEvent.change(screen.getByLabelText('事实状态筛选'), { target: { value: 'active' } })
  await waitFor(() => expect(listFacts).toHaveBeenCalledWith(expect.objectContaining({ state: 'active' })))
})

it('decides an observing growth entry as promoted', async () => {
  const decideGrowth = vi.fn().mockResolvedValue({ factId: F, decision: 'promoted' })
  const ops = opsApi({
    listGrowth: vi.fn().mockResolvedValue({ items: [{ ...growth }], total: 1, limit: 20, offset: 0 }),
    decideGrowth,
  })
  render(<MemoryOpsPanel ops={ops} />)
  expect(await screen.findByTestId('memory-growth-row')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '转正' }))
  await waitFor(() => expect(decideGrowth).toHaveBeenCalledOnce())
  expect(decideGrowth.mock.calls[0][0]).toMatchObject({ factId: F, decision: 'promoted' })
})

it('drops an observing growth entry', async () => {
  const decideGrowth = vi.fn().mockResolvedValue({ factId: F, decision: 'dropped' })
  const ops = opsApi({
    listGrowth: vi.fn().mockResolvedValue({ items: [{ ...growth }], total: 1, limit: 20, offset: 0 }),
    decideGrowth,
  })
  render(<MemoryOpsPanel ops={ops} />)
  await screen.findByTestId('memory-growth-row')
  fireEvent.click(screen.getByRole('button', { name: '放弃' }))
  await waitFor(() => expect(decideGrowth).toHaveBeenCalledWith({ factId: F, decision: 'dropped' }))
})

it('hides decide buttons for non-observing growth entries', async () => {
  const ops = opsApi({
    listGrowth: vi.fn().mockResolvedValue({ items: [{ ...growth, status: 'promoted' as const, decidedAt: now }], total: 1, limit: 20, offset: 0 }),
  })
  render(<MemoryOpsPanel ops={ops} />)
  expect(await screen.findByText('已转正')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '转正' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '放弃' })).not.toBeInTheDocument()
})

it('renders recall traces with hits and reasons', async () => {
  const ops = opsApi({
    listTraces: vi.fn().mockResolvedValue({
      items: [{ id: 'tr1', queryDigest: 'sha256:abcd', hits: '2', reasons: 'scope:local', redactions: '1', createdAt: now }],
      total: 1, limit: 20, offset: 0,
    }),
  })
  render(<MemoryOpsPanel ops={ops} />)
  expect(await screen.findByText('sha256:abcd')).toBeInTheDocument()
  expect(screen.getByText(/命中 2/)).toBeInTheDocument()
})

it('saves memory settings via memory.settings.update', async () => {
  const updateSettings = vi.fn().mockResolvedValue({ subjectId: 'local-user', memoryEnabled: false, autoNominate: true, growthDays: 30, updatedAt: now })
  render(<MemoryOpsPanel ops={opsApi({ updateSettings })} />)
  await waitFor(() => expect(screen.getByLabelText('启用记忆沉淀')).toBeChecked())
  fireEvent.click(screen.getByLabelText('启用记忆沉淀'))
  fireEvent.click(screen.getByLabelText('自动提名候选'))
  fireEvent.change(screen.getByLabelText(/成长观察期/), { target: { value: '30' } })
  fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledOnce())
  expect(updateSettings.mock.calls[0][0]).toMatchObject({ subjectId: 'local-user', memoryEnabled: false, autoNominate: true, growthDays: 30 })
  expect(await screen.findByText('记忆设置已保存')).toBeInTheDocument()
})

it('exports the memory bundle as a JSON download', async () => {
  if (!('createObjectURL' in URL)) Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(), writable: true })
  if (!('revokeObjectURL' in URL)) Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), writable: true })
  const createObjectURL = vi.mocked(URL.createObjectURL).mockReturnValue('blob:mock')
  const revokeObjectURL = vi.mocked(URL.revokeObjectURL).mockReturnValue()
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  render(<MemoryOpsPanel ops={opsApi()} />)
  await screen.findByText('暂无事实')
  fireEvent.click(screen.getByRole('button', { name: '导出记忆数据' }))
  await waitFor(() => expect(createObjectURL).toHaveBeenCalledOnce())
  expect(click).toHaveBeenCalledOnce()
  expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock')
  expect(await screen.findByText('记忆数据已导出')).toBeInTheDocument()
})

it('purges all memory data after double confirmation', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const ops = opsApi()
  render(<MemoryOpsPanel ops={ops} />)
  await screen.findByText('暂无事实')
  fireEvent.click(screen.getByRole('button', { name: '一键清除全部记忆' }))
  await waitFor(() => expect(ops.purge).toHaveBeenCalledOnce())
  expect(confirm).toHaveBeenCalledTimes(2)
  expect(await screen.findByText(/已清除：封存事实 3 条/)).toBeInTheDocument()
  confirm.mockRestore()
})

it('does not purge when the first confirmation is rejected', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  const ops = opsApi()
  render(<MemoryOpsPanel ops={ops} />)
  await screen.findByText('暂无事实')
  fireEvent.click(screen.getByRole('button', { name: '一键清除全部记忆' }))
  expect(confirm).toHaveBeenCalledOnce()
  expect(ops.purge).not.toHaveBeenCalled()
  confirm.mockRestore()
})

it('pages the facts list with the pager controls', async () => {
  const listFacts = vi.fn().mockResolvedValue({ items: [], total: 25, limit: 20, offset: 0 })
  render(<MemoryOpsPanel ops={opsApi({ listFacts })} />)
  await screen.findByText(/事实：共 25 条/)
  fireEvent.click(screen.getAllByRole('button', { name: '下一页' })[0]!)
  await waitFor(() => expect(listFacts).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 20 })))
})
