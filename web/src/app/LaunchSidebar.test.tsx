import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MessageBridge, ProjectBridge, SessionBridge } from '../bridge/client'
import { LaunchSidebar } from './LaunchSidebar'

vi.mock('../bridge/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return {
    ...actual,
    getIdentityBridge: () => ({ get: vi.fn().mockResolvedValue(undefined) }),
    getPeopleBridge: () => ({ threadList: vi.fn().mockResolvedValue({ items: [] }) }),
    getMcpBridge: () => ({ list: vi.fn().mockResolvedValue({ endpoints: [] }) }),
    mcpBridge: { list: vi.fn().mockResolvedValue({ endpoints: [] }) },
  }
})

afterEach(() => {
  cleanup()
  localStorage.clear()
})

function sidebarProps(overrides: Record<string, unknown> = {}) {
  return {
    open: true,
    page: 'home' as const,
    setPage: vi.fn(),
    projects: { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectBridge,
    sessions: { list: vi.fn(), update: vi.fn(), delete: vi.fn() } as unknown as SessionBridge,
    messages: {} as MessageBridge,
    onSelect: vi.fn(),
    onNew: vi.fn(),
    theme: 'dark' as const,
    language: 'zh-CN' as const,
    refreshKey: 0,
    localChats: [],
    deletedChatIds: new Set<string>(),
    draftSessionIds: new Set<string>(),
    onToggleTheme: vi.fn(),
    onToggleLanguage: vi.fn(),
    collapsed: false,
    onUpdated: vi.fn(),
    onDeleted: vi.fn(),
    onOpenPeople: vi.fn(),
    ...overrides,
  }
}

it('starts the sidebar at 新对话 and has no product wordmark', () => {
  render(<LaunchSidebar {...sidebarProps()} />)
  const sidebar = document.getElementById('launch-sidebar')
  expect(sidebar).not.toBeNull()
  expect(screen.queryByRole('button', { name: '月汐首页' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Lunitide home' })).toBeNull()
  expect(sidebar!.querySelector('.launch-brand')).toBeNull()
  expect(sidebar!.textContent).not.toContain('月汐')
  expect(sidebar!.textContent).not.toContain('LUNITIDE')
  expect(sidebar!.querySelector('button')).toHaveTextContent('新对话')
  expect(screen.getByRole('button', { name: '对话' })).not.toBeNull()
  expect(screen.getByRole('button', { name: '办公' })).not.toBeNull()
})

it('never lists AgentHub under Office, even when leftover office-menu storage still has it', () => {
  render(<LaunchSidebar {...sidebarProps()} />)
  expect(screen.queryByRole('button', { name: 'AgentHub' })).toBeNull()
  cleanup()
  localStorage.setItem('lunitide:office-menu', JSON.stringify({ agentHub: true }))
  const setPage = vi.fn()
  render(<LaunchSidebar {...sidebarProps({ setPage, page: 'agentHub' })} />)
  expect(screen.queryByRole('button', { name: 'AgentHub' })).toBeNull()
  expect(screen.getByRole('button', { name: '设置' }).className).not.toMatch(/active/)
  expect(screen.getByRole('button', { name: /新对话/ })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '历史任务' })).toBeNull()
})

it('does not show raw English conversation list failures', async () => {
  render(
    <LaunchSidebar
      open
      page="home"
      setPage={vi.fn()}
      projects={{ list: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as ProjectBridge}
      sessions={{ list: vi.fn(), update: vi.fn(), delete: vi.fn() } as unknown as SessionBridge}
      messages={{} as MessageBridge}
      onSelect={vi.fn()}
      onNew={vi.fn()}
      theme="dark"
      language="zh-CN"
      refreshKey={0}
      localChats={[]}
      deletedChatIds={new Set()}
      draftSessionIds={new Set()}
      onToggleTheme={vi.fn()}
      onToggleLanguage={vi.fn()}
      collapsed={false}
      onUpdated={vi.fn()}
      onDeleted={vi.fn()}
      onOpenPeople={vi.fn()}
    />,
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('对话列表加载失败，请重试。')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('shows replaceMainNav and hides the 对话 heading', () => {
  render(<LaunchSidebar {...sidebarProps({ replaceMainNav: <div>slot</div> })} />)
  const slot = screen.getByText('slot')
  expect(slot).not.toBeNull()
  expect(screen.queryByRole('button', { name: '对话' })).toBeNull()
  expect(slot.closest('.primary-actions')).toBeNull()
  expect(slot.closest('.hub-sidebar-stack')).not.toBeNull()
  expect(screen.getByRole('button', { name: '项目' })).toBeInTheDocument()
  expect(screen.getByLabelText('调整 Agent 与项目')).toBeInTheDocument()
})

it('hides Lunitide message search when replaceMainNav is set', () => {
  const search = vi.fn()
  render(<LaunchSidebar {...sidebarProps({
    replaceMainNav: <div>slot</div>,
    messages: { search } as unknown as MessageBridge,
  })} />)
  expect(screen.queryByRole('button', { name: /搜索/ })).toBeNull()
  expect(search).not.toHaveBeenCalled()
})

it('hides Media Center and Automation when office-menu turns them off', () => {
  localStorage.setItem('lunitide:office-menu', JSON.stringify({ media: false, automation: false }))
  render(<LaunchSidebar {...sidebarProps()} />)
  expect(screen.getByRole('button', { name: '办公' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '媒体中心' })).toBeNull()
  expect(screen.queryByRole('button', { name: '自动化' })).toBeNull()
  expect(screen.queryByRole('button', { name: '项目' })).toBeNull()
})

it('shows 项目 group only on AgentHub, not Chat', () => {
  render(<LaunchSidebar {...sidebarProps()} />)
  expect(screen.queryByRole('button', { name: '项目' })).toBeNull()
  expect(screen.getByRole('button', { name: '办公' })).toBeInTheDocument()
  cleanup()
  render(<LaunchSidebar {...sidebarProps({ replaceMainNav: <div>agents</div> })} />)
  expect(screen.getByRole('button', { name: '项目' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '项目' })).toHaveAttribute('title', expect.stringMatching(/绿灯|Green/))
  expect(screen.getByLabelText('调整 Agent 与项目')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '对话' })).toBeNull()
})

it('TestMediaCenterOfficeNavigation: keeps Media Center in Office even when optional office items are hidden', () => {
  render(<LaunchSidebar {...sidebarProps({ page: 'media' })} />)
  const media = screen.getByRole('button', { name: '媒体中心' })
  expect(media.className).toMatch(/active/)
  expect(document.getElementById('office-list')).toContainElement(media)
  expect(screen.getByRole('button', { name: '自动化' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '办公工作台' })).toBeNull()
  expect(screen.getByRole('button', { name: '办公' })).toHaveClass('is-current')
})
