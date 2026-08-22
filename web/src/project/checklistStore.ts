import {
  deliverableBridge,
  projectAttachmentBridge,
  type DeliverableBridge,
  type ProjectAttachmentBridge,
} from '../bridge/client'
import type { ProjectDTO, ProjectType } from '../generated/bridge'
import {
  dbRegistryPhase,
  interfaceRegistryPhase,
} from './deliverableTypes'
import {
  devChecklistReady,
  designPhaseForType,
  emptyChecklist,
  parseChecklist,
  serializeChecklist,
  devPhaseForType,
  type ChecklistDoc,
} from './checklistTypes'

const toBase64 = (text: string) => btoa(unescape(encodeURIComponent(text)))

export async function loadChecklistDoc(
  projectId: string,
  phase: number,
  documentType: string,
  deliverables: DeliverableBridge = deliverableBridge,
  attachments: ProjectAttachmentBridge = projectAttachmentBridge,
): Promise<{ doc: ChecklistDoc; deliverableId?: string; status?: string }> {
  const result = await deliverables.list({ projectId, phase })
  const saved = result.items.find(i => i.documentType === documentType)
  if (!saved?.attachmentId) return { doc: emptyChecklist(), deliverableId: saved?.id, status: saved?.status }
  const file = await attachments.get({ projectId, attachmentId: saved.attachmentId })
  return {
    doc: parseChecklist(atob(file.contentBase64)),
    deliverableId: saved.id,
    status: saved.status,
  }
}

export async function saveChecklistDoc(
  project: ProjectDTO,
  phase: number,
  documentType: string,
  title: string,
  doc: ChecklistDoc,
  status: 'review' | 'approved' = 'review',
  deliverables: DeliverableBridge = deliverableBridge,
  attachments: ProjectAttachmentBridge = projectAttachmentBridge,
): Promise<void> {
  const ingested = await attachments.ingest({
    projectId: project.id,
    phase,
    category: 'checklist',
    fileName: `${documentType}.json`,
    mimeType: 'application/json',
    contentBase64: toBase64(serializeChecklist(doc)),
  })
  await deliverables.upsert({
    projectId: project.id,
    phase,
    documentType,
    title,
    attachmentId: ingested.attachmentId,
    status,
    digest: `items:${doc.items.length}`,
  })
}

export async function rollbackTestFailToDev(
  project: ProjectDTO,
  testItemId: string,
  sourceId: string | undefined,
  reason: string,
  deliverables: DeliverableBridge = deliverableBridge,
  attachments: ProjectAttachmentBridge = projectAttachmentBridge,
): Promise<boolean> {
  if (!sourceId) return false
  const devPhase = devPhaseForType(project.type)
  const { doc } = await loadChecklistDoc(project.id, devPhase, 'dev_checklist', deliverables, attachments)
  if (!doc.items.some(i => i.id === sourceId)) return false
  const stamp = new Date().toISOString().slice(0, 16).replace('T', ' ')
  const next: ChecklistDoc = {
    version: 1,
    items: doc.items.map(item =>
      item.id === sourceId
        ? {
            ...item,
            status: 'in_progress',
            notes: `${item.notes ? `${item.notes}; ` : ''}[测试退回 ${testItemId} @ ${stamp}] ${reason}`.trim(),
          }
        : item,
    ),
  }
  await saveChecklistDoc(project, devPhase, 'dev_checklist', '开发检查清单', next, 'review', deliverables, attachments)
  return true
}

export type IntegrationGateResult = { ready: boolean; blockers: string[] }

export async function fetchIntegrationGateReady(
  project: ProjectDTO,
  deliverables: DeliverableBridge = deliverableBridge,
  attachments: ProjectAttachmentBridge = projectAttachmentBridge,
): Promise<IntegrationGateResult> {
  const blockers: string[] = []
  const devPhase = devPhaseForType(project.type)
  const testPhase = project.type === 'operations' ? 5 : 6
  const devList = await deliverables.list({ projectId: project.id, phase: devPhase })
  const testList = await deliverables.list({ projectId: project.id, phase: testPhase })
  const ifaceList = await deliverables.list({ projectId: project.id, phase: interfaceRegistryPhase(project.type) })
  const dbList = await deliverables.list({ projectId: project.id, phase: dbRegistryPhase(project.type) })

  const dev = devList.items.find(i => i.documentType === 'dev_checklist')
  const test = testList.items.find(i => i.documentType === 'test_checklist')
  const iface = ifaceList.items.find(i => i.documentType === 'interface_list')
  const db = dbList.items.find(i => i.documentType === 'db_design')

  if (!devChecklistReady(dev?.status)) blockers.push('开发检查清单未确认（需 review/approved）')
  if (dev?.attachmentId) {
    const file = await attachments.get({ projectId: project.id, attachmentId: dev.attachmentId })
    const doc = parseChecklist(atob(file.contentBase64))
    const open = doc.items.filter(i => i.status !== 'dev_done')
    if (open.length) blockers.push(`开发清单仍有 ${open.length} 条未完成`)
  } else {
    blockers.push('开发检查清单无数据')
  }

  if (!test || !['review', 'approved', 'immutable'].includes(test.status ?? '')) {
    blockers.push('测试检查清单未保存或未确认')
  }
  if (test?.attachmentId) {
    const file = await attachments.get({ projectId: project.id, attachmentId: test.attachmentId })
    const doc = parseChecklist(atob(file.contentBase64))
    const failed = doc.items.filter(i => i.status === 'test_fail')
    if (failed.length) blockers.push(`测试清单有 ${failed.length} 条不通过项需退回开发`)
    const pending = doc.items.filter(i => i.status === 'pending' || i.status === 'in_progress')
    if (pending.length) blockers.push(`测试清单仍有 ${pending.length} 条待测`)
  }

  if (!iface?.attachmentId) blockers.push('接口清单（OpenAPI）未绑定')
  if (!db?.attachmentId) blockers.push('数据库设计验证未绑定')

  const design = await deliverables.list({ projectId: project.id, phase: designPhaseForType(project.type) })
  const apiList = design.items.find(i => i.documentType === 'api_list')
  if (!apiList?.attachmentId && !iface?.attachmentId) blockers.push('方案阶段接口清单缺失')

  return { ready: blockers.length === 0, blockers }
}
