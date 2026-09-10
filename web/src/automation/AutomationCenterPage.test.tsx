import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { AutomationBridge } from '../bridge/client'
import type { AutomationRunListResult, AutomationStatusResult, SessionDTO } from '../generated/bridge'
import { AutomationCenterPage, type AutomationViewState } from './AutomationCenterPage'
import { localAutomationTimezone } from './AutomationRunControls'

vi.mock('./ensureAutomationRunner', () => ({
  // A slow model/setup request must not block rendering existing runs.
  ensureAutomationRunner: vi.fn(() => new Promise(() => {})),
  loadDefaultModel: vi.fn(() => new Promise(() => {})),
}))

afterEach(() => { cleanup(); vi.useRealTimers() })
const id = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
type Run = AutomationRunListResult['runs'][number]
const run = (overrides: Partial<Run> = {}): Run => ({
  id: 'run-one', jobId: id, jobName: '每日新闻', state: 'failed', trigger: 'manual',
  startedAt: '2026-09-07T00:55:24Z', finishedAt: '2026-09-07T00:57:24Z',
  totalTokens: 0, summary: '已完成第一条新闻的整理', error: 'context deadline exceeded', outcomeUnknown: true,
  ...overrides,
})

function setup(runs: Run[], running: string[] = []) {
  const status: AutomationStatusResult = { running: true, runningJobs: running as never, lastHeartbeat: '2026-09-07T00:55:24Z', nextFire: {} }
  const listRuns = vi.fn(async () => ({ runs }))
  const statusCall = vi.fn(async () => status)
  const bridge = { listJobs: vi.fn(async () => ({ jobs: [] })), listRuns, status: statusCall } as unknown as AutomationBridge
  return { bridge, listRuns, statusCall, status }
}

it('shows stopped timeout and partial output even while model setup is still pending', async () => {
  const api = setup([run()])
  render(<AutomationCenterPage bridge={api.bridge} onCreateInChat={vi.fn()} />)
  await screen.findByText('定时调度已开启 · 当前无任务执行')
  fireEvent.click(screen.getByRole('button', { name: '执行历史' }))
  fireEvent.click(await screen.findByRole('button', { name: /每日新闻/ }))
  expect(screen.getByText('执行已停止 · 结果待核对')).toBeInTheDocument()
  expect(screen.getByRole('alert')).toHaveTextContent('执行超时，任务已停止')
  expect(screen.getByText('已完成第一条新闻的整理')).toBeInTheDocument()
  expect(screen.queryByText('调度器运行中')).toBeNull()
})

it('opens the actual isolated execution session without rerunning the job', async () => {
  const session = { id, projectId: id, title: '新对话', version: 1, pinned: false, createdAt: '2026-09-07T00:55:24Z', updatedAt: '2026-09-07T00:55:24Z' } as SessionDTO
  const api = setup([run({ session, sessionId: id })])
  const open = vi.fn()
  render(<AutomationCenterPage bridge={api.bridge} onCreateInChat={vi.fn()} onOpenSession={open} />)
  await screen.findByText('定时调度已开启 · 当前无任务执行')
  fireEvent.click(screen.getByRole('button', { name: '执行历史' }))
  fireEvent.click(await screen.findByRole('button', { name: /每日新闻/ }))
  fireEvent.click(screen.getByRole('button', { name: '查看完整对话与产物' }))
  await waitFor(() => expect(open).toHaveBeenCalledWith(session))
})

it('refreshes an active task promptly and removes its running state after completion', async () => {
  vi.useFakeTimers()
  const api = setup([run({ state: 'running', finishedAt: undefined, error: undefined, outcomeUnknown: false })], [id])
  render(<AutomationCenterPage bridge={api.bridge} onCreateInChat={vi.fn()} />)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(screen.getByText('正在执行 1 个任务')).toBeInTheDocument()
  api.status.runningJobs = []
  api.listRuns.mockResolvedValue({ runs: [run({ state: 'succeeded', outcomeUnknown: false, error: undefined })] })
  await act(async () => { await vi.advanceTimersByTimeAsync(3_100) })
  expect(screen.getByText('定时调度已开启 · 当前无任务执行')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '执行历史' }))
  expect(screen.getByText('成功')).toBeInTheDocument()
})

