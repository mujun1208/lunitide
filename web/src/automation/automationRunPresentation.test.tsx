import {cleanup, render, screen, waitFor} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import type {ChatUsageBridge} from '../bridge/client'
import type {AutomationRunListResult, SessionDTO} from '../generated/bridge'
import {AutomationRunDetail} from './automationRunPresentation'

afterEach(cleanup)

const session = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', projectId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  title: '自动化执行', version: 1, pinned: false, status: 'active',
  createdAt: '2026-09-07T00:55:24Z', updatedAt: '2026-09-07T00:57:24Z',
} as SessionDTO

const run = (overrides: Partial<AutomationRunListResult['runs'][number]> = {}): AutomationRunListResult['runs'][number] => ({
  id: 'run-one', jobId: 'job-one', jobName: '新闻整理', state: 'succeeded', trigger: 'manual',
  startedAt: '2026-09-07T00:55:24Z', finishedAt: '2026-09-07T00:57:24Z',
  totalTokens: 0, session, sessionId: session.id, ...overrides,
})

it('shows isolated-session usage on the run detail and never invents parent-chat spend', async () => {
  const get = vi.fn().mockResolvedValue({
    sessionId: session.id, collected: true, integrity: 'reported',
    inputTokens: 12, outputTokens: 3,
    attempts: [{callId: 'c1', attemptId: 'a1', purpose: 'automation', status: 'succeeded', integrity: 'reported', inputTokens: 12, outputTokens: 3}],
  })
  render(<AutomationRunDetail run={run()} usageApi={{get} as unknown as ChatUsageBridge} />)
  await waitFor(() => expect(get).toHaveBeenCalledWith({sessionId: session.id}))
  const text = screen.getByRole('status').textContent ?? ''
  expect(text).toContain('输入 12')
  expect(text).toContain('自动化')
  expect(text).not.toContain('%')
  expect(text).not.toContain('parent')
})

it('omits a usage bar when the run has no isolated session', () => {
  render(<AutomationRunDetail run={run({session: undefined, sessionId: undefined})} />)
  expect(screen.queryByRole('status')).toBeNull()
})

it('does not show raw English scheduler errors on a failed run', () => {
  render(<AutomationRunDetail run={run({state: 'failed', error: 'sql: database is locked'})} />)
  const alert = screen.getByRole('alert')
  expect(alert.textContent).toMatch(/未完成|查看/)
  expect(alert.textContent).not.toMatch(/sql:|database is locked/)
  expect(alert.getAttribute('title') ?? '').not.toMatch(/sql:|database is locked/)
})
