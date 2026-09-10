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
