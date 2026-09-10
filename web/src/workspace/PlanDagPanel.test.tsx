import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { PlanBridge, ReviewBridge } from '../bridge/client'
import type { PlanDTO } from '../generated/bridge'
import { PlanDagPanel } from './PlanDagPanel'

afterEach(cleanup)

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const now = '2026-01-01T00:00:00Z'
const plan = { id: P, projectId: P, name: '计划', description: '', version: 1, status: 'draft', createdAt: now, updatedAt: now } as PlanDTO

it('does not show raw English plan list, detail or operation failures', async () => {
  render(<PlanDagPanel projectId={P} bridge={{ list: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as PlanBridge} reviews={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ReviewBridge} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('计划列表加载失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const listNodes = vi.fn().mockRejectedValue(new Error('Failed to fetch'))
  render(<PlanDagPanel projectId={P} bridge={{ list: vi.fn().mockResolvedValue({ items: [plan] }), listNodes, activate: vi.fn() } as unknown as PlanBridge} reviews={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ReviewBridge} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('加载计划失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  render(<PlanDagPanel projectId={P} bridge={{
    list: vi.fn().mockResolvedValue({ items: [plan] }),
    listNodes: vi.fn().mockResolvedValue({ items: [] }),
    activate: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
  } as unknown as PlanBridge} reviews={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ReviewBridge} />)
  fireEvent.click(await screen.findByRole('button', { name: '启动计划' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('操作失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
