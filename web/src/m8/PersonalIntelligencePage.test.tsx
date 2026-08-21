import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { PersonalIntelligencePage } from './PersonalIntelligencePage'

afterEach(cleanup)
vi.mock('../bridge/client', () => ({ projectBridge: { list: vi.fn().mockRejectedValue(new Error('unavailable')) } }))
vi.mock('../memory/MemoryPage', () => ({ MemoryPage: () => <div data-testid="memory-page-stub" /> }))
vi.mock('./PrivacyConsole', () => ({ PrivacyConsole: () => <div data-testid="privacy-console-stub" /> }))

it('keeps memory and privacy as user-facing tabs and hides ontology', () => {
  render(<PersonalIntelligencePage />)
  expect(screen.getByRole('heading', { name: '个人智能' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: '记忆确认' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: '隐私' })).toBeInTheDocument()
  expect(screen.queryByRole('tab', { name: '本体' })).toBeNull()
  expect(screen.queryByRole('button', { name: /KnowledgeBase/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /^Handoff$/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /^自动化$/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /专家中心/ })).toBeNull()
})

it('switches to the privacy panel', () => {
  render(<PersonalIntelligencePage />)
  fireEvent.click(screen.getByRole('tab', { name: '隐私' }))
  expect(screen.getByTestId('privacy-console-stub')).toBeInTheDocument()
})
