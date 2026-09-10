import {cleanup, render, screen, waitFor} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import type {ChatUsageBridge} from '../bridge/client'
import {SessionUsageBar} from './SessionUsageBar'

afterEach(cleanup)

it('keeps the last live usage after persist while the ledger reloads', async () => {
  const get = vi.fn().mockResolvedValue({
    sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
    inputTokens: 40, outputTokens: 8, attempts: [{callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported', inputTokens: 40, outputTokens: 8}],
  })
  const usageApi = {get} as unknown as ChatUsageBridge
  const view = render(<SessionUsageBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" zh usage={{inputTokens: 40, outputTokens: 8, totalTokens: 48}} usageApi={usageApi} />)
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  view.rerender(<SessionUsageBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" zh usage={{inputTokens: 40, outputTokens: 8, totalTokens: 48}} usageApi={usageApi} />)
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('输入 40'))
  expect(screen.getByRole('status').textContent).not.toContain('%')
})

it('reloads the ledger when live usage is cleared and never invents a savings percent', async () => {
  const get = vi.fn()
    .mockResolvedValueOnce({
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported', inputTokens: 40, outputTokens: 8}],
    })
    .mockResolvedValueOnce({
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported', inputTokens: 40, outputTokens: 8}],
    })
  const usageApi = {get} as unknown as ChatUsageBridge
  const view = render(<SessionUsageBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" zh usage={{inputTokens: 40, outputTokens: 8, totalTokens: 48}} usageApi={usageApi} />)
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  view.rerender(<SessionUsageBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" zh usageApi={usageApi} />)
  await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
  expect(screen.getByRole('status').textContent).toContain('输入 40')
  expect(screen.getByRole('status').textContent).not.toContain('%')
})
