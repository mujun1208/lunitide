import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type ChatBridge, type FeedbackBridge, type MemoryBridge, type MemoryOpsBridge, type MessageBridge, type ProviderBridge, type SessionBridge } from '../bridge/client'
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
const M = '01ARZ3NDEKTSV4RRFFQ69G5FAD'
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
  credentialBackupCount: 0,
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}

const userMessage = {
  id: M,
  sessionId: S,
  role: 'user' as const,
  status: 'completed' as const,
  sequence: 1,
  createdAt: NOW,
  text: '以后回答请默认用中文',
}

function sessionBridge(): SessionBridge {
  return { list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() } as SessionBridge
}

function listedMessages(): MessageBridge {
  return {
    list: vi.fn().mockResolvedValue({ items: [userMessage], hasMore: false, nextCursor: null, snapshotSequence: 1 }),
    append: vi.fn(),
  } as unknown as MessageBridge
}

function memoryOps(captureMode: 'auto' | 'manual' | 'off' = 'auto'): MemoryOpsBridge {
  return {
    getSettings: vi.fn().mockResolvedValue({
      subjectId: 'local-user',
      memoryEnabled: captureMode !== 'off',
      autoNominate: true,
      growthDays: 14,
      updatedAt: NOW,
      version: 'v1',
      captureMode,
      revision: 1,
      personalMemoryEnabled: true,
      projectMemoryEnabled: true,
    }),
  } as unknown as MemoryOpsBridge
}

it('does not poll memory candidates or show a preference banner', async () => {
  const candidates = vi.fn().mockResolvedValue({
    items: [{ candidateId: C, content: '以后回答默认用中文，封面用深色', scopeId: 'learning', confirmationToken: 'tok', createdAt: NOW, expiresAt: NOW }],
  })
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge()}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      feedback={{ record: vi.fn(), candidates } as unknown as FeedbackBridge}
      memory={{ confirmCandidate: vi.fn() } as unknown as MemoryBridge}
      memoryOps={memoryOps()}
    />,
  )
  await screen.findByText('还没有消息')
  expect(candidates).not.toHaveBeenCalled()
  expect(screen.queryByRole('status', { name: '待确认偏好' })).not.toBeInTheDocument()
})

it('TestMemoryCaptureToast: saves a persisted user message as memory with sourceRef only', async () => {
  const itemCreate = vi.fn().mockResolvedValue({
    item: { factId: C, version: 1, revision: 1, scopeKind: 'user', kind: 'preference', text: userMessage.text, forgotten: false, updatedAt: NOW },
    databaseRevision: 1,
    undoOperationId: C,
    undoExpiresAt: NOW,
  })
  const captureUndo = vi.fn().mockResolvedValue({ undone: true, forgottenFactIds: [C], databaseRevision: 2 })
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge()}
      messages={listedMessages()}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      memory={{ itemCreate, captureUndo } as unknown as MemoryBridge}
      memoryOps={memoryOps('manual')}
    />,
  )
  fireEvent.click(await screen.findByRole('button', { name: '保存为记忆' }))
  await waitFor(() => expect(itemCreate).toHaveBeenCalled())
  const [payload, options] = itemCreate.mock.calls[0]
  expect(payload).toEqual(expect.objectContaining({
    scopeKind: 'user',
    sourceRef: { messageId: M },
  }))
  expect(payload).not.toHaveProperty('text')
  expect(options).toEqual(expect.objectContaining({
    attempt: expect.objectContaining({ method: 'memory.item.create' }),
  }))
  expect(await screen.findByRole('status')).toHaveTextContent('已保存为记忆')
  fireEvent.click(screen.getByRole('button', { name: '撤销' }))
  await waitFor(() => expect(captureUndo).toHaveBeenCalled())
  expect(captureUndo.mock.calls[0][0]).toEqual(expect.objectContaining({
    undoOperationId: C,
  }))
  expect(await screen.findByRole('status')).toHaveTextContent('已撤销这次记忆')
})

it('hides save-as-memory when capture mode is off', async () => {
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge()}
      messages={listedMessages()}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      memory={{ itemCreate: vi.fn() } as unknown as MemoryBridge}
      memoryOps={memoryOps('off')}
    />,
  )
  await screen.findByText(userMessage.text)
  await waitFor(() => expect(screen.queryByRole('button', { name: '保存为记忆' })).not.toBeInTheDocument())
})

it('shows a stable notice when save-as-memory races with off mode', async () => {
  const itemCreate = vi.fn().mockRejectedValue(new BridgeClientError('记忆已关闭', 'MEMORY_MODE_OFF', false, 'renderer'))
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge()}
      messages={listedMessages()}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      memory={{ itemCreate } as unknown as MemoryBridge}
      memoryOps={memoryOps('manual')}
    />,
  )
  fireEvent.click(await screen.findByRole('button', { name: '保存为记忆' }))
  expect(await screen.findByRole('status')).toHaveTextContent('记忆已关闭')
  await waitFor(() => expect(screen.queryByRole('button', { name: '保存为记忆' })).not.toBeInTheDocument())
})

it('does not open a memory banner after a turn completes', async () => {
  let onEvent: ((event: { v: string; kind: string; id: string; streamId: string; sequence: number; type: string; completed?: { memorySummary?: string } }) => void) | undefined
  const candidates = vi.fn().mockResolvedValue({
    items: [{ candidateId: C, content: '以后回答默认用中文，封面用深色', scopeId: 'learning', confirmationToken: 'tok', createdAt: NOW, expiresAt: NOW }],
  })
  const start = vi.fn().mockImplementation(async (_payload: unknown, onStreamEvent: typeof onEvent) => {
    onEvent = onStreamEvent
    return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() }
  })
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge()}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      chat={{ start, approve: vi.fn(), dispose: vi.fn() } as unknown as ChatBridge}
      feedback={{ record: vi.fn(), candidates } as unknown as FeedbackBridge}
      memory={{ confirmCandidate: vi.fn() } as unknown as MemoryBridge}
      memoryOps={memoryOps()}
    />,
  )
  await screen.findByLabelText('向月汐提问，或描述你想完成的任务…')
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '以后回答请默认用中文' } })
  fireEvent.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => {
    onEvent?.({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', sequence: 1, type: 'completed', completed: { memorySummary: '注入记忆：任务 1' } })
  })
  expect(candidates).not.toHaveBeenCalled()
  expect(screen.queryByRole('status', { name: '待确认偏好' })).not.toBeInTheDocument()
})
