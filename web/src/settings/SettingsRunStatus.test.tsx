import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'
import type { ActivitySnapshotDTO } from '../generated/bridge'
import { SettingsRunStatus } from './SettingsRunStatus'

afterEach(() => cleanup())

const item = (overrides: Partial<ActivitySnapshotDTO>): ActivitySnapshotDTO => ({
  activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  domain: 'tool',
  kind: 'call',
  phase: 'succeeded',
  terminal: true,
  verificationStatus: 'confirmed',
  verificationSource: 'none',
  title: 'ok',
  completedUnits: null,
  totalUnits: null,
  errorCode: null,
  retryable: false,
  recoveryAction: 'none',
  scopeKind: 'user',
  scopeId: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  ...overrides,
})

it('shows four green lamps when idle and does not expand', () => {
  render(<SettingsRunStatus items={[]} />)
  expect(screen.getByRole('button', { name: /运行状态/ })).toBeDisabled()
  expect(screen.getByText('总览').previousElementSibling).toHaveClass('nav-lamp-ready')
  expect(screen.queryByRole('dialog', { name: '活动' })).toBeNull()
})

it('turns tool and overall yellow when MCP is quarantined', () => {
  render(<SettingsRunStatus items={[]} diagnostics={[{ id: 'mcp', state: 'degraded', code: 'MCP_QUARANTINED' }]} />)
  expect(screen.getByRole('button', { name: /运行状态/ })).not.toBeDisabled()
  expect(screen.getByText('总览').previousElementSibling).toHaveClass('nav-lamp-warn')
  expect(screen.getByText('工具').previousElementSibling).toHaveClass('nav-lamp-warn')
})

it('turns overall yellow on uncertain and red on failed, and expands recover', async () => {
  const user = userEvent.setup()
  const recovered: string[] = []
  const failed = item({
    phase: 'failed',
    terminal: true,
    retryable: true,
    recoveryAction: 'retry',
    title: '夜曲',
    domain: 'media',
  })
  render(<SettingsRunStatus items={[failed]} onRecover={item => recovered.push(item.recoveryAction)} />)
  const toggle = screen.getByRole('button', { name: /运行状态/ })
  expect(toggle).not.toBeDisabled()
  expect(screen.getByText('总览').previousElementSibling).toHaveClass('nav-lamp-error')
  expect(screen.getByText('媒体').previousElementSibling).toHaveClass('nav-lamp-error')
  await user.click(toggle)
  expect(screen.getByRole('dialog', { name: '活动' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '重试' }))
  expect(recovered).toEqual(['retry'])
})
