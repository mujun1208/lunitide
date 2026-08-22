import { expertBridge, type ExpertBridge } from '../bridge/client'
import type { ExpertMountingGetResult } from '../generated/bridge'

export type WorkbenchPhaseKey = ExpertMountingGetResult['matrix'][number]['phaseKey']

const LABEL_TO_PHASE_KEY: Record<string, WorkbenchPhaseKey> = {
  '需求架构规范': 'REQUIREMENT_DEFINITION',
  '方案和UI设计': 'SOLUTION_EXPERIENCE',
  '数据库': 'ARCHITECTURE_PLAN',
  '接口': 'ARCHITECTURE_PLAN',
  '开发': 'DEVELOPMENT_CHANGE',
  '测试': 'VERIFICATION_ACCEPTANCE',
  '集成': 'DEVELOPMENT_CHANGE',
  '发布': 'RELEASE_DELIVERY',
}

export function phaseKeyFromLabel(label?: string): WorkbenchPhaseKey | undefined {
  if (!label) return undefined
  return LABEL_TO_PHASE_KEY[label]
}

export async function resolvePhaseExpertIds(
  projectId: string,
  phaseLabel?: string,
  experts: ExpertBridge = expertBridge,
): Promise<string[]> {
  const phaseKey = phaseKeyFromLabel(phaseLabel)
  if (!phaseKey) return []
  const mounting = await experts.mountingGet({ projectId, phaseKey })
  const row = mounting.matrix.find(m => m.phaseKey === phaseKey)
  if (!row) return []
  const mounted = row.mountings.filter(m => m.state === 'mounted').map(m => m.expertId)
  if (mounted.length) return mounted.slice(0, 4)
  return row.defaults.slice(0, 4).map(d => d.expertId)
}

export async function applySessionPhaseExperts(
  sessionId: string,
  projectId: string,
  phaseLabel?: string,
  experts: ExpertBridge = expertBridge,
): Promise<string[]> {
  const ids = await resolvePhaseExpertIds(projectId, phaseLabel, experts)
  if (!ids.length || !experts.sessionMountSet) return []
  await experts.sessionMountSet({ sessionId, expertIds: ids })
  return ids
}