it('stops an expanded execution and shows cancellation with its retained partial result', async()=>{
  const active=run({state:'running',finishedAt:undefined,error:undefined,outcomeUnknown:false})
  const api=setup([active],[id])
  const cancelRun=vi.fn(async()=>{
    api.status.runningJobs=[]
    api.listRuns.mockResolvedValue({runs:[run({cancelled:true,error:'本次执行已停止'})]})
    return {cancellationRequested:true}
  })
  api.bridge.cancelRun=cancelRun
  render(<AutomationCenterPage bridge={api.bridge} onCreateInChat={vi.fn()}/> )
  await screen.findByText('正在执行 1 个任务')
  fireEvent.click(screen.getByRole('button',{name:'执行历史'}))
  fireEvent.click(await screen.findByRole('button',{name:/每日新闻/}))
  fireEvent.click(screen.getByRole('button',{name:'停止 每日新闻'}))
  await screen.findByText('已停止本次执行')
  expect(cancelRun).toHaveBeenCalledExactlyOnceWith({jobId:id,runId:active.id})
  expect(screen.getByText('已完成第一条新闻的整理')).toBeInTheDocument()
  expect(screen.queryByRole('button',{name:'停止 每日新闻'})).toBeNull()
})

it('does not show raw English refresh failures', async () => {
  const bridge = {
    listJobs: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
    listRuns: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
    status: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
  } as unknown as AutomationBridge
  render(<AutomationCenterPage bridge={bridge} onCreateInChat={vi.fn()} />)
  expect(await screen.findByText('自动化刷新失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('defaults newly created tasks to the system timezone',async()=>{
  const api=setup([])
  render(<AutomationCenterPage bridge={api.bridge} onCreateInChat={vi.fn()}/> )
  fireEvent.click(screen.getByRole('button',{name:'手动新建'}))
  expect(screen.getByRole('combobox',{name:'任务时区'})).toHaveValue(localAutomationTimezone())
})

it('restores the expanded execution history after opening its actual session and remounting', async () => {
  const session = { id, projectId: id, title: '执行对话', version: 1, pinned: false, createdAt: '2026-09-07T00:55:24Z', updatedAt: '2026-09-07T00:55:24Z' } as SessionDTO
  const api = setup([run({ session, sessionId: id })])
  const open = vi.fn()
  let view: AutomationViewState | undefined
  const saveView = (next: AutomationViewState) => { view = next }
  const first = render(<AutomationCenterPage bridge={api.bridge} onCreateInChat={vi.fn()} onOpenSession={open} onViewStateChange={saveView} />)
  await screen.findByText('定时调度已开启 · 当前无任务执行')
  fireEvent.click(screen.getByRole('button', { name: '执行历史' }))
  fireEvent.click(await screen.findByRole('button', { name: /每日新闻/ }))
  fireEvent.click(screen.getByRole('button', { name: '查看完整对话与产物' }))
  await waitFor(() => expect(open).toHaveBeenCalledExactlyOnceWith(session))
  expect(view).toEqual({ tab: 'runs', openRun: 'run-one' })
  const readsBeforeReturn = api.listRuns.mock.calls.length
  first.unmount()
  render(<AutomationCenterPage bridge={api.bridge} initialViewState={view} onViewStateChange={saveView} onCreateInChat={vi.fn()} onOpenSession={open} />)
  expect(screen.getByText('正在读取执行历史…')).toBeInTheDocument()
  expect(await screen.findByText('已完成第一条新闻的整理')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /每日新闻/ })).toHaveAttribute('aria-expanded', 'true')
  expect(api.listRuns.mock.calls.length).toBeGreaterThan(readsBeforeReturn)
  expect(open).toHaveBeenCalledOnce()
})

it('does not retain old execution data if the refreshed scope has no matching run', async () => {
  const api = setup([])
  render(<AutomationCenterPage bridge={api.bridge} initialViewState={{ tab: 'runs', openRun: 'old-private-run' }} onCreateInChat={vi.fn()} />)
  expect(await screen.findByText('还没有运行记录。')).toBeInTheDocument()
  expect(screen.queryByText('已完成第一条新闻的整理')).toBeNull()
})
