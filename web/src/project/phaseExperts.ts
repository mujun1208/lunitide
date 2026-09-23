import { expertBridge, type ExpertBridge } from '../bridge/client'
import type { ExpertMountingGetResult } from '../generated/bridge'
import { CONVERSATION_EXPERTS } from '../expert/conversationExperts'

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

const CONVERSATION_SPECIALIST_NAMES = new Set<string>(CONVERSATION_EXPERTS.map(item => item.name))

export function phaseKeyFromLabel(label?: string): WorkbenchPhaseKey | undefined {
  if (!label) return undefined
  return LABEL_TO_PHASE_KEY[label]
}

export function isConversationSpecialistName(name?: string): boolean {
  return !!name && CONVERSATION_SPECIALIST_NAMES.has(name)
}

/** Phase seed is confirmed mounts only — never the advisory 13-specialist catalog. */
export function phaseSeedExpertIds(row?: ExpertMountingGetResult['matrix'][number]): string[] {
  if (!row) return []
  return row.mountings.filter(m => m.state === 'mounted').map(m => m.expertId).slice(0, 4)
}

/** Explicit 推荐: mounted experts, otherwise the phase's advisory defaults. */
export function phaseRecommendExpertIds(row?: ExpertMountingGetResult['matrix'][number]): string[] {
  const mounted = phaseSeedExpertIds(row)
  if (mounted.length) return mounted
  const ids: string[] = []
  for (const item of row?.defaults ?? []) {
    if (!item.expertId || ids.includes(item.expertId)) continue
    ids.push(item.expertId)
    if (ids.length === 4) break
  }
  return ids
}

export function sessionExpertsAfterPhaseSeed(existing: readonly string[], seed: readonly string[]): string[] {
  if (existing.length) return [...existing]
  return [...seed]
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
  return phaseSeedExpertIds(row)
}

export async function resolvePhaseRecommendIds(
  projectId: string,
  phaseLabel?: string,
  experts: ExpertBridge = expertBridge,
): Promise<string[]> {
  const phaseKey = phaseKeyFromLabel(phaseLabel)
  if (!phaseKey) return []
  const mounting = await experts.mountingGet({ projectId, phaseKey })
  const row = mounting.matrix.find(m => m.phaseKey === phaseKey)
  return phaseRecommendExpertIds(row)
}

export async function applySessionPhaseExperts(
  sessionId: string,
  projectId: string,
  phaseLabel?: string,
  experts: ExpertBridge = expertBridge,
): Promise<string[]> {
  const existing = await experts.sessionMountGet?.({ sessionId }).catch(() => ({ expertIds: [] as string[] }))
  if (existing?.expertIds?.length) return existing.expertIds
  const ids = await resolvePhaseExpertIds(projectId, phaseLabel, experts)
  if (!ids.length || !experts.sessionMountSet) return []
  await experts.sessionMountSet({ sessionId, expertIds: ids })
  return ids
}
