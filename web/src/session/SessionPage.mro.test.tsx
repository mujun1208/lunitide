import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { FeedbackBridge, MessageBridge, ProviderBridge, SessionBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

afterEach(() => {
  cleanup()
  resetLiveChatForTests()
  localStorage.clear()
})

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const S = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = { id: P, name: 'Runtime', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const session: SessionDTO = { id: S, projectId: P, title: '机务手册', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const provider: ProviderDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', createdAt: NOW, updatedAt: NOW, version: 1 }
const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
const messages = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge

it('renders the MRO micro-strip from session metadata', async () => {
  const sessions: SessionBridge = {
    list: vi.fn().mockResolvedValue({ items: [session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    metadataGet: vi.fn().mockResolvedValue({
      mroContext: { tailNo: 'B-0000', asOf: '2026-09-03', pack: 'mro.v1', scenario: 'manual' },
    }),
  }
  render(<SessionPage project={project} bridge={sessions} messages={messages} onBack={vi.fn()} personal initialSession={session} providers={providers} />)
  expect(await screen.findByText('机务 · B-0000 · 2026-09-03 · 本轮：手册问答')).toBeInTheDocument()
  expect(document.querySelector('.mro-context-strip')).not.toBeNull()
})

it('hides the micro-strip when the session has no mroContext', async () => {
  const sessions: SessionBridge = {
    list: vi.fn().mockResolvedValue({ items: [session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    metadataGet: vi.fn().mockResolvedValue({}),
  }
  render(<SessionPage project={project} bridge={sessions} messages={messages} onBack={vi.fn()} personal initialSession={session} providers={providers} />)
  await screen.findByText('还没有消息')
  expect(document.querySelector('.mro-context-strip')).toBeNull()
})

it('asks to confirm uncontrolled manual cites with the memory banner language', async () => {
  const feedback = {
    record: vi.fn(),
    candidates: vi.fn().mockResolvedValue({ items: [] }),
  } as unknown as FeedbackBridge
  const listed = {
    list: vi.fn().mockResolvedValue({
      items: [{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        sessionId: S,
        role: 'assistant',
        status: 'completed',
        sequence: 1,
        createdAt: NOW,
        text: '辅助建议，不构成放行。\n<!--mro-cite:{"cites":[{"revision":"42","locator":"{\\"status\\":\\"uncontrolled\\"}","quote":"Gear isolation","expertName":"航空机务专家"}]}-->',
      }],
      hasMore: false,
      nextCursor: null,
      snapshotSequence: 1,
    }),
    append: vi.fn(),
  } as unknown as MessageBridge
  render(<SessionPage project={project} bridge={sessionsStub()} messages={listed} onBack={vi.fn()} personal initialSession={session} providers={providers} feedback={feedback} />)
  const banner = await screen.findByRole('status', { name: '待确认' })
  expect(banner).toHaveTextContent('待确认：将使用未受控手册回答')
  expect(banner).not.toHaveTextContent('放行')
  expect(screen.getByRole('button', { name: '确认沉淀' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '以后再说' })).toBeInTheDocument()
})

function sessionsStub(): SessionBridge {
  return {
    list: vi.fn().mockResolvedValue({ items: [session] }),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    metadataGet: vi.fn().mockResolvedValue({}),
  }
}
