import type { ProjectType } from '../generated/bridge'

export type ChecklistItemStatus = 'pending' | 'in_progress' | 'dev_done' | 'test_pass' | 'test_fail'

export type ChecklistItem = {
  id: string
  title: string
  module?: string
  priority?: string
  status: ChecklistItemStatus
  sourceId?: string
  notes?: string
}

export type ChecklistDoc = {
  version: 1
  items: ChecklistItem[]
}

export const CHECKLIST_DOCUMENTS = new Set([
  'feature_dev_list',
  'dev_checklist',
  'test_checklist',
  'integration_test_list',
])

export function isChecklistDocument(key: string): boolean {
  return CHECKLIST_DOCUMENTS.has(key)
}

export function emptyChecklist(): ChecklistDoc {
  return { version: 1, items: [] }
}

export function parseChecklist(raw: string): ChecklistDoc {
  try {
    const parsed = JSON.parse(raw) as Partial<ChecklistDoc>
    if (parsed?.version !== 1 || !Array.isArray(parsed.items)) return emptyChecklist()
    const items = parsed.items
      .filter(item => item && typeof item.id === 'string' && typeof item.title === 'string')
      .map(item => ({
        id: item.id,
        title: item.title,
        module: item.module,
        priority: item.priority,
        status: validStatus(item.status) ? item.status : 'pending',
        sourceId: item.sourceId,
        notes: item.notes,
      }))
    return { version: 1, items }
  } catch {
    return emptyChecklist()
  }
}

function validStatus(status: unknown): status is ChecklistItemStatus {
  return status === 'pending' || status === 'in_progress' || status === 'dev_done'
    || status === 'test_pass' || status === 'test_fail'
}

export function serializeChecklist(doc: ChecklistDoc): string {
  return JSON.stringify(doc, null, 2)
}

export function checklistSummary(doc: ChecklistDoc): string {
  const done = doc.items.filter(i => i.status === 'dev_done' || i.status === 'test_pass').length
  return `${done}/${doc.items.length}`
}

export function devPhaseForType(type: ProjectType): number {
  return type === 'operations' ? 4 : 5
}

export function designPhaseForType(_type: ProjectType): number {
  return 2
}

export function devChecklistReady(status?: string): boolean {
  return status === 'review' || status === 'approved' || status === 'immutable'
}

export function nextChecklistId(items: ChecklistItem[], prefix: string): string {
  const nums = items
    .map(i => i.id.match(new RegExp(`^${prefix}(\\d+)$`)))
    .filter(Boolean)
    .map(m => Number.parseInt(m![1]!, 10))
    .filter(n => Number.isFinite(n))
  const next = (nums.length ? Math.max(...nums) : 0) + 1
  return `${prefix}${String(next).padStart(3, '0')}`
}

export const DEV_ITEM_STATUSES: ChecklistItemStatus[] = ['pending', 'in_progress', 'dev_done']
export const TEST_ITEM_STATUSES: ChecklistItemStatus[] = ['pending', 'in_progress', 'test_pass', 'test_fail']
