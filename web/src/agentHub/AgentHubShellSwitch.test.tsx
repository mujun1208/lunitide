import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubShellSwitch } from './AgentHubShellSwitch'

afterEach(() => {
  cleanup()
})

it('shows 月汐 / 外接 Agent and calls onAgents without setting a personal target', () => {
  const onLunitide = vi.fn()
  const onAgents = vi.fn()
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubShellSwitch mode="lunitide" onLunitide={onLunitide} onAgents={onAgents} />
    </LanguageProvider>,
  )
  expect(screen.getByRole('group', { name: '月汐 / 外接 Agent' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '外接 Agent' }))
  expect(onAgents).toHaveBeenCalledTimes(1)
  expect(onLunitide).not.toHaveBeenCalled()
})

it('shows Lunitide / Agents and calls onLunitide from the agentHub mode', () => {
  const onLunitide = vi.fn()
  const onAgents = vi.fn()
  render(
    <LanguageProvider value="en">
      <AgentHubShellSwitch mode="agentHub" onLunitide={onLunitide} onAgents={onAgents} />
    </LanguageProvider>,
  )
  expect(screen.getByRole('group', { name: 'Lunitide / Agents' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Lunitide' }))
  expect(onLunitide).toHaveBeenCalledTimes(1)
  expect(onAgents).not.toHaveBeenCalled()
})
