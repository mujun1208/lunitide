import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type DeliverableBridge, type ProjectAttachmentBridge, type ProjectBridge, type TemplateBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import { DeliverablePanel } from './DeliverablePanel'
import { projectFactoryApi } from './projectFactoryApi'

vi.mock('./projectSpineApi', () => ({
  projectSpineApi: {
    treeGet: vi.fn().mockResolvedValue({ tree: { version: 1, dirs: ['src'], phaseMap: {}, codeRoot: 'src' }, treeStatus: 'none' }),
    treePut: vi.fn(),
    treeMaterialize: vi.fn(),
  },
}))
vi.mock('./ProjectPlanPanel', () => ({ ProjectPlanPanel: () => null }))
vi.mock('./projectFactoryApi', () => ({
  projectFactoryApi: {
    boardGet: vi.fn().mockResolvedValue({ board: { items: [] }, statsText: '' }),
    boardSync: vi.fn().mockResolvedValue({ board: { items: [] }, statsText: '' }),
    boardItemOpen: vi.fn(),
    schemaGet: vi.fn().mockResolvedValue({ schema: { version: 1, dialect: 'sqlite', tables: [] } }),
    generate: vi.fn(),
    interviewGet: vi.fn().mockResolvedValue({ interview: { phases: {} } }),
    interviewSave: vi.fn(),
  },
}))

afterEach(cleanup)
const project = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Project', projectCode: 'ITM00001', type: 'implementation', status: 'in_progress', version: 1 } as ProjectDTO
const encode = (obj: unknown) => btoa(unescape(encodeURIComponent(JSON.stringify(obj))))

