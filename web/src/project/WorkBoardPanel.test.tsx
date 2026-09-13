import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { WorkBoardPanel } from './WorkBoardPanel'
import { projectFactoryApi } from './projectFactoryApi'

vi.mock('./projectFactoryApi', () => ({
  projectFactoryApi: {
    boardGet: vi.fn(),
    boardSync: vi.fn(),
    boardPut: vi.fn(),
    boardItemOpen: vi.fn(),
    testRun: vi.fn(),
  },
}))

vi.mock('./projectSpineApi', () => ({
  projectSpineApi: {
    returnFromTest: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const project = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: '商场',
  projectCode: 'ITM00001',
  type: 'implementation' as const,
  status: 'in_progress' as const,
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z',
  version: 1,
}

beforeEach(() => {
  vi.mocked(projectFactoryApi.boardGet).mockResolvedValue({
    board: { version: 1, items: [{ id: 'I001', title: '登录', status: 'in_progress', method: 'POST', path: '/login' }] },
    stats: { total: 1, done: 0, needsReprocess: 0 },
    statsText: '共 1 条',
  })
  vi.mocked(projectFactoryApi.boardItemOpen).mockResolvedValue({
    brief: { itemId: 'I001', title: '登录', text: '任务 I001', rootPath: 'D:\\mall' },
    itemId: 'I001',
  })
  vi.mocked(projectFactoryApi.boardPut).mockResolvedValue({ board: {}, stats: {} })
})

it('does not complete without self-test and summary', async () => {
  const user = userEvent.setup()
  render(<WorkBoardPanel project={project} boardKind="interface" title="接口工作台" />)
  await screen.findByText(/I001/)
  await user.click(screen.getByRole('button', { name: '完成' }))
  expect(projectFactoryApi.boardPut).not.toHaveBeenCalled()
  expect(screen.getByRole('alert')).toHaveTextContent('请先勾选自测并填写结果摘要')
})

it('completes after self-test and stays off task.open', async () => {
  const user = userEvent.setup()
  const onOpenItem = vi.fn()
  const onBrief = vi.fn()
  render(<WorkBoardPanel project={project} boardKind="interface" title="接口工作台" onOpenItem={onOpenItem} onBrief={onBrief} />)
  await screen.findByText(/I001/)
  await user.click(screen.getByRole('button', { name: /I001 登录/ }))
  await waitFor(() => expect(projectFactoryApi.boardItemOpen).toHaveBeenCalledWith({
    projectId: project.id,
    boardKind: 'interface',
    itemId: 'I001',
  }))
  expect(onBrief).toHaveBeenCalledWith('任务 I001')
  expect(onOpenItem).toHaveBeenCalledWith('I001')
  await user.click(screen.getByRole('checkbox'))
  await user.type(screen.getByLabelText('I001 结果摘要'), '对照接口详细设计通过')
  await user.click(screen.getByRole('button', { name: '完成' }))
  await waitFor(() => expect(projectFactoryApi.boardPut).toHaveBeenCalled())
  const put = vi.mocked(projectFactoryApi.boardPut).mock.calls[0]?.[0] as { board: { items: Array<{ status: string; selfTestPass?: boolean }> } }
  expect(put.board.items[0]?.status).toBe('dev_done')
  expect(put.board.items[0]?.selfTestPass).toBe(true)
})
