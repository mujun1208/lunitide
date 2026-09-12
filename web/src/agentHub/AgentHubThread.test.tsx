import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubThread } from './AgentHubThread'
import { agentHubApi } from './agentHubApi'

vi.mock('./agentHubApi', () => ({
  agentHubApi: {
    threadGet: vi.fn(),
    threadPrompt: vi.fn(),
    threadCancel: vi.fn(),
    threadRespond: vi.fn(),
    workspaceList: vi.fn(),
    preview: vi.fn(),
    open: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

const THREAD_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAE'

function threadDetail(status: string, extra: {
  messages?: { id: string; seq: number; role: string; content: string; createdAt: string }[]
  prompt?: { callId: string; prompt: string; options: { id: string; label: string }[]; status: string }
} = {}) {
  return {
    thread: {
      threadId: THREAD_ID,
      harnessId: 'loopback',
      nativeSessionId: '',
      title: '新会话',
      pinned: false,
      workspaceRoot: 'C:/tmp',
      exportDir: '',
      scene: 'free' as const,
      status,
      accessMode: 'approval' as const,
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: extra.messages ?? [],
    events: [],
    files: [],
    prompt: extra.prompt,
  }
}

function stubWorkspace() {
  vi.mocked(agentHubApi.workspaceList).mockResolvedValue({ items: [] })
}

it('polls thread.get every 400ms while running and renders messages', async () => {
  vi.useFakeTimers()
  stubWorkspace()
  vi.mocked(agentHubApi.threadGet)
    .mockResolvedValueOnce(threadDetail('running', {
      messages: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', seq: 1, role: 'user', content: 'hello', createdAt: '2026-09-13T00:00:00Z' }],
    }))
    .mockResolvedValue(threadDetail('running', {
      messages: [
        { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', seq: 1, role: 'user', content: 'hello', createdAt: '2026-09-13T00:00:00Z' },
        { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', seq: 2, role: 'assistant', content: 'ok', createdAt: '2026-09-13T00:00:01Z' },
      ],
    }))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  expect(screen.getByText('hello')).toBeInTheDocument()
  await act(async () => { await vi.advanceTimersByTimeAsync(400) })
  expect(screen.getByText('ok')).toBeInTheDocument()
  expect(vi.mocked(agentHubApi.threadGet).mock.calls.length).toBeGreaterThan(1)
})

it('keeps polling while waiting_user and stops after a terminal status', async () => {
  vi.useFakeTimers()
  stubWorkspace()
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('waiting_user'))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  const waitingCalls = vi.mocked(agentHubApi.threadGet).mock.calls.length
  await act(async () => { await vi.advanceTimersByTimeAsync(400) })
  expect(vi.mocked(agentHubApi.threadGet).mock.calls.length).toBeGreaterThan(waitingCalls)
  cleanup()
  vi.mocked(agentHubApi.threadGet).mockClear()
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('success'))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  const doneCalls = vi.mocked(agentHubApi.threadGet).mock.calls.length
  await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
  expect(vi.mocked(agentHubApi.threadGet).mock.calls.length).toBe(doneCalls)
})

it('shows AskBar for an open prompt and disables Send while waiting_user', async () => {
  stubWorkspace()
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('waiting_user', {
    prompt: {
      callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
      prompt: '选哪个?',
      options: [{ id: '是', label: '是' }, { id: '否', label: '否' }],
      status: 'open',
    },
  }))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: '是' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('消息'), { target: { value: '继续' } })
  expect(screen.getByRole('button', { name: '发送' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  expect(agentHubApi.threadPrompt).not.toHaveBeenCalled()
})

it('disables Send while running', async () => {
  stubWorkspace()
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('running'))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: '发送' })).toBeDisabled()
})

it('shows cancel while live and calls threadCancel', async () => {
  stubWorkspace()
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('running'))
  vi.mocked(agentHubApi.threadCancel).mockResolvedValue(threadDetail('cancelled'))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '取消' }))
  await waitFor(() => expect(agentHubApi.threadCancel).toHaveBeenCalledWith({ threadId: THREAD_ID }))
})

