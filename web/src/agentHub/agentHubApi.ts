import type { InboxFile } from './agentHubCopy'

export type AgentHubName = 'codex' | 'cursor' | 'kimi'
export type AgentHubState = 'available' | 'not_installed' | 'not_logged_in' | 'unknown'
export type AgentHubTaskStatus = 'queued' | 'running' | 'success' | 'failed' | 'timeout' | 'cancelled'

export type AgentHubStatus = {
  name: AgentHubName
  state: AgentHubState
  version: string
  nonInteractive: boolean
  streamJSON: boolean
  hint: string
}

export type AgentHubEvent = {
  seq: number
  type: string
  title: string
  detail: string
  ts: string
}

export type AgentHubArtifact = {
  name: string
  path: string
  size: number
  mime: string
  source: string
  taskId?: string
  agent?: string
  createdAt?: string
}

export type AgentHubTask = {
  taskId: string
  agent: string
  prompt: string
  workDir: string
  sandbox: string
  status: string
  exitCode?: number | null
  tokensUsed: number
  errorMsg: string
  createdAt: string
  startedAt?: string
  finishedAt?: string
}

export type AgentHubTaskDetail = {
  task: AgentHubTask
  events: AgentHubEvent[]
  artifacts: AgentHubArtifact[]
}

export type AgentHubCounts = {
  queued: number
  running: number
  success: number
  failed: number
}

export type AgentHubPreview = {
  kind: string
  path: string
  absolutePath?: string
  size: number
  content: string
  notice?: string
}

type RequestFn = <T>(method: string, payload: object) => Promise<T>

let requestImpl: RequestFn | undefined

export function setAgentHubRequest(fn: RequestFn | undefined): void {
  requestImpl = fn
}

async function request<T>(method: string, payload: object): Promise<T> {
  if (requestImpl) return requestImpl(method, payload)
  const { getAgentHubBridge } = await import('../bridge/client')
  return getAgentHubBridge().request<T>(method, payload)
}

export const agentHubApi = {
  detect: () => request<{ agents: AgentHubStatus[] }>('agentHub.detect', {}),
  pickDir: () => request<{ canceled: boolean; path: string }>('agentHub.dir.pick', {}),
  start: (payload: {
    taskId?: string
    agent: AgentHubName
    prompt: string
    workDir?: string
    sandbox?: string
    timeoutMin?: number
    idempotencyKey: string
  }) => request<AgentHubTaskDetail>('agentHub.task.start', payload),
  get: (payload: { taskId: string }) => request<AgentHubTaskDetail>('agentHub.task.get', payload),
  cancel: (payload: { taskId: string }) => request<AgentHubTaskDetail>('agentHub.task.cancel', payload),
  list: (payload?: { agent?: string; status?: string; dateFrom?: string; dateTo?: string }) =>
    request<{ items: AgentHubTask[]; counts: AgentHubCounts }>('agentHub.task.list', payload ?? {}),
  listArtifacts: (payload?: { agent?: string; dateFrom?: string; dateTo?: string; ext?: string }) =>
    request<{ items: AgentHubArtifact[] }>('agentHub.artifact.list', payload ?? {}),
  preview: (payload: { taskId: string; path: string }) => request<AgentHubPreview>('agentHub.file.preview', payload),
  open: (payload: { taskId: string; path?: string; reveal?: boolean }) =>
    request<{ opened: string }>('agentHub.file.open', payload),
  inbox: (payload: { action: 'files' | 'folder' | 'list' | 'drop'; workDir?: string; name?: string }) =>
    request<{ canceled: boolean; workDir: string; files: InboxFile[]; skipped?: string[] }>('agentHub.inbox', payload),
}
