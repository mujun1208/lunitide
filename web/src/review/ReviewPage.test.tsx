import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ReviewBridge } from '../bridge/client'
import type { ReviewDTO } from '../generated/bridge'
import { ReviewPage } from './ReviewPage'

afterEach(cleanup)
const now = '2025-01-01T00:00:00Z'
const PLAN_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const review: ReviewDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', planId: PLAN_ID, actionType: 'plan.activate',
  actionDigest: 'abcdef0123456789', inputDigest: '1234567890abcdef', stateDigest: 'fedcba9876543210',
  policyVersion: 1, riskLevel: 'medium', status: 'pending', reviewerNote: '', createdAt: now,
}
const api = (o: Partial<ReviewBridge> = {}): ReviewBridge => ({
  list: vi.fn().mockResolvedValue({ items: [] }), approve: vi.fn().mockResolvedValue({ approved: true }),
  reject: vi.fn().mockResolvedValue({ rejected: true }), ...o,
})

it('renders empty state initially', () => {
  const bridge = api()
  render(<ReviewPage bridge={bridge} />)
  expect(screen.getByText('暂无审批记录')).toBeInTheDocument()
})

it('loads reviews by planId', async () => {
  const list = vi.fn().mockResolvedValue({ items: [review] }), bridge = api({ list })
  render(<ReviewPage bridge={bridge} />)
  fireEvent.change(screen.getByLabelText('计划 ID'), { target: { value: PLAN_ID } })
  fireEvent.click(screen.getByRole('button', { name: '查询审批' }))
  await waitFor(() => expect(list).toHaveBeenCalledWith({ planId: PLAN_ID }))
  expect(await screen.findByText('plan.activate')).toBeInTheDocument()
})

it('approves a pending review', async () => {
  const approve = vi.fn().mockResolvedValue({ approved: true })
  const bridge = api({ list: vi.fn().mockResolvedValue({ items: [review] }), approve })
  render(<ReviewPage bridge={bridge} />)
  fireEvent.change(screen.getByLabelText('计划 ID'), { target: { value: PLAN_ID } })
  fireEvent.click(screen.getByRole('button', { name: '查询审批' }))
  await screen.findByText('plan.activate')
  fireEvent.click(screen.getByRole('button', { name: '批准' }))
  await waitFor(() => expect(approve).toHaveBeenCalledOnce())
  expect(approve.mock.calls[0][0]).toMatchObject({ reviewId: review.id })
})
