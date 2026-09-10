import {cleanup, render, screen, waitFor} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import type {OperationBridge} from '../bridge/client'
import {SessionOperationsBar} from './SessionOperations'

afterEach(cleanup)

it('labels pending as 待执行 and unknown as 待核实', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [
      {
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
        state: 'pending', expectedVersion: 1, attempt: 1, resumeAction: 'compare_and_continue',
      },
      {
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', toolName: 'image.generate', effectClass: 'remote_trackable',
        state: 'unknown', expectedVersion: 2, attempt: 1, resumeAction: 'verify_unknown',
      },
      {
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAX', toolName: 'files.apply', effectClass: 'write',
        state: 'partial', expectedVersion: 3, attempt: 1, resumeAction: 'compare_and_continue',
      },
    ],
  })
  const opsApi = {list, get: vi.fn(), cancel: vi.fn(), resume: vi.fn()} as unknown as OperationBridge
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  const text = (await screen.findByRole('status')).textContent ?? ''
  expect(text).toContain('待执行')
  expect(text).toContain('待核实')
  expect(text).toContain('部分完成')
  expect(text.split('待核实').length - 1).toBe(1)
})

it('lists tool operations in Chinese and never invents a resume execution', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [{
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
      state: 'succeeded', expectedVersion: 1, attempt: 1, resumeAction: 'show_existing',
    }],
  })
  const opsApi = {list, get: vi.fn(), cancel: vi.fn(), resume: vi.fn()} as unknown as OperationBridge
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  expect(await screen.findByRole('status')).toHaveTextContent('文件批次')
  expect(screen.getByRole('status').textContent).toContain('已完成')
  expect(screen.getByRole('status').textContent).toContain('查看已有结果')
  expect(screen.getByRole('status').textContent).not.toContain('executed')
  expect(screen.queryByRole('button', {name: '继续执行'})).toBeNull()
  expect(screen.queryByRole('button', {name: '停止'})).toBeNull()
  expect(list).toHaveBeenCalledWith({sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', limit: 20})
})

it('offers stop for unknown and partial so cancel can freeze late receipts', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [
      {
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'image.generate', effectClass: 'remote_trackable',
        state: 'unknown', expectedVersion: 3, attempt: 1, resumeAction: 'verify_unknown',
        externalId: 'supplier-job-8',
      },
      {
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', toolName: 'files.apply', effectClass: 'write',
        state: 'partial', expectedVersion: 4, attempt: 1, resumeAction: 'compare_and_continue',
      },
    ],
  })
  const opsApi = {list, get: vi.fn(), cancel: vi.fn(), resume: vi.fn()} as unknown as OperationBridge
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAX" zh opsApi={opsApi} />)
  expect(await screen.findAllByRole('button', {name: '停止'})).toHaveLength(2)
  expect(screen.queryByRole('button', {name: '继续执行'})).toBeNull()
})

it('offers stop for a pending operation and never offers resume execution', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [{
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
      state: 'pending', expectedVersion: 1, attempt: 1, resumeAction: 'keep_stopped',
    }],
  })
  const opsApi = {list, get: vi.fn(), cancel: vi.fn(), resume: vi.fn()} as unknown as OperationBridge
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  expect(await screen.findByRole('button', {name: '停止'})).toBeInTheDocument()
  expect(screen.queryByRole('button', {name: '继续执行'})).toBeNull()
})

it('cancels a running operation and never offers resume execution', async () => {
  const list = vi.fn()
    .mockResolvedValueOnce({
      items: [{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
        state: 'running', expectedVersion: 2, attempt: 1, resumeAction: 'keep_stopped',
      }],
    })
    .mockResolvedValueOnce({
      items: [{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
        state: 'cancelled', expectedVersion: 3, attempt: 1, resumeAction: 'keep_stopped',
      }],
    })
  const cancel = vi.fn().mockResolvedValue({
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
    state: 'cancelled', expectedVersion: 3, attempt: 1, resumeAction: 'keep_stopped',
  })
  const opsApi = {list, get: vi.fn(), cancel, resume: vi.fn()} as unknown as OperationBridge
  const {userEvent} = await import('@testing-library/user-event')
  const user = userEvent.setup()
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  await user.click(await screen.findByRole('button', {name: '停止'}))
  await waitFor(() => expect(cancel).toHaveBeenCalledWith({
    sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    expectedVersion: 2,
  }))
  expect(opsApi.resume).not.toHaveBeenCalled()
  expect(screen.queryByRole('button', {name: '继续执行'})).toBeNull()
  expect(await screen.findByRole('status')).toHaveTextContent('已取消')
})

it('shows the server cancel conflict in Chinese and never offers resume execution', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [{
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
      state: 'intent', expectedVersion: 4, attempt: 1, resumeAction: 'keep_stopped',
    }],
  })
  const cancel = vi.fn().mockRejectedValue(new Error('操作版本已变化，请重新查询后再取消'))
  const opsApi = {list, get: vi.fn(), cancel, resume: vi.fn()} as unknown as OperationBridge
  const {userEvent} = await import('@testing-library/user-event')
  const user = userEvent.setup()
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  await user.click(await screen.findByRole('button', {name: '停止'}))
  expect(await screen.findByText('操作版本已变化，请重新查询后再取消')).toBeInTheDocument()
  expect(opsApi.resume).not.toHaveBeenCalled()
  expect(screen.queryByRole('button', {name: '继续执行'})).toBeNull()
})

it('does not show raw English cancel failures', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [{
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'files.apply', effectClass: 'write',
      state: 'intent', expectedVersion: 4, attempt: 1, resumeAction: 'keep_stopped',
    }],
  })
  const cancel = vi.fn().mockRejectedValue(new Error('Failed to fetch'))
  const opsApi = {list, get: vi.fn(), cancel, resume: vi.fn()} as unknown as OperationBridge
  const {userEvent} = await import('@testing-library/user-event')
  const user = userEvent.setup()
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  await user.click(await screen.findByRole('button', {name: '停止'}))
  expect(await screen.findByText('取消失败，请重新查询后再试')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('shows a persisted remote job id and tells the user not to regenerate', async () => {
  const list = vi.fn().mockResolvedValue({
    items: [{
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'image.generate', effectClass: 'remote_trackable',
      state: 'unknown', expectedVersion: 3, attempt: 1, resumeAction: 'verify_unknown',
      externalId: 'supplier-job-9', resumeHint: '结果未确认，请先核实，不要自动重做',
    }],
  })
  const opsApi = {list, get: vi.fn(), cancel: vi.fn(), resume: vi.fn()} as unknown as OperationBridge
  render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  const status = await screen.findByRole('status')
  expect(status.textContent).toContain('supplier-job-9')
  expect(status.textContent).toContain('不要自动重做')
  expect(screen.queryByRole('button', {name: '继续执行'})).toBeNull()
})

it('renders nothing when the session has no operations', async () => {
  const opsApi = {list: vi.fn().mockResolvedValue({items: []}), get: vi.fn(), cancel: vi.fn(), resume: vi.fn()} as unknown as OperationBridge
  const {container} = render(<SessionOperationsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh opsApi={opsApi} />)
  await waitFor(() => expect(opsApi.list).toHaveBeenCalled())
  expect(container).toBeEmptyDOMElement()
})
