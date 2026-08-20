import { createMutationAttempt, type StageBridge } from '../bridge/client'
import type { ProjectType, StageDTO, StageStatus } from '../generated/bridge'

export type ProjectPhaseDef = { phase: number; label: string }

/** 实施 / 增强：设计文档八阶段投影 */
export const IMPLEMENTATION_PHASES: ProjectPhaseDef[] = [
  { phase: 1, label: '需求架构规范' },
  { phase: 2, label: '方案和UI设计' },
  { phase: 3, label: '数据库' },
  { phase: 4, label: '接口' },
  { phase: 5, label: '开发' },
  { phase: 6, label: '测试' },
  { phase: 7, label: '集成' },
  { phase: 8, label: '发布' },
]

/** 运维：六阶段投影（跳过方案/UI、集成） */
export const OPERATIONS_PHASES: ProjectPhaseDef[] = [
  { phase: 1, label: '需求架构规范' },
  { phase: 2, label: '数据库' },
  { phase: 3, label: '接口' },
  { phase: 4, label: '开发' },
  { phase: 5, label: '测试' },
  { phase: 6, label: '发布' },
]

export const PROJECT_TYPE_SHORT: Record<ProjectType, string> = {
  implementation: '实施',
  operations: '运维',
  enhancement: '增强',
}

export function phasesForProjectType(type: ProjectType): ProjectPhaseDef[] {
  return type === 'operations' ? OPERATIONS_PHASES : IMPLEMENTATION_PHASES
}

export function phaseStepClass(status?: StageStatus): 'done' | 'run' | '' {
  if (!status) return ''
  if (status === 'completed' || status === 'approved') return 'done'
  if (status === 'in_progress' || status === 'waiting_review') return 'run'
  return ''
}

export function inferActivePhase(
  phases: ProjectPhaseDef[],
  stageMap: Map<number, StageDTO>,
  preferred?: number,
): number {
  if (preferred && phases.some(p => p.phase === preferred)) return preferred
  const running = phases.find(p => {
    const s = stageMap.get(p.phase)?.status
    return s === 'in_progress' || s === 'waiting_review'
  })
  if (running) return running.phase
  const firstOpen = phases.find(p => {
    const s = stageMap.get(p.phase)?.status
    return s !== 'completed' && s !== 'approved'
  })
  return firstOpen?.phase ?? phases[phases.length - 1]?.phase ?? 1
}

const phaseStorageKey = (projectId: string) => `lunitide:project-phase:${projectId}`

export function readPreferredPhase(projectId: string): number | undefined {
  try {
    const raw = localStorage.getItem(phaseStorageKey(projectId))
    if (!raw) return undefined
    const n = Number.parseInt(raw, 10)
    return Number.isInteger(n) && n >= 1 && n <= 9 ? n : undefined
  } catch {
    return undefined
  }
}

export function writePreferredPhase(projectId: string, phase: number): void {
  try {
    localStorage.setItem(phaseStorageKey(projectId), String(phase))
  } catch {
    /* ignore quota */
  }
}

export async function ensureProjectStages(
  stages: StageBridge,
  projectId: string,
  type: ProjectType,
): Promise<StageDTO[]> {
  const defs = phasesForProjectType(type)
  let listed = await stages.list({ projectId })
  const byPhase = new Map(listed.items.map(s => [s.phase, s]))
  for (const def of defs) {
    if (byPhase.has(def.phase)) continue
    const payload = { projectId, phase: def.phase, title: def.label }
    try {
      const created = await stages.create(payload, {
        attempt: createMutationAttempt('stage.create', payload),
      })
      byPhase.set(def.phase, created)
    } catch {
      /* seed best-effort; list again below */
    }
  }
  listed = await stages.list({ projectId })
  return listed.items
}
