import type { ProjectType } from '../generated/bridge'

export type WorkbenchProjectStatus =
  | 'created'
  | 'chartered'
  | 'req_architecture'
  | 'req_assessment'
  | 'in_progress'
  | 'integration_test'
  | 'go_live_prep'
  | 'live'
  | 'closed'
  | 'archived'
  | 'active'

const STATUS_LABELS: Record<Exclude<WorkbenchProjectStatus, 'active'>, string> = {
  created: '创建',
  chartered: '立项',
  req_architecture: '需求架构',
  req_assessment: '需求评估',
  in_progress: '实施中',
  integration_test: '集成测试',
  go_live_prep: '上线准备',
  live: '系统上线',
  closed: '项目关闭·只读',
  archived: '已删除',
}

export function normalizeStatus(status: string): Exclude<WorkbenchProjectStatus, 'active'> {
  if (status === 'active') return 'chartered'
  return status as Exclude<WorkbenchProjectStatus, 'active'>
}

export function statusLabel(status: string, type?: ProjectType): string {
  const s = normalizeStatus(status)
  if (s === 'req_assessment' && type === 'enhancement') return '需求评估'
  if (s === 'req_architecture' && type === 'enhancement') return '需求评估'
  return STATUS_LABELS[s] ?? status
}

export function canEnterWorkbench(status: string): boolean {
  const s = normalizeStatus(status)
  return s !== 'created' && s !== 'archived'
}

export function canPublish(status: string): boolean {
  return normalizeStatus(status) === 'created'
}

export function closeable(type: ProjectType): Exclude<WorkbenchProjectStatus, 'active'>[] {
  switch (type) {
    case 'operations':
    case 'enhancement':
      return ['chartered', 'req_assessment', 'in_progress']
    default:
      return ['chartered', 'req_architecture', 'in_progress', 'integration_test', 'go_live_prep', 'live']
  }
}

export function canClose(status: string, type: ProjectType): boolean {
  const s = normalizeStatus(status)
  if (s === 'closed' || s === 'created' || s === 'archived') return false
  return closeable(type).includes(s)
}

export function canDelete(status: string): boolean {
  return normalizeStatus(status) === 'created'
}

export function isReadOnly(status: string): boolean {
  const s = normalizeStatus(status)
  return s === 'closed' || s === 'archived'
}
