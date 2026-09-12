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

it('hides Agent Hub until the office menu switch is on, then navigates without activating Settings', async () => {
  render(<LaunchSidebar {...sidebarProps()} />)
  expect(screen.queryByRole('button', { name: 'Agent 调度台' })).toBeNull()
  localStorage.setItem('lunitide:office-menu', JSON.stringify({ agentHub: true }))
  cleanup()
  const setPage = vi.fn()
  render(<LaunchSidebar {...sidebarProps({ setPage, page: 'agentHub' })} />)
  const button = screen.getByRole('button', { name: 'Agent 调度台' })
  expect(button.className).toMatch(/active/)
  expect(screen.getByRole('button', { name: '设置' }).className).not.toMatch(/active/)
  cleanup()
  render(<LaunchSidebar {...sidebarProps({ setPage })} />)
  screen.getByRole('button', { name: 'Agent 调度台' }).click()
  expect(setPage).toHaveBeenCalledWith('agentHub')
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
  expect(slot.parentElement).toBe(document.getElementById('launch-sidebar'))
})
