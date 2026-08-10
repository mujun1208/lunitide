import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MemoryBridge } from '../bridge/client'
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
