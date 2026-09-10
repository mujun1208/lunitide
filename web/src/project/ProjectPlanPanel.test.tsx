import React from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PlanBridge, StageBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import { ProjectPlanPanel } from './ProjectPlanPanel'

afterEach(cleanup)
const project = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Project', type: 'implementation', status: 'in_progress', version: 1 } as ProjectDTO
const props = { project, phase: 5, checklistPhase: 5, checklistType: 'dev_checklist', checklistTitle: '开发清单' }
const api = () => ({
  bridge: { list: vi.fn().mockResolvedValue({ items: [] }), get: vi.fn(), startRun: vi.fn(), retryRun: vi.fn(), cancelRun: vi.fn(), create: vi.fn(), createNode: vi.fn(), activate: vi.fn(), listNodes: vi.fn().mockResolvedValue({ items: [] }), runTree: vi.fn().mockResolvedValue({ items: [] }) },
  stages: { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn() },
})
const cast = (value: ReturnType<typeof api>) => ({ bridge: value.bridge as unknown as PlanBridge, stages: value.stages as unknown as StageBridge })

it('does not show raw English plan load failures', async () => {
  const value = api()
  value.stages.list.mockRejectedValue(new Error('Failed to fetch'))
  render(<ProjectPlanPanel {...props} {...cast(value)} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

describe('stage-bound plan records', () => {
  it('performs no writes when a read-only project has no phase plan', async () => {
    const value = api()
    render(<ProjectPlanPanel {...props} {...cast(value)} readOnly />)
    await waitFor(() => expect(value.stages.list).toHaveBeenCalled())
    expect(value.stages.create).not.toHaveBeenCalled()
    expect(value.bridge.create).not.toHaveBeenCalled()
    expect(value.bridge.createNode).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '回写清单状态' })).not.toBeInTheDocument()
    expect(screen.getByText(/只有工具回执与实际产物验证通过/)).toBeInTheDocument()
    expect(value.bridge.startRun).not.toHaveBeenCalled()
  })

  it('starts a real task explicitly and shows its verified result after polling', async () => {
    const value = api()
    const task = { id: 'task', todo: { title: '生成报告' }, status: 'queued', depth: 0 }
    value.stages.list.mockResolvedValue({ items: [{ id: 'stage-five', phase: 5 }] })
    value.bridge.list.mockResolvedValue({ items: [{ id: 'plan', stageId: 'stage-five', status: 'draft' }] })
    value.bridge.get.mockResolvedValue({ id: 'plan', status: 'draft' })
    value.bridge.runTree.mockResolvedValueOnce({ items: [task] }).mockResolvedValueOnce({ items: [{ ...task, status: 'running', executionStatus: 'running' }] }).mockResolvedValue({ items: [{ ...task, status: 'succeeded', executionStatus: 'succeeded', summary: '报告完成', artifacts: [{ path: 'report.md', sha256: 'a'.repeat(64), bytes: 100 }] }] })
    render(<ProjectPlanPanel {...props} {...cast(value)} />)
    fireEvent.click(await screen.findByRole('button', { name: '启动执行' }))
    await waitFor(() => expect(value.bridge.startRun).toHaveBeenCalledWith({ runId: 'task' }))
    expect(value.bridge.activate).toHaveBeenCalledWith({ planId: 'plan' })
    await waitFor(() => expect(screen.getByText('产物已验证')).toBeInTheDocument(), { timeout: 3000 })
    expect(screen.getByText('报告完成')).toBeInTheDocument()
    expect(screen.getByText(/report.md · 100 字节/)).toBeInTheDocument()
  })

  it('requires explicit acknowledgment before retrying an uncertain execution', async () => {
    const value = api()
    const task = { id: 'task', todo: { title: '结果未知的任务' }, status: 'failed', executionStatus: 'outcome_unknown', executionVersion: 2, depth: 0 }
    value.stages.list.mockResolvedValue({ items: [{ id: 'stage-five', phase: 5 }] })
    value.bridge.list.mockResolvedValue({ items: [{ id: 'plan', stageId: 'stage-five' }] })
    value.bridge.runTree.mockResolvedValue({ items: [task] })
    render(<ProjectPlanPanel {...props} {...cast(value)} />)
    const retry = await screen.findByRole('button', { name: '重新执行' })
    expect(retry).toBeDisabled()
    fireEvent.click(screen.getByRole('checkbox', { name: '已核对前次操作，允许新建任务执行' }))
    fireEvent.click(retry)
    await waitFor(() => expect(value.bridge.retryRun).toHaveBeenCalledWith({ runId: 'task', expectedVersion: 2, acknowledgeUncertain: true }))
  })

  it('binds by stage id and lets the server create the plan and root together', async () => {
    const value = api()
    value.stages.list.mockResolvedValue({ items: [{ id: 'stage-five', phase: 5 }] })
    value.bridge.list.mockResolvedValue({ items: [{ id: 'wrong-plan', stageId: 'stage-two', name: '阶段5 misleading name' }] })
    value.bridge.create.mockResolvedValue({ plan: { id: 'right-plan', stageId: 'stage-five' } })
    value.bridge.listNodes.mockResolvedValue({ items: [{ id: 'root', sequence: 1 }] })
    render(<ProjectPlanPanel {...props} {...cast(value)} />)
    await waitFor(() => expect(value.bridge.listNodes).toHaveBeenCalledWith({ planId: 'right-plan' }))
    expect(value.bridge.create.mock.calls[0][0]).toMatchObject({ projectId: project.id, stageId: 'stage-five' })
    expect(value.bridge.activate).not.toHaveBeenCalled()
    expect(value.bridge.createNode).not.toHaveBeenCalled()
  })

  it('ignores a response from a phase that has already been left', async () => {
    const value = api()
    let resolveOld!: (value: { items: { id: string; phase: number }[] }) => void
    value.stages.list.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    value.stages.list.mockResolvedValue({ items: [{ id: 'stage-six', phase: 6 }] })
    value.bridge.list.mockResolvedValue({ items: [{ id: 'new-plan', stageId: 'stage-six' }] })
    value.bridge.runTree.mockResolvedValue({ items: [{ id: 'new-run', todo: { title: 'new-phase-task' }, status: 'pending', depth: 0 }] })
    const view = render(<ProjectPlanPanel {...props} {...cast(value)} readOnly />)
    view.rerender(<ProjectPlanPanel {...props} phase={6} {...cast(value)} readOnly />)
    await screen.findByText('new-phase-task')
    await act(async () => { resolveOld({ items: [{ id: 'stage-five', phase: 5 }] }) })
    expect(screen.getByText('new-phase-task')).toBeInTheDocument()
    expect(value.bridge.list).toHaveBeenCalledTimes(1)
    expect(value.bridge.create).not.toHaveBeenCalled()
  })
})
