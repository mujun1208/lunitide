import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { FeedbackBridge, MemoryBridge, NominationBridge } from '../bridge/client'
import type { MemoryDTO } from '../generated/bridge'
import { MemoryPage } from './MemoryPage'

afterEach(cleanup)
const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV', now = '2025-01-01T00:00:00Z'
const memory: MemoryDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', projectId: P, layer: 'semantic', scope: 'project',
  key: '架构决策', content: '采用 React 架构', confidence: 0.9, accessCount: 3, createdAt: now, updatedAt: now,
}
const api = (o: Partial<MemoryBridge> = {}): MemoryBridge => ({
  get: vi.fn(), list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn().mockResolvedValue(memory),
  search: vi.fn().mockResolvedValue({ items: [] }), update: vi.fn().mockResolvedValue({ updated: true }),
  delete: vi.fn().mockResolvedValue({ deleted: true }), ...o,
})
const feedbackApi = (o: Partial<FeedbackBridge> = {}): FeedbackBridge => ({
  record: vi.fn(), candidates: vi.fn().mockResolvedValue({ items: [] }), ...o,
})
const nominationApi = (o: Partial<NominationBridge> = {}): NominationBridge => ({
  nominate: vi.fn(), withdraw: vi.fn().mockResolvedValue({ nominationId: P, state: 'withdrawn' }),
  list: vi.fn().mockResolvedValue({ items: [] }), ...o,
})

it('renders empty state and loads memories for the project', async () => {
  const bridge = api()
  render(<MemoryPage projectId={P} bridge={bridge} />)
  expect(await screen.findByText('暂无记忆')).toBeInTheDocument()
  expect(bridge.list).toHaveBeenCalledWith({ projectId: P })
})

it('renders memory items from bridge.list', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [memory] }) })
  render(<MemoryPage projectId={P} bridge={bridge} />)
  expect(await screen.findByText('架构决策')).toBeInTheDocument()
})

it('creates a memory via the create form', async () => {
  const create = vi.fn().mockResolvedValue(memory), bridge = api({ create })
  render(<MemoryPage projectId={P} bridge={bridge} />)
  await screen.findByText('暂无记忆')
  fireEvent.click(screen.getByText('新建记忆'))
  fireEvent.change(screen.getByLabelText('键名'), { target: { value: '新决策' } })
  fireEvent.change(screen.getByLabelText('内容'), { target: { value: '采用 TypeScript' } })
  fireEvent.click(screen.getByRole('button', { name: '创建记忆' }))
  await waitFor(() => expect(create).toHaveBeenCalledOnce())
  expect(create.mock.calls[0][0]).toMatchObject({ projectId: P, key: '新决策', content: '采用 TypeScript' })
})

const pendingItem = {
  candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', content: '回答默认使用中文', scopeId: 'local',
  confirmationToken: 'a'.repeat(64), createdAt: now, expiresAt: now,
}

it('confirms a pending preference candidate and removes it from the list', async () => {
  const confirmCandidate = vi.fn().mockResolvedValue({ candidateId: pendingItem.candidateId, state: 'confirmed' })
  const bridge = api({ confirmCandidate })
  const feedback = feedbackApi({ candidates: vi.fn().mockResolvedValue({ items: [pendingItem] }) })
  render(<MemoryPage projectId={P} bridge={bridge} feedback={feedback} />)
  expect(await screen.findByText('回答默认使用中文')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '确认沉淀' }))
  await waitFor(() => expect(confirmCandidate).toHaveBeenCalledOnce())
  expect(confirmCandidate.mock.calls[0][0]).toMatchObject({ candidateId: pendingItem.candidateId, action: 'confirm' })
  await waitFor(() => expect(screen.queryByText('回答默认使用中文')).not.toBeInTheDocument())
})

it('rejects a pending preference candidate', async () => {
  const confirmCandidate = vi.fn().mockResolvedValue({ candidateId: pendingItem.candidateId, state: 'rejected' })
  const bridge = api({ confirmCandidate })
  const feedback = feedbackApi({ candidates: vi.fn().mockResolvedValue({ items: [pendingItem] }) })
  render(<MemoryPage projectId={P} bridge={bridge} feedback={feedback} />)
  await screen.findByText('回答默认使用中文')
  fireEvent.click(screen.getByRole('button', { name: '拒绝' }))
  await waitFor(() => expect(confirmCandidate).toHaveBeenCalledWith(expect.objectContaining({ action: 'reject' })))
  await waitFor(() => expect(screen.queryByText('回答默认使用中文')).not.toBeInTheDocument())
})

