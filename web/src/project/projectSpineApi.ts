import { AGENT_HUB_DIR_PICK_MS, getAgentHubBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'

export type ProjectExecutor = 'lunitide' | 'cursor' | 'codex'
export type ProjectTreeStatus = 'none' | 'pending' | 'ready' | 'partial' | 'failed'

export type TaskBrief = {
  itemId: string
  title: string
  acceptance: string
  targetRelPath: string
  rootPath: string
  text: string
  testReturn?: { id: string; reason: string; at: string }
}

export type ProjectTreeV1 = {
  version: 1
  dirs: string[]
  phaseMap: Record<string, string>
  codeRoot: string
}

function request<T>(method: string, payload: object, deadlineMs?: number): Promise<T> {
  const bridge = getAgentHubBridge()
  if (method === 'project.root.pick') {
    return getAgentHubBridge().request<T>(method, payload)
  }
  return bridge.request<T>(method, payload)
}

export const projectSpineApi = {
  rootPick: () =>
    request<{ canceled: boolean; path: string }>('project.root.pick', {}, AGENT_HUB_DIR_PICK_MS),
  rootRebind: (payload: { id: string; version: number; rootPath: string }) =>
    request<ProjectDTO>('project.root.rebind', payload),
  treeGet: (payload: { projectId: string }) =>
    request<{ tree: ProjectTreeV1; receipt?: unknown; treeStatus?: ProjectTreeStatus; rootPath?: string }>('project.tree.get', payload),
  treePut: (payload: { projectId: string; version: number; tree: ProjectTreeV1 }) =>
    request<{ tree: ProjectTreeV1; project: ProjectDTO }>('project.tree.put', payload),
  treeMaterialize: (payload: { projectId: string; version: number }) =>
    request<{ receipt: unknown; project: ProjectDTO }>('project.tree.materialize', payload),
  executorSet: (payload: { id: string; version: number; executor: ProjectExecutor }) =>
    request<ProjectDTO>('project.executor.set', payload),
  taskOpen: (payload: { projectId: string; itemId: string; executor?: ProjectExecutor; workSessionId?: string; hubThreadId?: string }) =>
    request<{ brief: TaskBrief; executor: ProjectExecutor; itemId: string; hubThreadId?: string }>('project.task.open', payload),
  taskReport: (payload: { projectId: string; itemId: string; summary: string; executor?: ProjectExecutor; hubThreadId?: string }) =>
    request<{ itemId: string; reported: boolean }>('project.task.report', payload),
  taskComplete: (payload: { projectId: string; itemId: string; selfTestPass: boolean }) =>
    request<{ itemId: string; completed: boolean }>('project.task.complete', payload),
  returnFromTest: (payload: { projectId: string; testItemId: string; reason: string }) =>
    request<{ testItemId: string; devItemId: string; returned: boolean }>('project.task.returnFromTest', payload),
}

export function shortRootPath(path?: string): string {
  const value = (path ?? '').trim()
  if (!value) return ''
  const parts = value.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts[parts.length - 1] || value
}
