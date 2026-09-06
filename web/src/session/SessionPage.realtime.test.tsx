import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import type { ChatBridge, MessageBridge, ProviderBridge, SessionBridge } from '../bridge/client'
import type { MessageDTO, ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

vi.mock('./companion/CompanionStage', () => ({
  CompanionStage: (props: { chatReady: boolean; onSend: (text: string, messageId: string) => unknown }) => <button disabled={!props.chatReady} onClick={() => void props.onSend('打开网页', '01ARZ3NDEKTSV4RRFFQ69G5FAC')}>已保存的通话指令</button>,
}))
vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return { ...actual, sessionFolderBridge: { get: vi.fn().mockResolvedValue({ path: '' }) }, automationBridge: { listRuns: vi.fn().mockResolvedValue({ runs: [] }) } }
})

afterEach(() => { cleanup(); resetLiveChatForTests(); localStorage.clear() })
const now = '2026-09-06T00:00:00Z'
const project: ProjectDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Project', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: now, updatedAt: now, version: 1 }
const session: SessionDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', projectId: project.id, title: '月伴对话', pinned: false, status: 'active', createdAt: now, updatedAt: now, version: 1 }
const provider: ProviderDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', credentialBackupCount: 0, createdAt: now, updatedAt: now, version: 1 }

test.each([false, true])('handoff skips append only after validating session history (foreign=%s)', async foreign => {
  const saved: MessageDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', sessionId: foreign ? project.id : session.id, role: 'user', status: 'completed', text: '打开网页', sequence: 1, createdAt: now }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [saved], hasMore: false, snapshotSequence: 1 }), append: vi.fn() }
  const start = vi.fn().mockResolvedValue({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), done: Promise.resolve() })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const sessions: SessionBridge = { list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() }
  render(<SessionPage project={project} bridge={sessions} messages={messages} chat={chat} providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge} initialSession={session} initialCompanion personal onBack={vi.fn()} />)
  const button = await screen.findByRole('button', { name: '已保存的通话指令' })
  await waitFor(() => expect(button).toBeEnabled())
  fireEvent.click(button)
  if (foreign) {
    await waitFor(() => expect(messages.list).toHaveBeenCalledTimes(2))
    expect(start).not.toHaveBeenCalled()
  } else {
    await waitFor(() => expect(start).toHaveBeenCalledOnce())
    expect(start.mock.calls[0][0]).toMatchObject({ sessionId: session.id, companion: true, messages: [{ role: 'user', content: '打开网页' }] })
  }
  expect(messages.append).not.toHaveBeenCalled()
})
