import type { ProjectType } from '../generated/bridge'

export type DeliverableDef = { key: string; title: string; ordinal: number }

/** 需求架构规范 · 9 份必需交付物（PRD 第 8 章） */
export const PHASE1_DOCS: DeliverableDef[] = [
  { key: 'biz_req_analysis', ordinal: 1, title: '业务需求分析报告' },
  { key: 'impl_assessment', ordinal: 2, title: '系统实现评估报告' },
  { key: 'req_task_list', ordinal: 3, title: '需求任务清单' },
  { key: 'arch_design', ordinal: 4, title: '系统架构设计文档' },
  { key: 'hw_config', ordinal: 5, title: '系统硬件配置文档' },
  { key: 'biz_standard', ordinal: 6, title: '系统业务规范' },
  { key: 'dev_standard', ordinal: 7, title: '系统开发规范' },
  { key: 'tech_standard', ordinal: 8, title: '系统技术规范' },
  { key: 'project_structure', ordinal: 9, title: '项目结构规范' },
]

/** 方案和 UI 设计 · 10 份必需交付物（PRD 第 8 章） */
export const PHASE2_DOCS: DeliverableDef[] = [
  { key: 'biz_flow_diagram', ordinal: 1, title: '业务流程图' },
  { key: 'biz_flow_list', ordinal: 2, title: '业务流程清单' },
  { key: 'biz_blueprint', ordinal: 3, title: '业务蓝图文档' },
  { key: 'api_list', ordinal: 4, title: '接口清单' },
  { key: 'feature_dev_list', ordinal: 5, title: '功能开发清单' },
  { key: 'feature_detail', ordinal: 6, title: '功能详细设计文档' },
  { key: 'api_detail', ordinal: 7, title: '接口详细设计文档' },
  { key: 'db_detail', ordinal: 8, title: '数据库详细设计文档' },
  { key: 'ui_detail', ordinal: 9, title: 'UI界面详细设计' },
  { key: 'integration_test_list', ordinal: 10, title: '集成测试清单文档' },
]

export const PHASE3_DOCS: DeliverableDef[] = [
  { key: 'interface_list', ordinal: 1, title: '接口清单' },
]

export const PHASE4_DOCS: DeliverableDef[] = [
  { key: 'db_design', ordinal: 1, title: '数据库设计文档' },
]

export const PHASE5_DOCS: DeliverableDef[] = [
  { key: 'dev_checklist', ordinal: 1, title: '开发检查清单' },
]

export const PHASE6_DOCS: DeliverableDef[] = [
  { key: 'test_checklist', ordinal: 1, title: '测试检查清单' },
]

const APPROVED = new Set(['approved', 'immutable'])

export function isDeliverableReady(status?: string): boolean {
  return !!status && APPROVED.has(status)
}

export function deliverablesForPhase(phase: number, type?: ProjectType): DeliverableDef[] {
  if (phase === 1) return PHASE1_DOCS
  if (type === 'operations') {
    if (phase === 2) return PHASE4_DOCS
    if (phase === 3) return PHASE3_DOCS
    if (phase === 4) return PHASE5_DOCS
    if (phase === 5) return PHASE6_DOCS
    return []
  }
  if (phase === 2) return PHASE2_DOCS
  if (phase === 3) return PHASE3_DOCS
  if (phase === 4) return PHASE4_DOCS
  if (phase === 5) return PHASE5_DOCS
  if (phase === 6) return PHASE6_DOCS
  return []
}

export function gatePhaseForType(type: string): number[] {
  if (type === 'operations') return [1, 2, 3, 4, 5]
  return [1, 2, 3, 4, 5, 6]
}
