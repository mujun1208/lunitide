import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { PlanBridge } from '../bridge/client'
import type { PlanDTO, PlanNodeDTO } from '../generated/bridge'
import { PlanPage } from './PlanPage'

afterEach(cleanup)
const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV', now = '2025-01-01T00:00:00Z'
const plan: PlanDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', projectId: P, name: '测试计划', description: '描述', version: 1, status: 'draft', createdAt: now, updatedAt: now }
const node: PlanNodeDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', planId: plan.id, name: '节点1', description: '节点描述', status: 'pending', riskLevel: 'low', workerRole: 'coder', sequence: 1, createdAt: now, updatedAt: now }
const api = (o: Partial<PlanBridge> = {}): PlanBridge => ({
  get: vi.fn(), list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn().mockResolvedValue({ plan }),
  activate: vi.fn(), complete: vi.fn(), pause: vi.fn(), resume: vi.fn(),
  listNodes: vi.fn().mockResolvedValue({ items: [] }), createNode: vi.fn().mockResolvedValue({ node }),
  startNode: vi.fn(), completeNode: vi.fn(), failNode: vi.fn(), ...o,
})

it('renders empty state and loads plans for the project', async () => {
  const bridge = api()
  render(<PlanPage projectId={P} bridge={bridge} />)
  expect(await screen.findByText('暂无计划')).toBeInTheDocument()
  expect(bridge.list).toHaveBeenCalledWith({ projectId: P })
})

it('renders plan items from bridge.list', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [plan] }) })
  render(<PlanPage projectId={P} bridge={bridge} />)
  expect(await screen.findByText('测试计划')).toBeInTheDocument()
})

it('creates a plan via the create form', async () => {
  const create = vi.fn().mockResolvedValue({ plan }), bridge = api({ create })
  render(<PlanPage projectId={P} bridge={bridge} />)
  await screen.findByText('暂无计划')
  fireEvent.click(screen.getByText('新建计划'))
  fireEvent.change(screen.getByLabelText('计划名称'), { target: { value: '新计划' } })
  fireEvent.change(screen.getByLabelText('计划描述'), { target: { value: '新描述' } })
  fireEvent.click(screen.getByRole('button', { name: '创建计划' }))
  await waitFor(() => expect(create).toHaveBeenCalledOnce())
  expect(create.mock.calls[0][0]).toEqual({ projectId: P, name: '新计划', description: '新描述' })
})

it('creates a node after selecting a plan', async () => {
  const createNode = vi.fn().mockResolvedValue({ node }), listNodes = vi.fn().mockResolvedValue({ items: [node] })
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [plan] }), listNodes, createNode })
  render(<PlanPage projectId={P} bridge={bridge} />)
  await screen.findByText('测试计划')
  fireEvent.click(screen.getByText('测试计划'))
  await screen.findByText(/节点1/)
  fireEvent.click(screen.getByText('新建节点'))
  fireEvent.change(screen.getByLabelText('节点名称'), { target: { value: '新节点' } })
  fireEvent.change(screen.getByLabelText('节点描述'), { target: { value: '节点说明' } })
  fireEvent.change(screen.getByLabelText('执行角色'), { target: { value: 'coder' } })
  fireEvent.click(screen.getByRole('button', { name: '创建节点' }))
  await waitFor(() => expect(createNode).toHaveBeenCalledOnce())
  expect(createNode.mock.calls[0][0]).toMatchObject({ planId: plan.id, name: '新节点', description: '节点说明', workerRole: 'coder' })
})
