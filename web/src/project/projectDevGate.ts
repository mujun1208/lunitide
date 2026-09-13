import type { ProjectDTO } from '../generated/bridge'

export function registryTreeReady(project: Pick<ProjectDTO, 'rootPath' | 'treeStatus'>): boolean {
  return Boolean(project.rootPath?.trim()) && project.treeStatus === 'ready'
}
