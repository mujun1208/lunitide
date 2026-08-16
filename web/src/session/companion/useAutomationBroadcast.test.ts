// useAutomationBroadcast.test.ts pins the P3-4 automation→TTS linkage
// rules: baseline runs are never replayed, only terminal runs
// broadcast (running ones wait), busy or disabled stages stay silent,
// and multi-run bursts collapse into one brief combined utterance.
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AutomationRunListResult } from '../../generated/bridge'
import { buildBroadcastText, useAutomationBroadcast } from './useAutomationBroadcast'

type Run = AutomationRunListResult['runs'][number]

afterEach(cleanup)

const run = (overrides: Partial<Run>): Run => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  jobId: '01ARZ3NDEKTSV4RRFFQ69G5FAK',
  jobName: '每日站会摘要',
  state: 'succeeded',
  trigger: 'cron',
  summary: '今日待办 3 项',
  startedAt: '2026-08-16T00:30:00Z',
  finishedAt: '2026-08-16T00:30:12Z',
  ...overrides,
})

const makeBridge = (runs: () => Run[]) => ({ listRuns: vi.fn().mockImplementation(() => Promise.resolve({ runs: runs() })) })

const renderBroadcast = (options: {
  runs: () => Run[]
  enabled?: boolean
  idle?: () => boolean
  onBroadcast?: ReturnType<typeof vi.fn>
}) => {
  const onBroadcast: ReturnType<typeof vi.fn> = options.onBroadcast ?? vi.fn()
  const bridge = makeBridge(options.runs)
  const utils = renderHook(() =>
    useAutomationBroadcast({
      bridge,
      intervalMs: 10,
      enabled: options.enabled ?? true,
      idle: options.idle ?? (() => true),
      onBroadcast: (text: string) => onBroadcast(text),
    }),
  )
  return { ...utils, bridge, onBroadcast }
}

const awaitBaseline = async (onBroadcast: ReturnType<typeof vi.fn>) => {
  await waitFor(() => expect(onBroadcast).not.toHaveBeenCalled())
}

it('marks the existing terminal runs as the baseline and never replays them', async () => {
  const rows = [run({})]
  const { onBroadcast } = renderBroadcast({ runs: () => rows })
  await awaitBaseline(onBroadcast)
  // Let several 10ms polls fire over the unchanged history.
  await new Promise(resolve => setTimeout(resolve, 60))
  expect(onBroadcast).not.toHaveBeenCalled()
})

it('broadcasts a newly succeeded run with the summary first line', async () => {
  const rows: Run[] = [run({})]
  const { onBroadcast } = renderBroadcast({ runs: () => rows })
  await awaitBaseline(onBroadcast)
  rows.unshift(run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F01', jobName: '周报生成', summary: '第一行摘要\n第二行详情' }))
  await waitFor(() => expect(onBroadcast).toHaveBeenCalledTimes(1))
  expect(onBroadcast.mock.calls[0][0]).toContain('自动化任务「周报生成」已完成。第一行摘要')
  expect(onBroadcast.mock.calls[0][0]).not.toContain('第二行详情')
})

it('broadcasts a failed run with the error first line', async () => {
  const rows: Run[] = []
  const { onBroadcast } = renderBroadcast({ runs: () => rows })
  await awaitBaseline(onBroadcast)
  rows.push(run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F02', state: 'failed', error: '模型网关超时\n详情略' }))
  await waitFor(() => expect(onBroadcast).toHaveBeenCalledTimes(1))
  expect(onBroadcast.mock.calls[0][0]).toContain('自动化任务「每日站会摘要」执行失败。模型网关超时')
})

it('ignores runs still running until they reach a terminal state', async () => {
  const rows: Run[] = []
  const { onBroadcast } = renderBroadcast({ runs: () => rows })
  await awaitBaseline(onBroadcast)
  rows.push(run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F03', state: 'running', summary: undefined, finishedAt: undefined }))
  await new Promise(resolve => setTimeout(resolve, 60))
  expect(onBroadcast).not.toHaveBeenCalled()
  const [row] = rows
  row.state = 'succeeded'
  await waitFor(() => expect(onBroadcast).toHaveBeenCalledTimes(1))
})

it('stays silent when disabled or the stage is busy', async () => {
  const disabled = vi.fn()
  renderBroadcast({ runs: () => [run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F04' })], enabled: false, onBroadcast: disabled })
  await new Promise(resolve => setTimeout(resolve, 60))
  expect(disabled).not.toHaveBeenCalled()

  const busy = vi.fn()
  const rows: Run[] = []
  renderBroadcast({ runs: () => rows, idle: () => false, onBroadcast: busy })
  await new Promise(resolve => setTimeout(resolve, 40))
  rows.push(run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F05' }))
  await new Promise(resolve => setTimeout(resolve, 60))
  expect(busy).not.toHaveBeenCalled()
})

it('combines a burst of new terminal runs into one utterance', async () => {
  const rows: Run[] = []
  const { onBroadcast } = renderBroadcast({ runs: () => rows })
  await awaitBaseline(onBroadcast)
  for (let index = 0; index < 5; index++) rows.push(run({ id: `01ARZ3NDEKTSV4RRFFQ69G5F1${index}`, jobName: `任务${index}` }))
  await waitFor(() => expect(onBroadcast).toHaveBeenCalledTimes(1))
  const text = onBroadcast.mock.calls[0][0] as string
  expect(text).toContain('任务0')
  expect(text).toContain('任务2')
  expect(text).not.toContain('任务3已完成')
  expect(text).toContain('其余 2 个结果请查看运行历史')
})

it('swallows bridge failures and keeps polling', async () => {
  let fail = true
  const listRuns = vi.fn().mockImplementation(() => (fail ? Promise.reject(new Error('bridge down')) : Promise.resolve({ runs: [run({ id: '01ARZ3NDEKTSV4RRFFQ69G5F20' })] })))
  const onBroadcast = vi.fn()
  renderHook(() => useAutomationBroadcast({ bridge: { listRuns: listRuns as never }, intervalMs: 10, enabled: true, idle: () => true, onBroadcast }))
  await new Promise(resolve => setTimeout(resolve, 40))
  expect(onBroadcast).not.toHaveBeenCalled()
  fail = false
  // First successful poll after failures establishes the baseline.
  await new Promise(resolve => setTimeout(resolve, 60))
  expect(onBroadcast).not.toHaveBeenCalled()
  expect(listRuns.mock.calls.length).toBeGreaterThan(2)
})

describe('buildBroadcastText', () => {
  it('clips long details mid-rune-safely', () => {
    const text = buildBroadcastText([run({ summary: '长'.repeat(300) })])
    const detail = text.replace('自动化任务「每日站会摘要」已完成。', '')
    expect(Array.from(detail).length).toBeLessThanOrEqual(122)
    expect(detail.endsWith('。')).toBe(true)
  })

  it('omits the detail sentence when the run has no summary', () => {
    expect(buildBroadcastText([run({ summary: undefined })])).toBe('自动化任务「每日站会摘要」已完成。')
  })
})
