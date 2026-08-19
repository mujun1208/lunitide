import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { PersonalIntelligencePage } from './PersonalIntelligencePage'

afterEach(cleanup)
vi.mock('../bridge/client', () => ({ projectBridge: { list: vi.fn().mockRejectedValue(new Error('unavailable')) } }))
vi.mock('../memory/MemoryPage', () => ({ MemoryPage: () => <div data-testid="memory-page-stub" /> }))
vi.mock('../memory/MemoryOpsPanel', () => ({ MemoryOpsPanel: () => <div data-testid="memory-ops-stub" /> }))
vi.mock('../workspace/AutomationPanel', () => ({ AutomationPanel: ({ mode }: { mode?: string }) => <div data-testid={`automation-stub-${mode ?? 'full'}`} /> }))
vi.mock('./HandoffConsole', () => ({ HandoffConsole: () => <div data-testid="handoff-console-stub" /> }))
vi.mock('./PrivacyConsole', () => ({ PrivacyConsole: () => <div data-testid="privacy-console-stub" /> }))

it('renders the M8 navigation with live badges and embeds the memory inbox', () => {
  render(<PersonalIntelligencePage />)
  expect(screen.getByText('个人智能')).toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: /记忆收件箱/ })[0]).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /KnowledgeBase/ })).toBeInTheDocument()
  expect(screen.getByText(/M8 · 已实现/)).toBeInTheDocument()
})

it('switches to the facts tab embedding the ops panel', () => {
  render(<PersonalIntelligencePage />)
  fireEvent.click(screen.getAllByRole('button', { name: /已确认事实/ })[0])
  expect(screen.getByTestId('memory-ops-stub')).toBeInTheDocument()
})

it('wires live consoles for handoff, automation, runs and privacy', () => {
  render(<PersonalIntelligencePage />)
  fireEvent.click(screen.getByRole('button', { name: /^Handoff$/ }))
  expect(screen.getByTestId('handoff-console-stub')).toBeInTheDocument()
  expect(document.querySelector('.blocked-banner')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /^自动化$/ }))
  expect(screen.getByTestId('automation-stub-full')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /^运行中心$/ }))
  expect(screen.getByTestId('automation-stub-runs')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /隐私与设备/ }))
  expect(screen.getByTestId('privacy-console-stub')).toBeInTheDocument()
  expect(document.querySelector('.screen-route[data-route="/privacy/devices"]')).toBeNull()
})

it('planned sub-domains render concept contracts without enableable controls', () => {
  render(<PersonalIntelligencePage />)
  fireEvent.click(screen.getByRole('button', { name: /KnowledgeBase/ }))
  expect(screen.getAllByText(/概念预览/).length).toBeGreaterThanOrEqual(1)
  expect(document.querySelector('.screen-route[data-route="/knowledge"]')).not.toBeNull()
  expect(screen.getByText('indexing')).toBeInTheDocument()
})

it('experts tab offers navigation to the expert center', () => {
  const onNavigateExpert = vi.fn()
  render(<PersonalIntelligencePage onNavigateExpert={onNavigateExpert} />)
  fireEvent.click(screen.getAllByRole('button', { name: /专家中心/ })[0])
  fireEvent.click(screen.getByRole('button', { name: /前往专家中心/ }))
  expect(onNavigateExpert).toHaveBeenCalledOnce()
})
