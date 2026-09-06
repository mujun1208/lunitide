import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { runQueueBridge, type ChatBridge, type MessageBridge, type ProviderBridge, type SessionBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

afterEach(() => { cleanup(); resetLiveChatForTests(); vi.restoreAllMocks(); localStorage.clear() })

it('starts the saved queue delivery with its receipt without appending the long text again', async () => {
  const p = '01ARZ3NDEKTSV4RRFFQ69G5FAV', s = '01ARZ3NDEKTSV4RRFFQ69G5FAW', d = '01ARZ3NDEKTSV4RRFFQ69G5FAX', now = '2025-01-01T00:00:00Z'
  const project: ProjectDTO = { id: p, name: 'Queue project', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: now, updatedAt: now, version: 1 }
  const session: SessionDTO = { id: s, projectId: p, title: 'Queue session', pinned: false, status: 'active', createdAt: now, updatedAt: now, version: 1 }
  const provider: ProviderDTO = { id: p, name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.invalid', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', credentialBackupCount: 0, createdAt: now, updatedAt: now, version: 1 }
  const items = [{ queuedId: d, seq: 1, text: '🙂'.repeat(8000), status: 'queued' as const, mark: 'turn_boundary' as const, createdAt: now }]
  const delivery = { id: d, state: 'prepared' as const, messageIds: [d], items }
  vi.spyOn(runQueueBridge, 'list').mockResolvedValue({ items: [], delivery })
  vi.spyOn(runQueueBridge, 'consume').mockResolvedValue({ count: 1, items, delivery })
  const append = vi.fn(), start = vi.fn().mockResolvedValue({ streamId: d, cancel: vi.fn().mockResolvedValue(true), dispose: vi.fn() })
  const messages = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append } as unknown as MessageBridge
  render(<SessionPage project={project} initialSession={session} onBack={vi.fn()} bridge={{ list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() } as SessionBridge} messages={messages} providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge} chat={{ start, dispose: vi.fn() } as ChatBridge} />)
  const button = await screen.findByRole('button', { name: '继续发送已保存说明' })
  await waitFor(() => expect(screen.queryByText('模型加载中…')).not.toBeInTheDocument())
  await userEvent.setup().click(button)
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  expect(start.mock.calls[0][0]).toMatchObject({ sessionId: s, queueDeliveryId: d })
  expect(start.mock.calls[0][0]).not.toHaveProperty('messages')
  expect(append).not.toHaveBeenCalled()
})
