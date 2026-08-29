import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatBridge, ChatStream, MessageBridge, ProviderBridge, SessionBridge, StreamEvent } from '../bridge/client'
import { runQueueBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'
import { applyLiveChatEvent, resetLiveChatForTests, startLiveChat } from './liveChat'

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  const queue = {
    input: vi.fn().mockResolvedValue({ queuedId: '01ARZ3NDEKTSV4RRFFQ69G5FA01', seq: 1, status: 'queued', mark: 'turn_boundary' }),
    list: vi.fn().mockResolvedValue({ items: [] }),
    withdraw: vi.fn(),
    consume: vi.fn().mockResolvedValue({ count: 0, items: [] }),
  }
  return { ...actual, runQueueBridge: queue, getRunQueueBridge: () => queue }
})

afterEach(() => {
  cleanup()
  resetLiveChatForTests()
  vi.mocked(runQueueBridge.input).mockClear()
  vi.mocked(runQueueBridge.list).mockResolvedValue({ items: [] })
})

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV', S = '01ARZ3NDEKTSV4RRFFQ69G5FAA', NOW = '2025-01-01T00:00:00Z'
const USER_MSG = '01ARZ3NDEKTSV4RRFFQ69G5FAC'
const project: ProjectDTO = { id: P, name: 'TurnControl', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const session: SessionDTO = { id: S, projectId: P, title: 'Session', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const sessionBridge: SessionBridge = { list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() }
const provider: ProviderDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', createdAt: NOW, updatedAt: NOW, version: 1 }
const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge

function chatHarness() {
  let onEvent!: (event: StreamEvent) => void
  const cancel = vi.fn().mockResolvedValue(true)
  const stream: ChatStream = { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel, dispose: vi.fn() }
  const start = vi.fn().mockImplementation(async (_payload, onStreamEvent) => {
    onEvent = onStreamEvent
    return stream
  })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  return { onEvent: () => onEvent, cancel, start, chat, stream }
}

it('morphs the primary composer button to 停止 for a single in-flight turn', async () => {
  const { onEvent, cancel, start, chat } = chatHarness()
  const user = userEvent.setup()
  render(<SessionPage project={project} bridge={sessionBridge} personal initialSession={session} providers={providers} messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({ id: USER_MSG }) } as MessageBridge} chat={chat} onBack={vi.fn()} />)
  fireEvent.change(await screen.findByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '先做一份报告' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => onEvent()({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', sequence: 1, type: 'thinking', thinking: { text: '列目录…' } }))
  const stopButton = screen.getByRole('button', { name: '停止' })
  expect(stopButton).toBeInTheDocument()
  expect(stopButton.querySelector('svg')).toBeTruthy()
  expect(stopButton).not.toHaveTextContent('停止')
  expect(screen.queryByRole('button', { name: '↑ 发送并对话' })).toBeNull()
  await user.click(screen.getByRole('button', { name: '停止' }))
  expect(cancel).toHaveBeenCalledOnce()
})

it('sends follow-ups while streaming instead of stopping', async () => {
  const { onEvent, cancel, start, chat } = chatHarness()
  const user = userEvent.setup()
  render(<SessionPage project={project} bridge={sessionBridge} personal initialSession={session} providers={providers} messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({ id: USER_MSG }) } as MessageBridge} chat={chat} onBack={vi.fn()} />)
  const input = await screen.findByLabelText('向月汐提问，或描述你想完成的任务…')
  fireEvent.change(input, { target: { value: '先做一份报告' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => onEvent()({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', sequence: 1, type: 'thinking', thinking: { text: '列目录…' } }))
  fireEvent.change(input, { target: { value: '封面先做出来' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送' }))
  await waitFor(() => expect(runQueueBridge.input).toHaveBeenCalledWith(expect.objectContaining({ sessionId: S, text: '封面先做出来' })))
  expect(cancel).not.toHaveBeenCalled()
  expect(start).toHaveBeenCalledOnce()
})

it('shows segment stop controls for multiple active turns and cancels only one', async () => {
  const cancel1 = vi.fn().mockResolvedValue(true)
  const cancel2 = vi.fn().mockResolvedValue(true)
  const entry1 = startLiveChat(S, 'turn-a')
  entry1.stream = { streamId: 'stream-a', cancel: cancel1, dispose: vi.fn() }
  const entry2 = startLiveChat(S, 'turn-b')
  entry2.stream = { streamId: 'stream-b', cancel: cancel2, dispose: vi.fn() }
  const { chat } = chatHarness()
  render(<SessionPage project={project} bridge={sessionBridge} personal initialSession={session} providers={providers} messages={{ list: vi.fn().mockResolvedValue({ items: [{ id: 'turn-a', sessionId: S, role: 'user', text: '任务 A', status: 'completed', sequence: 1, createdAt: NOW }, { id: 'turn-b', sessionId: S, role: 'user', text: '任务 B', status: 'completed', sequence: 3, createdAt: NOW }], hasMore: false, nextCursor: null, snapshotSequence: 3 }), append: vi.fn().mockResolvedValue({ id: USER_MSG }) } as MessageBridge} chat={chat} onBack={vi.fn()} />)
  const stops = await screen.findAllByRole('button', { name: '停止此轮' })
  expect(stops.length).toBeGreaterThanOrEqual(2)
  await userEvent.setup().click(stops[0])
  expect(cancel1).toHaveBeenCalledOnce()
  expect(cancel2).not.toHaveBeenCalled()
  applyLiveChatEvent(entry1, { v: '1.0', kind: 'event', id: 'e1', streamId: 'stream-a', sequence: 1, type: 'cancelled' })
  expect(entry2.terminal).toBe(false)
})
