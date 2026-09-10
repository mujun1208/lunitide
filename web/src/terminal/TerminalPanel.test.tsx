import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { TerminalBridge } from '../bridge/client'
import { TerminalPanel } from './TerminalPanel'

afterEach(cleanup)

it('does not show raw English start failures', async () => {
  const bridge = { start: vi.fn().mockRejectedValue(new Error('Failed to fetch')), dispose: vi.fn() } as unknown as TerminalBridge
  render(<TerminalPanel projectId="p" sessionId="s" bridge={bridge} />)
  fireEvent.click(screen.getByRole('button', { name: '启动交互终端' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('终端启动失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('treats cancel as silent', async () => {
  const bridge = { start: vi.fn().mockRejectedValue(new Error('用户取消')), dispose: vi.fn() } as unknown as TerminalBridge
  render(<TerminalPanel projectId="p" sessionId="s" bridge={bridge} />)
  fireEvent.click(screen.getByRole('button', { name: '启动交互终端' }))
  await screen.findByRole('status')
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.queryByText('用户取消')).toBeNull()
})
