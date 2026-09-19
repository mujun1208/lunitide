import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubShellSwitch } from './AgentHubShellSwitch'

afterEach(() => {
  cleanup()
})

it('shows Chat / Work and calls onAgents without setting a personal target', () => {
  const onLunitide = vi.fn()
  const onAgents = vi.fn()
  const onToggleDrawer = vi.fn()
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubShellSwitch mode="lunitide" onLunitide={onLunitide} onAgents={onAgents} onToggleDrawer={onToggleDrawer} drawerOpen />
    </LanguageProvider>,
  )
  expect(screen.getByRole('group', { name: 'Chat / Work' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Work' }))
  expect(onAgents).toHaveBeenCalledTimes(1)
  expect(onLunitide).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '收起左侧栏' }))
  expect(onToggleDrawer).toHaveBeenCalledTimes(1)
})

it('shows Chat / Work and calls onLunitide from the agentHub mode', () => {
  const onLunitide = vi.fn()
  const onAgents = vi.fn()
  render(
    <LanguageProvider value="en">
      <AgentHubShellSwitch mode="agentHub" onLunitide={onLunitide} onAgents={onAgents} />
    </LanguageProvider>,
  )
  expect(screen.getByRole('group', { name: 'Chat / Work' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Chat' }))
  expect(onLunitide).toHaveBeenCalledTimes(1)
  expect(onAgents).not.toHaveBeenCalled()
})