it('does not show raw English list failures', async () => {
  render(
    <DeliverablePanel
      project={project}
      phase={1}
      bridge={{} as ProjectBridge}
      deliverableBridge={{ list: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as DeliverableBridge}
      projectAttachments={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  const alerts = await screen.findAllByRole('alert')
  expect(alerts.some(node => node.textContent?.includes('请求失败'))).toBe(true)
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('does not leak BridgeClientError transport English from the deliverable list', async () => {
  render(
    <DeliverablePanel
      project={project}
      phase={1}
      bridge={{} as ProjectBridge}
      deliverableBridge={{ list: vi.fn().mockRejectedValue(new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, 'engine')) } as unknown as DeliverableBridge}
      projectAttachments={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  const alerts = await screen.findAllByRole('alert')
  expect(alerts.some(node => node.textContent?.includes('请求失败'))).toBe(true)
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('imports operations development items from the phase-1 task list', async () => {
  let saved = false
  const list = vi.fn().mockImplementation(async ({ phase }: { phase: number }) => {
    if (phase === 1) return { items: [{ documentType: 'req_task_list', attachmentId: 'att-req' }] }
    return { items: saved ? [{ documentType: 'dev_checklist', attachmentId: 'att-dev', status: 'review' }] : [] }
  })
  const ingest = vi.fn().mockResolvedValue({ attachmentId: 'att-dev' })
  const upsert = vi.fn().mockImplementation(async () => {
    saved = true
    return { documentType: 'dev_checklist', attachmentId: 'att-dev', status: 'review' }
  })
  render(
    <DeliverablePanel
      project={{ ...project, type: 'operations' }}
      phase={4}
      bridge={{} as ProjectBridge}
      deliverableBridge={{ list, upsert } as unknown as DeliverableBridge}
      projectAttachments={{
        list: vi.fn().mockResolvedValue({ items: [] }),
        get: vi.fn().mockResolvedValue({
          contentBase64: encode({ version: 1, items: [{ id: 'R001', title: '巡检', status: 'pending' }] }),
        }),
        ingest,
      } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  expect(await screen.findByDisplayValue('巡检')).toBeInTheDocument()
  expect(list).toHaveBeenCalledWith(expect.objectContaining({ phase: 1 }))
})

it('refuses to approve a test checklist that is not all passing', async () => {
  const user = userEvent.setup()
  const upsert = vi.fn()
  render(
    <DeliverablePanel
      project={project}
      phase={6}
      bridge={{} as ProjectBridge}
      deliverableBridge={{
        list: vi.fn().mockResolvedValue({
          items: [{ documentType: 'test_checklist', title: '测试检查清单', attachmentId: 'att-t', status: 'review' }],
        }),
        upsert,
      } as unknown as DeliverableBridge}
      projectAttachments={{
        list: vi.fn().mockResolvedValue({ items: [] }),
        get: vi.fn().mockResolvedValue({
          contentBase64: encode({ version: 1, items: [{ id: 'T1', title: '测', status: 'pending' }] }),
        }),
      } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  await user.click(await screen.findByRole('button', { name: '确认 测试检查清单' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('测试未全部通过')
  expect(upsert).not.toHaveBeenCalled()
})

it('requires an empty-interface ack before phase-4 三关 can finish', async () => {
  const user = userEvent.setup()
  const advanceStatus = vi.fn()
  render(
    <DeliverablePanel
      project={project}
      phase={4}
      bridge={{ advanceStatus } as unknown as ProjectBridge}
      deliverableBridge={{
        list: vi.fn().mockResolvedValue({
          items: [{ documentType: 'interface_list', title: '接口清单', attachmentId: 'att-i', status: 'approved', digest: 'items:0' }],
        }),
      } as unknown as DeliverableBridge}
      projectAttachments={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  await user.click(await screen.findByRole('button', { name: '三关确认晋级' }))
  await user.click(screen.getByRole('button', { name: '下一关' }))
  await user.click(screen.getByRole('button', { name: '下一关' }))
  expect(screen.getByLabelText('无接口任务')).toBeInTheDocument()
  const submit = screen.getByRole('button', { name: '确认晋级' })
  expect(submit).toBeDisabled()
  await user.type(screen.getByLabelText('确认语'), '确认阶段晋级')
  expect(submit).toBeDisabled()
  await user.click(screen.getByLabelText('无接口任务'))
  expect(submit).toBeEnabled()
  expect(advanceStatus).not.toHaveBeenCalled()
  await user.click(submit)
  expect(advanceStatus).toHaveBeenCalledWith(
    { id: project.id, version: project.version, phase: 4, emptyBoardAck: true },
    expect.anything(),
  )
})

it('refuses to confirm a card that only has a templateId', async () => {
  const user = userEvent.setup()
  const upsert = vi.fn()
  render(
    <DeliverablePanel
      project={project}
      phase={1}
      bridge={{} as ProjectBridge}
      deliverableBridge={{
        list: vi.fn().mockResolvedValue({
          items: [{ documentType: 'biz_req_analysis', title: '业务需求分析报告', templateId: '01ARZ3NDEKTSV4RRFFQ69G5FAT', status: 'review' }],
        }),
        upsert,
      } as unknown as DeliverableBridge}
      projectAttachments={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  await user.click(await screen.findByRole('button', { name: /业务需求分析报告/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('不能只绑模版')
  expect(upsert).not.toHaveBeenCalled()
})

it('resyncs the work board when checklist upsert reports boardDirty', async () => {
  const user = userEvent.setup()
  vi.mocked(projectFactoryApi.boardSync).mockClear()
  const upsert = vi.fn().mockResolvedValue({
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    projectId: project.id,
    phase: 2,
    documentType: 'api_list',
    title: '接口清单',
    status: 'approved',
    gateConfirmations: 0,
    version: 1,
    boardDirty: true,
  })
  render(
    <DeliverablePanel
      project={project}
      phase={2}
      bridge={{} as ProjectBridge}
      deliverableBridge={{
        list: vi.fn().mockResolvedValue({
          items: [{ documentType: 'api_list', title: '接口清单', attachmentId: 'att-api', status: 'review' }],
        }),
        upsert,
      } as unknown as DeliverableBridge}
      projectAttachments={{
        list: vi.fn().mockResolvedValue({ items: [] }),
        get: vi.fn().mockResolvedValue({
          contentBase64: encode({ version: 1, items: [{ id: 'I001', title: '登录', status: 'pending' }] }),
        }),
      } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  await user.click(await screen.findByRole('button', { name: '确认 接口清单' }))
  await waitFor(() => expect(projectFactoryApi.boardSync).toHaveBeenCalledTimes(1))
  expect(projectFactoryApi.boardSync).toHaveBeenCalledWith({ projectId: project.id, boardKind: 'interface' })
})