it('hides the confirmation section when no candidates are pending', async () => {
  render(<MemoryPage projectId={P} bridge={api()} feedback={feedbackApi()} />)
  await screen.findByText('暂无记忆')
  expect(screen.queryByLabelText('偏好确认')).not.toBeInTheDocument()
})

const nomination = {
  nominationId: '01ARZ3NDEKTSV4RRFFQ69G5FAC', candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
  nominator: 'assistant', reason: '连续三次会话提及', state: 'nominated' as const,
  content: '偏好简洁回答', scopeId: 'local', confirmationToken: 'b'.repeat(64), createdAt: now,
}

it('shows nominated items in the inbox tab with the pending count', async () => {
  const nominations = nominationApi({ list: vi.fn().mockImplementation((p: { state: string }) =>
    Promise.resolve({ items: p.state === 'nominated' ? [nomination] : [] })) })
  render(<MemoryPage projectId={P} bridge={api()} feedback={feedbackApi()} nominations={nominations} />)
  await screen.findByRole('tab', { name: '提名收件箱（1）' })
  fireEvent.click(screen.getByRole('tab', { name: '提名收件箱（1）' }))
  expect(await screen.findByText('偏好简洁回答')).toBeInTheDocument()
  expect(screen.getByText(/连续三次会话提及/)).toBeInTheDocument()
})

it('confirms a nomination through the 0061 confirm path and reloads', async () => {
  const confirmCandidate = vi.fn().mockResolvedValue({ candidateId: nomination.candidateId, state: 'confirmed' })
  const list = vi.fn().mockImplementation((p: { state: string }) =>
    Promise.resolve({ items: p.state === 'nominated' ? [nomination] : [] }))
  const nominations = nominationApi({ list })
  render(<MemoryPage projectId={P} bridge={api({ confirmCandidate })} feedback={feedbackApi()} nominations={nominations} />)
  fireEvent.click(screen.getByRole('tab', { name: /提名收件箱/ }))
  await screen.findByText('偏好简洁回答')
  fireEvent.click(screen.getAllByRole('button', { name: '确认沉淀' })[0]!)
  await waitFor(() => expect(confirmCandidate).toHaveBeenCalledOnce())
  expect(confirmCandidate.mock.calls[0][0]).toMatchObject({ candidateId: nomination.candidateId, action: 'confirm' })
})

it('withdraws a nomination and moves it out of the inbox', async () => {
  const withdraw = vi.fn().mockResolvedValue({ nominationId: nomination.nominationId, state: 'withdrawn' })
  const list = vi.fn().mockImplementation((p: { state: string }) =>
    Promise.resolve({ items: p.state === 'nominated' ? [nomination] : [] }))
  const nominations = nominationApi({ list, withdraw })
  render(<MemoryPage projectId={P} bridge={api()} feedback={feedbackApi()} nominations={nominations} />)
  fireEvent.click(screen.getByRole('tab', { name: /提名收件箱/ }))
  await screen.findByText('偏好简洁回答')
  fireEvent.click(screen.getByRole('button', { name: '撤回提名' }))
  await waitFor(() => expect(withdraw).toHaveBeenCalledWith({ nominationId: nomination.nominationId }))
})

it('lists decided and withdrawn nominations in the history tab', async () => {
  const list = vi.fn().mockImplementation((p: { state: string }) => Promise.resolve({
    items: p.state === 'decided' ? [{ ...nomination, nominationId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', state: 'decided' as const, decidedAt: now }]
      : p.state === 'withdrawn' ? [{ ...nomination, nominationId: '01ARZ3NDEKTSV4RRFFQ69G5FAF', state: 'withdrawn' as const, decidedAt: now }] : [],
  }))
  render(<MemoryPage projectId={P} bridge={api()} feedback={feedbackApi()} nominations={nominationApi({ list })} />)
  fireEvent.click(screen.getByRole('tab', { name: '处理历史' }))
  expect(await screen.findByText(/已处理/)).toBeInTheDocument()
  expect(screen.getAllByText(/已撤回/).length).toBeGreaterThan(0)
})
