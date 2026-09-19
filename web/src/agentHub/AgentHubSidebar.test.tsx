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
    threadDelete: vi.fn(),
    list: vi.fn(),
    install: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const THREAD_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAE'

function thread(title: string, harnessId = 'cursor', extras: { pinned?: boolean; updatedAt?: string } = {}) {
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
    updatedAt: extras.updatedAt ?? '2026-09-13T00:00:00Z',
  }
}

it('shows three branded agents and opens that Agent’s latest thread', async () => {
  const onOpenThread = vi.fn()
  const onSelectAgent = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'kimi', state: 'not_logged_in', version: '1', nonInteractive: true, streamJSON: true, hint: '未登录' },
    ],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({
    items: [
      thread('旧会话', 'cursor', { updatedAt: '2026-09-13T00:00:00Z' }),
      { ...thread('新会话', 'cursor', { updatedAt: '2026-09-14T00:00:00Z' }), threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAF' },
    ],
  })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar
        onOpenThread={onOpenThread}
        onSelectAgent={onSelectAgent}
        selectedAgent="cursor"
        selectedThreadId="01ARZ3NDEKTSV4RRFFQ69G5FAE"
      />
    </LanguageProvider>,
  )
  const nav = await screen.findByRole('navigation', { name: 'Work' })
  expect(nav.className).toContain('agent-hub-sidebar')
  expect(screen.getByRole('img', { name: 'Codex' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Cursor' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Kimi' })).toBeInTheDocument()
  expect(screen.queryByLabelText('搜索会话')).toBeNull()
  expect(screen.queryByRole('button', { name: '新会话' })).toBeNull()
  expect(screen.getByText('已连接')).toBeInTheDocument()
  expect(screen.getByText('未安装')).toBeInTheDocument()
  expect(screen.getByText('未连接')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '打开 Cursor' }))
  expect(onSelectAgent).toHaveBeenCalledWith('cursor')
  expect(onOpenThread).toHaveBeenCalledWith('01ARZ3NDEKTSV4RRFFQ69G5FAF')
  const detectAtStart = vi.mocked(agentHubApi.detect).mock.calls.length
  fireEvent.click(screen.getByRole('button', { name: '连接 Cursor' }))
  await waitFor(() => expect(vi.mocked(agentHubApi.detect).mock.calls.length).toBeGreaterThan(detectAtStart))
})

it('keeps a blank home after 新对话 when the agent row is clicked', async () => {
  const onOpenThread = vi.fn()
  const onSelectAgent = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({
    items: [{ ...thread('仍可用的会话', 'cursor'), threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAF' }],
  })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar
        onOpenThread={onOpenThread}
        onSelectAgent={onSelectAgent}
        selectedAgent="cursor"
        selectedThreadId={undefined}
        newThreadNonce={1}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '打开 Cursor' }))
  expect(onSelectAgent).toHaveBeenCalledWith('cursor')
  expect(onOpenThread).toHaveBeenCalledWith('')
})

it('opens the old task center from 历史对话', async () => {
  const onOpenHistory = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({ agents: [] })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [] })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={vi.fn()} onOpenHistory={onOpenHistory} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: /历史对话/ }))
  expect(onOpenHistory).toHaveBeenCalled()
})

it('reloads the latest thread when selectedThreadId or newThreadNonce changes', async () => {
  const onOpenThread = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [] })
  const view = render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={onOpenThread} onSelectAgent={vi.fn()} selectedThreadId={undefined} newThreadNonce={0} />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('img', { name: 'Cursor' })).toBeInTheDocument()
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [thread('新会话')] })
  view.rerender(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={onOpenThread} onSelectAgent={vi.fn()} selectedThreadId={THREAD_ID} newThreadNonce={1} />
    </LanguageProvider>,
  )
  await waitFor(() => expect(vi.mocked(agentHubApi.threadList).mock.calls.length).toBeGreaterThan(1))
  fireEvent.click(screen.getByRole('button', { name: '打开 Cursor' }))
  expect(onOpenThread).toHaveBeenCalledWith(THREAD_ID)
})

it('opens a new chat instead of a faulted latest thread', async () => {
  const onOpenThread = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({
    items: [{ ...thread('卡住的会话', 'kimi'), status: 'faulted' }],
  })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={onOpenThread} onSelectAgent={vi.fn()} selectedAgent="kimi" />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '打开 Kimi' }))
  expect(onOpenThread).toHaveBeenCalledWith('')
})

it('asks before installing a missing CLI from the connect button', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [] })
  vi.mocked(agentHubApi.install).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
    installed: true,
    connected: true,
    hint: '',
  })
  render(<LanguageProvider value="zh-CN"><AgentHubSidebar onOpenThread={vi.fn()} /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '连接 Cursor' }))
  fireEvent.click(screen.getByRole('button', { name: '确定安装' }))
  await waitFor(() => expect(agentHubApi.install).toHaveBeenCalledWith({ name: 'cursor', confirmed: true }))
})

it('starts a new chat from the rail', async () => {
  const onNewChat = vi.fn()
  vi.mocked(agentHubApi.detect).mockResolvedValue({ agents: [] })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [] })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubSidebar onOpenThread={vi.fn()} onNewChat={onNewChat} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: /新对话/ }))
  expect(onNewChat).toHaveBeenCalled()
})

it('keeps the three agents when detect fails', async () => {
  vi.mocked(agentHubApi.detect).mockRejectedValue(new Error('timeout'))
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [thread('写项目会话')] })
  render(<LanguageProvider value="zh-CN"><AgentHubSidebar onOpenThread={vi.fn()} /></LanguageProvider>)
  expect(await screen.findByRole('img', { name: 'Codex' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Cursor' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Kimi' })).toBeInTheDocument()
})

it('keeps 历史对话 but does not list thread titles on the rail', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [{ name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' }],
  })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({
    items: [thread('今天上海的天气？ Kimi', 'kimi'), thread('写项目会话', 'cursor')],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubSidebar onOpenThread={vi.fn()} selectedAgent="cursor" /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: /历史对话/ })).toBeInTheDocument()
  expect(screen.queryByText('今天上海的天气？ Kimi')).toBeNull()
  expect(screen.queryByText('写项目会话')).toBeNull()
  expect(screen.queryByLabelText('最近对话')).toBeNull()
  expect(screen.queryByText('还没有对话记录')).toBeNull()
})
