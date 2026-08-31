import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { ToolTrajectory } from './ToolTrajectory'

afterEach(cleanup)

it('renders an append-only tool list and hides when empty', () => {
  const { rerender } = render(<ToolTrajectory items={[]} />)
  expect(screen.queryByLabelText('工具轨迹')).toBeNull()
  rerender(<ToolTrajectory items={[
    { callId: 'c1', name: 'browser.act', status: 'failed', summary: 'BROWSER_MCP_NOT_READY' },
    { callId: 'c2', name: 'web.fetch', status: 'tool_completed', summary: 'ok' },
  ]} />)
  expect(screen.getByLabelText('工具轨迹')).toBeInTheDocument()
  expect(screen.getByText('browser.act')).toBeInTheDocument()
  expect(screen.getByText(/失败/)).toBeInTheDocument()
  expect(screen.getByText('web.fetch')).toBeInTheDocument()
})
