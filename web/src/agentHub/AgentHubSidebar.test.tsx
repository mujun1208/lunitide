import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubSidebar } from './AgentHubSidebar'
import { agentHubApi } from './agentHubApi'

vi.mock('./agentHubApi', () => ({
  agentHubApi: {
    detect: vi.fn(),
    threadList: vi.fn(),
    threadUpdate: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const THREAD_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAE'

function thread(title: string, harnessId = 'cursor', extras: { pinned?: boolean } = {}) {
  return {
    threadId: THREAD_ID,
    harnessId,
    nativeSessionId: '',
    title,
    pinned: extras.pinned ?? false,
    workspaceRoot: 'C:/tmp',
    exportDir: '',
    scene: 'free' as const,
    status: 'idle',
    accessMode: 'approval',
    createdAt: '2026-09-13T00:00:00Z',
    updatedAt: '2026-09-13T00:00:00Z',
  }
}

it('groups threads under detect results, opens a thread, and fills the launch-nav slot', async () => {
  const onOpenThread = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
      { name: 'codex', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({
    items: [thread('写项目会话', 'cursor'), { ...thread('改代码会话', 'codex'), threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAF' }],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubSidebar onOpenThread={onOpenThread} /></LanguageProvider>)
  const nav = await screen.findByRole('navigation', { name: '外接 Agent' })
  expect(nav.getAttribute('style') ?? '').toMatch(/flex:\s*1/)
  expect(nav.getAttribute('style') ?? '').toMatch(/min-height:\s*0/)
  expect(nav.getAttribute('style') ?? '').toContain('overflow: auto')
  expect(await screen.findByRole('heading', { name: 'cursor' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'codex' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '写项目会话' }))
  expect(onOpenThread).toHaveBeenCalledWith(THREAD_ID)
})

it('filters thread titles locally and pins through thread.update', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({
    items: [
      thread('写项目会话', 'cursor'),
      { ...thread('周报草稿', 'cursor'), threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAF' },
    ],
  })
  vi.mocked(agentHubApi.threadUpdate).mockResolvedValue({
    thread: { ...thread('写项目会话', 'cursor'), pinned: true },
    messages: [],
    events: [],
    files: [],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubSidebar onOpenThread={vi.fn()} /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: '写项目会话' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '周报' } })
  expect(screen.queryByRole('button', { name: '写项目会话' })).toBeNull()
  expect(screen.getByRole('button', { name: '周报草稿' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '' } })
  fireEvent.click(screen.getByRole('button', { name: '置顶 写项目会话' }))
  await waitFor(() => expect(agentHubApi.threadUpdate).toHaveBeenCalledWith({ threadId: THREAD_ID, pinned: true }))
})

it('reloads threads when selectedThreadId or newThreadNonce changes', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [] })
  const view = render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={vi.fn()} selectedThreadId={undefined} newThreadNonce={0} />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('heading', { name: 'cursor' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '新会话' })).toBeNull()
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [thread('新会话')] })
  view.rerender(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={vi.fn()} selectedThreadId={THREAD_ID} newThreadNonce={1} />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('button', { name: '新会话' })).toBeInTheDocument()
})

it('keeps listed threads when detect fails', async () => {
  vi.mocked(agentHubApi.detect).mockRejectedValue(new Error('timeout'))
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [thread('写项目会话')] })
  render(<LanguageProvider value="zh-CN"><AgentHubSidebar onOpenThread={vi.fn()} /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: '写项目会话' })).toBeInTheDocument()
})
