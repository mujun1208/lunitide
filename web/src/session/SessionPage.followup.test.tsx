import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { ChatBridge, ChatStream, MessageBridge, ProviderBridge, SessionBridge, StreamEvent } from '../bridge/client'
import { runQueueBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

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
const project: ProjectDTO = { id: P, name: 'Followup', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const session: SessionDTO = { id: S, projectId: P, title: 'Session', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const sessionBridge: SessionBridge = { list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() }
const provider: ProviderDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', credentialBackupCount: 0, createdAt: NOW, updatedAt: NOW, version: 1 }
const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge

async function openSession(chat: ChatBridge, messages: MessageBridge) {
  render(<SessionPage project={project} bridge={sessionBridge} personal initialSession={session} providers={providers} messages={messages} chat={chat} onBack={vi.fn()} />)
  await screen.findByRole('button', { name: '↑ 发送并对话' })
}

it('keeps prior thinking when 「做好了没有」 attaches to the in-flight task; 停止 still cancels', async () => {
  let onEvent!: (event: StreamEvent) => void
  const cancel = vi.fn().mockResolvedValue(true)
  const stream: ChatStream = { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel, dispose: vi.fn() }
  const start = vi.fn().mockImplementation(async (_payload, onStreamEvent) => {
    onEvent = onStreamEvent
    return stream
  })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const user = userEvent.setup()
  await openSession(chat, { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge)
  const input = screen.getByLabelText('向月汐提问，或描述你想完成的任务…')
  fireEvent.change(input, { target: { value: '请 PPT专家做一份介绍' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => onEvent({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: stream.streamId, sequence: 1, type: 'thinking', thinking: { text: '先定受众和页序，再动手做幻灯片。' } }))
  await user.click(screen.getByText('任务过程'))
  expect(document.querySelector('.thinking-live-text')?.textContent).toContain('先定受众和页序')
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '做好了没有' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送' }))
  await waitFor(() => expect(runQueueBridge.input).toHaveBeenCalledWith(expect.objectContaining({ sessionId: S, text: '做好了没有' })))
  expect(start).toHaveBeenCalledOnce()
  expect(cancel).not.toHaveBeenCalled()
  expect(document.querySelector('.thinking-live-text')?.textContent).toContain('先定受众和页序')
  await act(async () => onEvent({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAF', streamId: stream.streamId, sequence: 2, type: 'thinking', thinking: { text: '还在写大纲，没有另起炉灶。' } }))
  const live = document.querySelector('.thinking-live-text')?.textContent ?? ''
  expect(live).toContain('先定受众和页序')
  expect(live).toContain('没有另起炉灶')
  await user.click(screen.getByRole('button', { name: '停止' }))
  expect(cancel).toHaveBeenCalledOnce()
})

it('queues 「做好了没有」 for an in-flight 报告编写专家 turn without aborting', async () => {
  let onEvent!: (event: StreamEvent) => void
  const cancel = vi.fn().mockResolvedValue(true)
  const stream: ChatStream = { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel, dispose: vi.fn() }
  const start = vi.fn().mockImplementation(async (_payload, onStreamEvent) => {
    onEvent = onStreamEvent
    return stream
  })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const user = userEvent.setup()
  await openSession(chat, { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge)
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '请 报告编写专家写一份调研报告' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => onEvent({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: stream.streamId, sequence: 1, type: 'thinking', thinking: { text: '先想读者和目的，再列目录。' } }))
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '做好了没有' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送' }))
  await waitFor(() => expect(runQueueBridge.input).toHaveBeenCalledWith(expect.objectContaining({ sessionId: S, text: '做好了没有' })))
  expect(start).toHaveBeenCalledOnce()
  expect(cancel).not.toHaveBeenCalled()
})

it('queues a PPT supplement without clearing prior mermaid/thinking', async () => {
  let onEvent!: (event: StreamEvent) => void
  const cancel = vi.fn().mockResolvedValue(true)
  const stream: ChatStream = { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel, dispose: vi.fn() }
  const start = vi.fn().mockImplementation(async (_payload, onStreamEvent) => {
    onEvent = onStreamEvent
    return stream
  })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const user = userEvent.setup()
  await openSession(chat, { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge)
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '请 PPT专家做一份介绍' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => onEvent({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: stream.streamId, sequence: 1, type: 'thinking', thinking: { text: '```mermaid\nflowchart TD\nA-->B\n```\n' } }))
  await user.click(screen.getByText('任务过程'))
  expect(document.querySelector('.thinking-live-text')?.textContent ?? '').toContain('flowchart')
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '封面先做出来' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送' }))
  await waitFor(() => expect(runQueueBridge.input).toHaveBeenCalledWith(expect.objectContaining({ sessionId: S, text: '封面先做出来' })))
  expect(start).toHaveBeenCalledOnce()
  expect(cancel).not.toHaveBeenCalled()
  expect(document.querySelector('.thinking-live-text')?.textContent ?? '').toContain('flowchart')
})

it('pivots to a new task: cancels in-flight work, refreshes history, starts a new stream', async () => {
  let onEvent!: (event: StreamEvent) => void
  let firstOnEvent: ((event: StreamEvent) => void) | undefined
  const cancel = vi.fn().mockResolvedValue(true)
  const firstStream: ChatStream = { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel, dispose: vi.fn() }
  const nextStream: ChatStream = { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FB1', cancel: vi.fn().mockResolvedValue(true), dispose: vi.fn() }
  const start = vi.fn().mockImplementation(async (_payload, onStreamEvent) => {
    onEvent = onStreamEvent
    if (!firstOnEvent) firstOnEvent = onStreamEvent
    return start.mock.calls.length > 1 ? nextStream : firstStream
  })
  const list = vi.fn()
    .mockResolvedValueOnce({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 })
    .mockResolvedValue({
      items: [{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAC',
        sessionId: S,
        role: 'assistant',
        text: '先定受众和页序，再动手做幻灯片。\n终止打断了',
        status: 'completed',
        sequence: 2,
        createdAt: NOW,
      }],
      hasMore: false,
      nextCursor: null,
      snapshotSequence: 2,
    })
  const append = vi.fn().mockResolvedValue({})
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const user = userEvent.setup()
  await openSession(chat, { list, append } as MessageBridge)
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '请 PPT专家做一份介绍' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => onEvent({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAE', streamId: firstStream.streamId, sequence: 1, type: 'thinking', thinking: { text: '先定受众和页序，再动手做幻灯片。' } }))
  await user.click(screen.getByText('任务过程'))
  expect(document.querySelector('.thinking-live-text')?.textContent ?? '').toContain('先定受众和页序')
  fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), { target: { value: '别做PPT了帮我查天气' } })
  await user.click(screen.getByRole('button', { name: '↑ 发送' }))
  await waitFor(() => expect(cancel).toHaveBeenCalledOnce())
  await act(async () => firstOnEvent?.({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FAF', streamId: firstStream.streamId, sequence: 2, type: 'cancelled' }))
  await waitFor(() => expect(start).toHaveBeenCalledTimes(2))
  expect(await screen.findByText(/先定受众和页序/)).toBeInTheDocument()
  expect(start.mock.calls[1][0]).toMatchObject({ sessionId: S })
  expect(append).toHaveBeenLastCalledWith(expect.objectContaining({ text: '别做PPT了帮我查天气' }), expect.anything())
  await act(async () => onEvent({ v: '1.0', kind: 'event', id: '01ARZ3NDEKTSV4RRFFQ69G5FB0', streamId: nextStream.streamId, sequence: 1, type: 'thinking', thinking: { text: '正在查天气…' } }))
  await user.click(screen.getByText('任务过程'))
  const live = document.querySelector('.thinking-live-text')?.textContent ?? ''
  expect(live).toContain('正在查天气')
  expect(live).not.toContain('先定受众和页序')
})
