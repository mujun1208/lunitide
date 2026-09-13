import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { agentHubApi } from '../agentHub/agentHubApi'
import type { ProjectDTO } from '../generated/bridge'
import { projectSpineApi } from './projectSpineApi'
import { hubWorkspaceRoot, ProjectDevBar } from './ProjectDevBar'

vi.mock('../agentHub/agentHubApi', () => ({
  agentHubApi: {
    detect: vi.fn().mockResolvedValue({
      agents: [
        { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
        { name: 'codex', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '' },
      ],
    }),
    threadGet: vi.fn(),
  },
}))

vi.mock('./projectSpineApi', () => ({
  projectSpineApi: {
    executorSet: vi.fn(),
    taskReport: vi.fn(),
    taskComplete: vi.fn(),
    taskOpen: vi.fn(),
  },
}))

afterEach(cleanup)

it('disables Cursor when DetectAll says it is not available', async () => {
  render(
    <ProjectDevBar
      project={{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Mall',
        projectCode: 'ITM00001',
        type: 'implementation',
        status: 'in_progress',
        version: 1,
        defaultExecutor: 'lunitide',
      } as ProjectDTO}
    />,
  )
  await waitFor(() => expect(screen.getByRole('button', { name: /Codex 对接/ })).toBeEnabled())
  const cursor = screen.getByRole('button', { name: /Cursor 对接/ })
  expect(cursor).toBeDisabled()
  expect(cursor).toHaveAttribute('title', '未检测到，装好并登录后可选')
  expect(screen.getByRole('button', { name: '月汐平台' })).toBeEnabled()
})

it('reports a faulted hub thread without completing the task', async () => {
  vi.mocked(agentHubApi.threadGet).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAT',
      harnessId: 'cursor',
      nativeSessionId: '',
      title: 'F001',
      pinned: false,
      workspaceRoot: '',
      exportDir: '',
      scene: 'write_project',
      status: 'faulted',
      accessMode: 'approval',
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', seq: 1, role: 'assistant', content: 'CLI 退出 1', createdAt: '2026-09-13T00:00:01Z' }],
    events: [],
    files: [],
  })
  render(
    <ProjectDevBar
      project={{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Mall',
        projectCode: 'ITM00001',
        type: 'implementation',
        status: 'in_progress',
        version: 1,
        defaultExecutor: 'cursor',
      } as ProjectDTO}
      currentItemId="F001"
      hubThreadId="01ARZ3NDEKTSV4RRFFQ69G5FAT"
    />,
  )
  expect(await screen.findByRole('button', { name: '回写失败原因' })).toBeInTheDocument()
  await waitFor(() => expect(screen.getByDisplayValue('CLI 退出 1')).toBeInTheDocument())
  await userEvent.click(screen.getByRole('button', { name: '回写失败原因' }))
  await waitFor(() => expect(projectSpineApi.taskReport).toHaveBeenCalled())
  expect(projectSpineApi.taskComplete).not.toHaveBeenCalled()
})

it('prefills 月汐 summary from the last assistant reply', async () => {
  render(
    <ProjectDevBar
      project={{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Mall',
        projectCode: 'ITM00001',
        type: 'implementation',
        status: 'in_progress',
        version: 1,
        defaultExecutor: 'lunitide',
      } as ProjectDTO}
      currentItemId="F001"
      lastAssistant="写了登录接口"
    />,
  )
  expect(await screen.findByDisplayValue('写了登录接口')).toBeInTheDocument()
})

it('does not complete a task until the self-test checkbox is checked', async () => {
  render(
    <ProjectDevBar
      project={{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Mall',
        projectCode: 'ITM00001',
        type: 'implementation',
        status: 'in_progress',
        version: 1,
        defaultExecutor: 'lunitide',
        dbStatus: 'ready',
      } as ProjectDTO}
      currentItemId="F001"
    />,
  )
  await userEvent.type(await screen.findByPlaceholderText('回写结果摘要（确认后才算完成）'), '写了登录')
  await userEvent.click(screen.getByRole('button', { name: '回写结果并完成' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('已对照功能详细设计自测通过')
  expect(projectSpineApi.taskComplete).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('checkbox', { name: '已对照功能详细设计自测通过' }))
  await userEvent.click(screen.getByRole('button', { name: '回写结果并完成' }))
  await waitFor(() => expect(projectSpineApi.taskComplete).toHaveBeenCalledWith({
    projectId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    itemId: 'F001',
    selfTestPass: true,
  }))
})

it('uses the project root as the hub workspace', () => {
  expect(hubWorkspaceRoot({ rootPath: 'D:\\work\\mall' } as ProjectDTO)).toBe('D:\\work\\mall')
  expect(hubWorkspaceRoot({} as ProjectDTO)).toBe('')
})

it('shows the hard-gate copy when the interface phase is not confirmed', async () => {
  render(
    <ProjectDevBar
      project={{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Mall',
        projectCode: 'ITM00001',
        type: 'implementation',
        status: 'in_progress',
        version: 1,
        defaultExecutor: 'lunitide',
        dbStatus: 'ready',
      } as ProjectDTO}
      interfaceReady={false}
    />,
  )
  expect(await screen.findByText('先完成库表核齐与接口阶段确认，才能开始开发任务。')).toBeInTheDocument()
})

it('shows the hard-gate copy when the project database is not ready', async () => {
  render(
    <ProjectDevBar
      project={{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Mall',
        projectCode: 'ITM00001',
        type: 'implementation',
        status: 'in_progress',
        version: 1,
        defaultExecutor: 'lunitide',
        dbStatus: 'none',
      } as ProjectDTO}
    />,
  )
  expect(await screen.findByText('先完成库表核齐与接口阶段确认，才能开始开发任务。')).toBeInTheDocument()
})
