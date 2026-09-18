import { afterEach, expect, it, vi } from 'vitest'
import { agentHubApi, type AgentHubStatus } from './agentHubApi'
import { clearInstallJob, getInstallJob, startAgentInstall } from './agentHubInstallStore'

type InstallResult = { agents: AgentHubStatus[]; installed: boolean; connected: boolean; hint: string }

vi.mock('./agentHubApi', () => ({
  agentHubApi: {
    install: vi.fn(),
  },
}))

afterEach(() => {
  clearInstallJob('codex')
  clearInstallJob('cursor')
  clearInstallJob('kimi')
  vi.clearAllMocks()
})

it('reuses an in-flight install so leaving the page does not start a second job', async () => {
  let finish: (value: InstallResult) => void = () => {}
  vi.mocked(agentHubApi.install).mockReturnValue(new Promise(resolve => { finish = resolve }))
  const first = startAgentInstall('codex')
  clearInstallJob('codex')
  const second = startAgentInstall('codex')
  expect(getInstallJob('codex')?.status).toBe('running')
  expect(agentHubApi.install).toHaveBeenCalledTimes(1)
  finish({ connected: true, agents: [], installed: true, hint: '' })
  await expect(first).resolves.toMatchObject({ status: 'done', connected: true })
  await expect(second).resolves.toMatchObject({ status: 'done', connected: true })
})
