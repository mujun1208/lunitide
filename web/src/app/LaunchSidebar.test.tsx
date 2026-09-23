import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MessageBridge, ProjectBridge, SessionBridge } from '../bridge/client'
import type { ProjectDTO, SessionDTO } from '../generated/bridge'
import type { ChatTarget } from './appTypes'
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

it('shows 产品总览 in Office like other menu items and hides it when the toggle is off', () => {
  const setPage = vi.fn()
  render(<LaunchSidebar {...sidebarProps({ page: 'media', setPage })} />)
  fireEvent.click(screen.getByRole('button', { name: '产品总览' }))
  expect(setPage).toHaveBeenCalledWith('productHub')
  cleanup()
  localStorage.setItem('lunitide:office-menu', JSON.stringify({ productHub: false }))
  render(<LaunchSidebar {...sidebarProps({ page: 'media' })} />)
  expect(screen.queryByRole('button', { name: '产品总览' })).toBeNull()
  expect(screen.getByRole('button', { name: '媒体中心' })).toBeInTheDocument()
})

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

it('frames the open history row and leaves the others plain', async () => {
  const now = '2027-01-01T00:00:00Z'
  const project = { id: 'p1', name: 'chat', projectCode: 'chat', type: 'implementation', status: 'active', createdAt: now, updatedAt: now, version: 1 } as ProjectDTO
  const row = (id: string, title: string): ChatTarget => ({
    project,
    personal: true,
    session: { id, projectId: project.id, title, pinned: false, status: 'active', createdAt: now, updatedAt: now, version: 1 } satisfies SessionDTO,
  })
  render(<LaunchSidebar {...sidebarProps({
    projects: { list: () => new Promise(() => {}) } as unknown as ProjectBridge,
    localChats: [row('open-1', '自动化执行'), row('other-1', '创建技能')],
    visibleDraftId: 'open-1',
  })} />)
  const current = await screen.findByRole('button', { name: '自动化执行' })
  expect(current).toHaveAttribute('aria-current', 'true')
  expect(current.closest('.conversation-row')).toHaveClass('is-current')
  const other = screen.getByRole('button', { name: '创建技能' })
  expect(other).not.toHaveAttribute('aria-current')
  expect(other.closest('.conversation-row')).not.toHaveClass('is-current')
  const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')
  expect(css).toContain('.conversation-row.is-current{margin:1px 6px;border:1px solid rgba(168,198,255,.62);border-radius:8px;background:rgba(92,132,196,.34)}')
  expect(css).toContain('html[data-theme="light"] .conversation-row.is-current{border-color:rgba(0,110,170,.55);background:rgba(0,120,186,.16)}')
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
