import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryNominationQueue } from './MemoryNominationQueue'

afterEach(cleanup)

const item = {
  nominationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  nominator: 'compaction-flush',
  reason: '压缩前整理，确认后才进长期记忆',
  state: 'nominated' as const,
  content: '默认用中文回答',
  scopeId: 'learning',
  confirmationToken: 'a'.repeat(64),
  createdAt: '2026-09-19T00:00:00Z',
}

it('lists pending nominations and confirms without inventing a second contract', async () => {
  const list = vi.fn()
    .mockResolvedValueOnce({ items: [item] })
    .mockResolvedValueOnce({ items: [] })
  const confirmCandidate = vi.fn().mockResolvedValue({ candidateId: item.candidateId, state: 'confirmed' })
  const withdraw = vi.fn()
  render(
    <MemoryNominationQueue
      nominations={{ nominate: vi.fn(), list, withdraw }}
      memory={{ confirmCandidate }}
    />,
  )
  expect(await screen.findByText('默认用中文回答')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '确认' }))
  await waitFor(() => expect(confirmCandidate).toHaveBeenCalledWith(expect.objectContaining({
    candidateId: item.candidateId,
    confirmationToken: item.confirmationToken,
    action: 'confirm',
  })))
  expect(withdraw).not.toHaveBeenCalled()
  await waitFor(() => expect(screen.getByText('当前没有待确认提名。')).toBeInTheDocument())
})

it('withdraws a nomination without confirming it', async () => {
  const list = vi.fn()
    .mockResolvedValueOnce({ items: [item] })
    .mockResolvedValueOnce({ items: [] })
  const confirmCandidate = vi.fn()
  const withdraw = vi.fn().mockResolvedValue({ nominationId: item.nominationId, state: 'withdrawn' })
  render(
    <MemoryNominationQueue
      nominations={{ nominate: vi.fn(), list, withdraw }}
      memory={{ confirmCandidate }}
    />,
  )
  expect(await screen.findByText('默认用中文回答')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '撤回' }))
  await waitFor(() => expect(withdraw).toHaveBeenCalledWith({ nominationId: item.nominationId, actor: 'settings' }))
  expect(confirmCandidate).not.toHaveBeenCalled()
})
