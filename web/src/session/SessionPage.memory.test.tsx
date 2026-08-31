import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatBridge, FeedbackBridge, MemoryBridge, MessageBridge, ProviderBridge, SessionBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

afterEach(() => {
  cleanup()
  resetLiveChatForTests()
})

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const S = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const C = '01ARZ3NDEKTSV4RRFFQ69G5FAC'
const NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = { id: P, name: 'Memory', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const session: SessionDTO = { id: S, projectId: P, title: '文字对话', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
  name: 'Ready',
  protocol: 'openai_compatible',
  baseUrl: 'https://example.test',
  models: [{ modelId: 'model', displayName: 'Model', isDefault: true }],
  status: 'enabled',
  credentialState: 'configured',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}

it('confirms a pending preference on the session without opening the memory page', async () => {
  const confirmCandidate = vi.fn().mockResolvedValue({ candidateId: C, state: 'confirmed' })
  const feedback = {
    record: vi.fn(),
    candidates: vi.fn().mockResolvedValue({
      items: [{ candidateId: C, content: '以后回答默认用中文，封面用深色', scopeId: 'learning', confirmationToken: 'tok', createdAt: NOW, expiresAt: NOW }],
    }),
  } as unknown as FeedbackBridge
  const memory = {
    get: vi.fn(),
    list: vi.fn(),
    create: vi.fn(),
    search: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    confirmCandidate,
  } as unknown as MemoryBridge
  render(
    <SessionPage
      project={project}
      bridge={{ list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() } as SessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      feedback={feedback}
      memory={memory}
    />,
  )
  const banner = await screen.findByRole('status', { name: '待确认偏好' })
  expect(banner).toHaveTextContent('确认后才进长期记忆')
  expect(banner).toHaveTextContent('默认用中文')
  fireEvent.click(screen.getByRole('button', { name: '确认沉淀' }))
  await waitFor(() => expect(confirmCandidate).toHaveBeenCalledWith(expect.objectContaining({
    candidateId: C,
    confirmationToken: 'tok',
    action: 'confirm',
  })))
  await waitFor(() => expect(screen.queryByRole('status', { name: '待确认偏好' })).not.toBeInTheDocument())
})

it('shows the confirm banner after a turn completes even if the first inbox fetch is empty', async () => {
  const C2 = '01ARZ3NDEKTSV4RRFFQ69G5FAD'
  let onEvent: ((event: { v: string; kind: string; id: string; streamId: string; sequence: number; type: string; completed?: { memorySummary?: string } }) => void) | undefined
  const candidates = vi.fn()
    .mockResolvedValueOnce({ items: [] })
    .mockResolvedValueOnce({ items: [] })
    .mockResolvedValue({
      items: [{ candidateId: C2, content: '以后回答默认用中文，封面用深色', scopeId: 'learning', confirmationToken: 'tok', createdAt: NOW, expiresAt: NOW }],
    })
  const start = vi.fn().mockImplementation(async (_payload: unknown, onStreamEvent: typeof onEvent) => {
    onEvent = onStreamEvent
    return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() }
  })
  render(
    <SessionPage
      project={project}
      bridge={{ list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() } as SessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      chat={{ start, approve: vi.fn(), dispose: vi.fn() } as unknown as ChatBridge}
      feedback={{ record: vi.fn(), candidates } as unknown as FeedbackBridge}
      memory={{ confirmCandidate: vi.fn() } as unknown as MemoryBridge}
    />,
  )
  await screen.findByLabelText('向月汐提问，或描述你想完成的任务…')
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '以后回答请默认用中文' } })
  fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => {
    onEvent?.({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', sequence: 1, type: 'completed', completed: { memorySummary: '注入记忆：任务 1' } })
  })
  expect(await screen.findByRole('status', { name: '待确认偏好' }, { timeout: 2000 })).toHaveTextContent('默认用中文')
  expect(candidates.mock.calls.length).toBeGreaterThanOrEqual(2)
})
