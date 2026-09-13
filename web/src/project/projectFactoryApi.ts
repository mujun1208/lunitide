import { getAgentHubBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'

function request<T>(method: string, payload: object): Promise<T> {
  return getAgentHubBridge().request<T>(method, payload)
}

export const projectFactoryApi = {
  interviewGet: (payload: { projectId: string }) =>
    request<{ interview: unknown; phase1: unknown; phase2: unknown }>('project.interview.get', payload),
  interviewSave: (payload: { projectId: string; phase: number; mode: string; completedAt?: string; answers: Array<{ id: string; prompt: string; value: string }> }) =>
    request<{ interview: unknown }>('project.interview.save', payload),
  generate: (payload: { projectId: string; phase: number; documentTypes?: string[]; overwriteDrafts?: boolean; councilSynthesis?: string }) =>
    request<{ generated: number }>('project.deliverable.generate', payload),
  schemaGet: (payload: { projectId: string }) =>
    request<{ schema: { version: number; dialect: string; tables: Array<{ name: string; columns: Array<{ name: string; type: string }> }> }; dbPath?: string; dbStatus?: string }>('project.schema.get', payload),
  schemaPut: (payload: { projectId: string; schema: unknown }) =>
    request<{ schema: unknown }>('project.schema.put', payload),
  schemaMaterialize: (payload: { projectId: string; version: number }) =>
    request<{ project: ProjectDTO }>('project.schema.materialize', payload),
  schemaVerify: (payload: { projectId: string; version: number }) =>
    request<{ project: ProjectDTO }>('project.schema.verify', payload),
  boardGet: (payload: { projectId: string; boardKind: string }) =>
    request<{ board: { version: number; items: unknown[] }; stats: unknown; statsText: string }>('project.board.get', payload),
  boardSync: (payload: { projectId: string; boardKind: string }) =>
    request<{ board: unknown; stats: unknown; statsText: string; dirty: boolean }>('project.board.sync', payload),
  boardPut: (payload: { projectId: string; boardKind: string; board: unknown }) =>
    request<{ board: unknown; stats: unknown }>('project.board.put', payload),
  boardItemOpen: (payload: { projectId: string; boardKind: string; itemId: string }) =>
    request<{ brief: { itemId: string; title: string; text: string; rootPath: string }; itemId: string }>('project.board.item.open', payload),
  testRun: (payload: { projectId: string; itemId: string; kind: string; command?: string; evidence?: string }) =>
    request<{ result: unknown; item: unknown }>('project.test.run', payload),
  releaseSync: (payload: { projectId: string; destPath: string }) =>
    request<{ receipt: unknown }>('project.release.sync', payload),
}
