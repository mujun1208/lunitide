import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BridgeClientError } from '../bridge/client'
import { officeStudioApi } from './officeStudioApi'

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return { ...actual, requestOffice: vi.fn() }
})

const { requestOffice } = await import('../bridge/client')

const snapshot = {
  task: { id: 'task', sessionId: 'session', title: '汇报', goal: '', revision: 1, status: 'succeeded' as const, createdAt: '', updatedAt: '' },
  artifacts: [],
  steps: [],
  sources: [],
}

describe('officeStudioApi.sync', () => {
  beforeEach(() => {
    vi.mocked(requestOffice).mockReset()
  })

  it('reads the current task instead of showing OFFICE_BUSY as a conflict', async () => {
    vi.mocked(requestOffice)
      .mockRejectedValueOnce(new BridgeClientError('办公任务正在同步或写入，请稍后重试', 'OFFICE_BUSY', true, 'trace'))
      .mockResolvedValueOnce(snapshot)
    await expect(officeStudioApi.sync({ taskId: 'task' })).resolves.toMatchObject({ task: { id: 'task' } })
    expect(requestOffice).toHaveBeenNthCalledWith(1, 'office.task.sync', { taskId: 'task' })
    expect(requestOffice).toHaveBeenNthCalledWith(2, 'office.task.get', { taskId: 'task' })
  })

  it('does not pretend a busy file import succeeded', async () => {
    const busy = new BridgeClientError('办公任务正在同步或写入，请稍后重试', 'OFFICE_BUSY', true, 'trace')
    vi.mocked(requestOffice).mockRejectedValueOnce(busy)
    await expect(officeStudioApi.sync({ taskId: 'task', artifactPath: '汇报.pptx' })).rejects.toBe(busy)
    expect(requestOffice).toHaveBeenCalledTimes(1)
  })

  it('still surfaces a real version conflict', async () => {
    const conflict = new BridgeClientError('同步文件 汇报.pptx：OFFICE_VERSION_CONFLICT', 'OFFICE_VERSION_CONFLICT', false, 'trace')
    vi.mocked(requestOffice).mockRejectedValueOnce(conflict)
    await expect(officeStudioApi.sync({ taskId: 'task' })).rejects.toBe(conflict)
    expect(requestOffice).toHaveBeenCalledTimes(1)
  })
})
