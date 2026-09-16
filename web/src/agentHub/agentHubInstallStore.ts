import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'

export type InstallJobStatus = 'running' | 'done' | 'error'

export type InstallJob = {
  name: AgentHubName
  status: InstallJobStatus
  error?: string
  agents?: AgentHubStatus[]
  connected?: boolean
  hint?: string
}

type Listener = () => void

const jobs = new Map<AgentHubName, InstallJob>()
const inflight = new Map<AgentHubName, Promise<InstallJob>>()
const listeners = new Set<Listener>()

function emit() {
  for (const listener of listeners) listener()
}

export function getInstallJob(name: AgentHubName): InstallJob | undefined {
  return jobs.get(name)
}

export function subscribeInstallJobs(listener: Listener): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

/** Start or reuse an in-flight install. Survives sidebar/home unmount. */
export function startAgentInstall(name: AgentHubName): Promise<InstallJob> {
  const pending = inflight.get(name)
  if (pending) return pending
  const running: InstallJob = { name, status: 'running' }
  jobs.set(name, running)
  emit()
  const work = agentHubApi.install({ name, confirmed: true })
    .then(got => {
      const next: InstallJob = {
        name,
        status: got.connected ? 'done' : 'error',
        agents: got.agents ?? [],
        connected: got.connected,
        hint: got.hint,
        error: got.connected ? undefined : (got.hint || 'install incomplete'),
      }
      jobs.set(name, next)
      emit()
      return next
    })
    .catch(err => {
      const message = err instanceof Error ? err.message : 'install failed'
      const next: InstallJob = { name, status: 'error', error: message }
      jobs.set(name, next)
      emit()
      return next
    })
    .finally(() => {
      if (inflight.get(name) === work) inflight.delete(name)
    })
  inflight.set(name, work)
  return work
}

export function clearInstallJob(name: AgentHubName) {
  if (jobs.get(name)?.status === 'running' || inflight.has(name)) return
  jobs.delete(name)
  emit()
}