it('submits the composer via threadPrompt when idle', async () => {
  stubWorkspace()
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('idle'))
  vi.mocked(agentHubApi.threadPrompt).mockResolvedValue(threadDetail('running'))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  fireEvent.change(await screen.findByLabelText('消息'), { target: { value: '继续' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  await waitFor(() => expect(agentHubApi.threadPrompt).toHaveBeenCalledWith({ threadId: THREAD_ID, text: '继续' }))
})

it('lists loopback.txt after answering an open prompt', async () => {
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('waiting_user', {
    prompt: {
      callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
      prompt: '选哪个?',
      options: [{ id: '是', label: '是' }, { id: '否', label: '否' }],
      status: 'open',
    },
  }))
  vi.mocked(agentHubApi.workspaceList).mockResolvedValue({ items: [] })
  vi.mocked(agentHubApi.threadRespond).mockResolvedValue(threadDetail('success'))
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: '是' })).toBeInTheDocument()
  vi.mocked(agentHubApi.workspaceList).mockResolvedValue({
    items: [{ name: 'loopback.txt', path: 'loopback.txt', size: 2, isDir: false }],
  })
  fireEvent.click(screen.getByRole('button', { name: '是' }))
  await waitFor(() => expect(agentHubApi.threadRespond).toHaveBeenCalledWith({
    threadId: THREAD_ID,
    callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
    optionId: '是',
  }))
  expect(await screen.findByRole('button', { name: 'loopback.txt' })).toBeInTheDocument()
})

it('ignores a late poll from a previous threadId', async () => {
  vi.useFakeTimers()
  stubWorkspace()
  const otherId = '01ARZ3NDEKTSV4RRFFQ69G5FAF'
  let finishPoll: (value: ReturnType<typeof threadDetail>) => void = () => {}
  let gets = 0
  vi.mocked(agentHubApi.threadGet).mockImplementation(async (payload: { threadId: string }) => {
    if (payload.threadId === THREAD_ID) {
      gets += 1
      if (gets === 1) {
        return threadDetail('running', {
          messages: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', seq: 1, role: 'user', content: 'a-live', createdAt: '2026-09-13T00:00:00Z' }],
        })
      }
      return new Promise(resolve => { finishPoll = resolve })
    }
    return {
      ...threadDetail('idle'),
      thread: { ...threadDetail('idle').thread, threadId: otherId, title: 'B' },
      messages: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', seq: 1, role: 'user', content: 'b-idle', createdAt: '2026-09-13T00:00:00Z' }],
    }
  })
  const { rerender } = render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  expect(screen.getByText('a-live')).toBeInTheDocument()
  await act(async () => { await vi.advanceTimersByTimeAsync(400) })
  rerender(<LanguageProvider value="zh-CN"><AgentHubThread threadId={otherId} /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  expect(screen.getByText('b-idle')).toBeInTheDocument()
  await act(async () => {
    finishPoll(threadDetail('running', {
      messages: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', seq: 1, role: 'user', content: 'a-stale', createdAt: '2026-09-13T00:00:01Z' }],
    }))
  })
  expect(screen.queryByText('a-stale')).toBeNull()
  expect(screen.getByText('b-idle')).toBeInTheDocument()
})

it('previews a workspace file with threadId', async () => {
  vi.mocked(agentHubApi.threadGet).mockResolvedValue(threadDetail('idle'))
  vi.mocked(agentHubApi.workspaceList).mockResolvedValue({
    items: [{ name: 'loopback.txt', path: 'loopback.txt', size: 2, isDir: false }],
  })
  vi.mocked(agentHubApi.preview).mockResolvedValue({
    kind: 'text',
    path: 'loopback.txt',
    size: 2,
    content: 'yes',
  })
  render(<LanguageProvider value="zh-CN"><AgentHubThread threadId={THREAD_ID} /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'loopback.txt' }))
  await waitFor(() => expect(agentHubApi.preview).toHaveBeenCalledWith({ threadId: THREAD_ID, path: 'loopback.txt' }))
  expect(await screen.findByText('yes')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '用本机打开' }))
  await waitFor(() => expect(agentHubApi.open).toHaveBeenCalledWith({ threadId: THREAD_ID, path: 'loopback.txt' }))
})
