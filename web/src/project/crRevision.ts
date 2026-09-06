import { createMutationAttempt, releaseBridge, type DeliverableBridge, type ReleaseBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import {
  dbRegistryPhase,
  interfaceRegistryPhase,
} from './deliverableTypes'
import { devPhaseForType } from './checklistTypes'

export function crIdForProject(project: ProjectDTO): string {
  return `CR-${project.projectCode}`
}

export type CrMember = { name: string; sha256: string }

export async function collectCrMembers(
  project: ProjectDTO,
  deliverables: DeliverableBridge,
): Promise<CrMember[]> {
  const members: CrMember[] = []
  const phases = [
    { phase: devPhaseForType(project.type), types: ['dev_checklist'] },
    { phase: interfaceRegistryPhase(project.type), types: ['interface_list'] },
    { phase: dbRegistryPhase(project.type), types: ['db_design'] },
  ]
  for (const entry of phases) {
    const list = await deliverables.list({ projectId: project.id, phase: entry.phase })
    for (const type of entry.types) {
      const item = list.items.find(i => i.documentType === type)
      if (!item) continue
      if (!['approved', 'immutable'].includes(item.status) || !/^[0-9a-f]{64}$/.test(item.digest || '')) continue
      // Size is derived when the server reads the approved source for the
      // immutable revision. This view only previews the recorded digest.
      members.push({ name: `${type}.json`, sha256: item.digest! })
    }
  }
  return members
}

export async function createProjectCrRevision(
  project: ProjectDTO,
  summary: string,
  deliverables: DeliverableBridge,
  release: ReleaseBridge = releaseBridge,
): Promise<{ crRevisionId: string; revisionNo: number; digest: string }> {
  // Read readiness for a useful local error. Source bytes, sizes, hashes and
  // inventory are always reconstructed by the server inside its transaction.
  const members = await collectCrMembers(project, deliverables)
  if (members.length !== 3) throw new Error('请先批准数据库、接口和开发清单的真实交付文件')
  const payload = {
    crId: crIdForProject(project),
    manifest: {
      authorId: 'workbench',
      summary,
      projectId: project.id,
      projectCode: project.projectCode,
    },
    requestId: `cr-${project.id}-${Date.now()}`,
  }
  const result = await release.createRevision(payload, { attempt: createMutationAttempt('release.createRevision', payload) })
  return result
}

const storageKey = (projectId: string) => `lunitide:cr-revision:${projectId}`

export function readStoredCrRevision(projectId: string): { crRevisionId: string; digest: string } | undefined {
  try {
    const raw = localStorage.getItem(storageKey(projectId))
    if (!raw) return undefined
    return JSON.parse(raw) as { crRevisionId: string; digest: string }
  } catch {
    return undefined
  }
}

export function writeStoredCrRevision(projectId: string, crRevisionId: string, digest: string): void {
  try {
    localStorage.setItem(storageKey(projectId), JSON.stringify({ crRevisionId, digest }))
  } catch { /* ignore */ }
}
