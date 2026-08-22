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

export type CrMember = { name: string; size: number; sha256: string }

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
      const digest = (item.digest || '').padEnd(64, '0').slice(0, 64)
      members.push({ name: `${type}.json`, size: 1024, sha256: /^[0-9a-f]{64}$/.test(digest) ? digest : '0'.repeat(64) })
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
  const members = await collectCrMembers(project, deliverables)
  const payload = {
    crId: crIdForProject(project),
    manifest: {
      authorId: 'workbench',
      summary,
      projectId: project.id,
      projectCode: project.projectCode,
      members: members.length ? members : [{ name: 'manifest.stub', size: 1, sha256: '0'.repeat(64) }],
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
