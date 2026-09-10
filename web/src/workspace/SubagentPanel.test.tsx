import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { SubagentPanel } from './SubagentPanel'

vi.mock('../bridge/client', () => ({
  subagentBridge: { tree: vi.fn(), join: vi.fn() },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it('does not show raw English subagent list or summary failures', async () => {
  const { subagentBridge } = await import('../bridge/client')
  vi.mocked(subagentBridge.tree).mockRejectedValue(new Error('Failed to fetch'))
  render(<SubagentPanel sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" />)
  expect(await screen.findByRole('alert')).toHaveTextContent('子智能体列表载入失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  vi.mocked(subagentBridge.tree).mockResolvedValue({
    subagents: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', purpose: '[调研] 核对来源', status: 'completed', spentTokens: 8, observationCount: 1 }],
  })
  vi.mocked(subagentBridge.join).mockRejectedValue(new Error('Failed to fetch'))
  render(<SubagentPanel sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" />)
  fireEvent.click(await screen.findByRole('button', { name: '查看摘要' }))
  expect(await screen.findByText('读取摘要失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
