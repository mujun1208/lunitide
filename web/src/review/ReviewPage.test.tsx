import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ReviewBridge, PlanBridge } from '../bridge/client'
import type { ReviewDTO, PlanDTO } from '../generated/bridge'
import { ReviewPage } from './ReviewPage'

afterEach(cleanup)
const now = '2025-01-01T00:00:00Z'
const PROJECT_ID = '01ARZ3NDEKTSV4RRFFQ69G5FA0'
const PLAN_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const plan: PlanDTO = { id: PLAN_ID, projectId: PROJECT_ID, name: '测试计划', description: '', version: 1, status: 'active', createdAt: now, updatedAt: now }
const review: ReviewDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', planId: PLAN_ID, actionType: 'plan.activate',
  actionDigest: 'abcdef0123456789', inputDigest: '1234567890abcdef', stateDigest: 'fedcba9876543210',
  policyVersion: 1, riskLevel: 'medium', status: 'pending', reviewerNote: '', createdAt: now,
}
const reviewApi = (o: Partial<ReviewBridge> = {}): ReviewBridge => ({
  list: vi.fn().mockResolvedValue({ items: [] }), approve: vi.fn().mockResolvedValue({ approved: true }),
  reject: vi.fn().mockResolvedValue({ rejected: true }), ...o,
})
const planApi = (o: Partial<PlanBridge> = {}): PlanBridge => ({
  list: vi.fn().mockResolvedValue({ items: [] }), ...o,
}) as PlanBridge

it('renders empty state when no project selected', () => {
  const bridge = reviewApi()
  const plans = planApi()
  render(<ReviewPage projectId="" bridge={bridge} plans={plans} />)
  expect(screen.getByText('请选择计划')).toBeInTheDocument()
})

it('loads plans and reviews on plan selection', async () => {
  const list = vi.fn().mockResolvedValue({ items: [review] })
  const bridge = reviewApi({ list })
  const plans = planApi({ list: vi.fn().mockResolvedValue({ items: [plan] }) })
  render(<ReviewPage projectId={PROJECT_ID} bridge={bridge} plans={plans} />)
  await waitFor(() => expect(plans.list).toHaveBeenCalledWith({ projectId: PROJECT_ID }))
  const select = await screen.findByRole('combobox')
  fireEvent.change(select, { target: { value: PLAN_ID } })
  await waitFor(() => expect(list).toHaveBeenCalledWith({ planId: PLAN_ID }))
  expect(await screen.findByText('plan.activate')).toBeInTheDocument()
})

it('approves a pending review', async () => {
  const approve = vi.fn().mockResolvedValue({ approved: true })
  const bridge = reviewApi({ list: vi.fn().mockResolvedValue({ items: [review] }), approve })
  const plans = planApi({ list: vi.fn().mockResolvedValue({ items: [plan] }) })
  render(<ReviewPage projectId={PROJECT_ID} bridge={bridge} plans={plans} />)
  const select = await screen.findByRole('combobox')
  fireEvent.change(select, { target: { value: PLAN_ID } })
  await screen.findByText('plan.activate')
  fireEvent.click(screen.getByRole('button', { name: '批准' }))
  await waitFor(() => expect(approve).toHaveBeenCalledOnce())
  expect(approve.mock.calls[0][0]).toMatchObject({ reviewId: review.id })
})
