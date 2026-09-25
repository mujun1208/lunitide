import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { DeliverableBridge, ProjectAttachmentBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import { ChecklistPanel, DEV_ITEM_STATUSES, TEST_ITEM_STATUSES } from './ChecklistPanel'

vi.mock('./projectSpineApi', () => ({
  projectSpineApi: {
    returnFromTest: vi.fn().mockResolvedValue({ returned: true, testItemId: 'T-F001', devItemId: 'F001' }),
  },
}))

afterEach(cleanup)

const project = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'Mall',
  projectCode: 'ITM00001',
  type: 'implementation',
  status: 'in_progress',
  version: 1,
  treeStatus: 'ready',
} as ProjectDTO

const encode = (obj: unknown) => btoa(unescape(encodeURIComponent(JSON.stringify(obj))))

it('shows 进入开发 once the project directory is selected', async () => {
  const onOpenTask = vi.fn()
  const deliverables = {
    list: vi.fn().mockResolvedValue({
      items: [{ documentType: 'dev_checklist', attachmentId: 'att-dev', status: 'review' }],
    }),
  } as unknown as DeliverableBridge
  const attachments = {
    get: vi.fn().mockResolvedValue({
      contentBase64: encode({ version: 1, items: [{ id: 'F001', title: '登录', status: 'pending' }] }),
    }),
  } as unknown as ProjectAttachmentBridge
  const { rerender } = render(
    <ChecklistPanel
      project={{ ...project, rootPath: '', treeStatus: 'none' }}
      phase={5}
      documentType="dev_checklist"
      title="开发检查清单"
      deliverables={deliverables}
      attachments={attachments}
      statusOptions={DEV_ITEM_STATUSES}
      onOpenTask={onOpenTask}
    />,
  )
  const blocked = await screen.findByRole('button', { name: '进入开发' })
  expect(blocked).toBeDisabled()
  rerender(
    <ChecklistPanel
      project={{ ...project, rootPath: 'D:\\work\\mall', treeStatus: 'none' }}
      phase={5}
      documentType="dev_checklist"
      title="开发检查清单"
      deliverables={deliverables}
      attachments={attachments}
      statusOptions={DEV_ITEM_STATUSES}
      onOpenTask={onOpenTask}
    />,
  )
  const open = await screen.findByRole('button', { name: '进入开发' })
  expect(open).toBeEnabled()
  expect(screen.getByLabelText('执行器 F001')).toBeInTheDocument()
  await userEvent.click(open)
  expect(onOpenTask).toHaveBeenCalledWith('F001', undefined)
})

it('auto-imports an empty development checklist once', async () => {
  let saved = false
  const ingest = vi.fn().mockResolvedValue({ attachmentId: 'att-dev' })
  const upsert = vi.fn().mockImplementation(async () => {
    saved = true
    return { id: 'd1', documentType: 'dev_checklist', attachmentId: 'att-dev', status: 'review' }
  })
  const deliverables = {
    list: vi.fn().mockImplementation(async ({ phase }: { phase: number }) => {
      if (phase === 2) return { items: [{ documentType: 'feature_dev_list', attachmentId: 'att-feat' }] }
      return { items: saved ? [{ documentType: 'dev_checklist', attachmentId: 'att-dev', status: 'review' }] : [] }
    }),
    upsert,
  } as unknown as DeliverableBridge
  const attachments = {
    get: vi.fn().mockImplementation(async ({ attachmentId }: { attachmentId: string }) => ({
      contentBase64: encode(
        attachmentId === 'att-feat'
          ? { version: 1, items: [{ id: 'F001', title: '登录', status: 'pending' }] }
          : { version: 1, items: [{ id: 'F001', title: '登录', status: 'pending', sourceId: 'F001' }] },
      ),
    })),
    ingest,
  } as unknown as ProjectAttachmentBridge
  render(
    <ChecklistPanel
      project={project}
      phase={5}
      documentType="dev_checklist"
      title="开发检查清单"
      deliverables={deliverables}
      attachments={attachments}
      statusOptions={DEV_ITEM_STATUSES}
      autoImport
      importFrom={{
        label: '功能开发清单',
        phase: 2,
        documentType: 'feature_dev_list',
        mapItems: (source) => source.items.map(item => ({ ...item, status: 'pending' as const })),
      }}
    />,
  )
  expect(await screen.findByDisplayValue('登录')).toBeInTheDocument()
  await waitFor(() => expect(ingest).toHaveBeenCalled())
  expect(upsert).toHaveBeenCalled()
})

it('requires a reason before marking a test row as failed', async () => {
  const user = userEvent.setup()
  render(
    <ChecklistPanel
      project={project}
      phase={6}
      documentType="test_checklist"
      title="测试检查清单"
      deliverables={{
        list: vi.fn().mockResolvedValue({
          items: [{ documentType: 'test_checklist', attachmentId: 'att-test', status: 'review' }],
        }),
      } as unknown as DeliverableBridge}
      attachments={{
        get: vi.fn().mockResolvedValue({
          contentBase64: encode({
            version: 1,
            items: [{ id: 'T-F001', title: '测登录', status: 'pending', sourceId: 'F001' }],
          }),
        }),
      } as unknown as ProjectAttachmentBridge}
      statusOptions={TEST_ITEM_STATUSES}
      enableTestRollback
    />,
  )
  const select = await screen.findByDisplayValue('待处理')
  await user.selectOptions(select, 'test_fail')
  expect(await screen.findByRole('dialog', { name: '测试不通过原因' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '退回开发' })).toBeDisabled()
  await user.type(screen.getByPlaceholderText('说明失败原因，将退回同一条开发任务'), '登录失败')
  expect(screen.getByRole('button', { name: '退回开发' })).toBeEnabled()
})
