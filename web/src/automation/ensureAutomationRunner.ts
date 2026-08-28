import {
  createMutationAttempt,
  projectBridge,
  providerBridge,
  sessionBridge,
  type ProjectBridge,
  type ProviderBridge,
  type SessionBridge,
} from '../bridge/client'
import type { ProjectDTO, SessionDTO } from '../generated/bridge'
import { pickDefaultLLM } from '../provider/modelKind'

export const AUTOMATION_RUNNER_TITLE = '自动化执行'

export async function ensureAutomationRunner(
  projects: ProjectBridge = projectBridge,
  sessions: SessionBridge = sessionBridge,
): Promise<{ project: ProjectDTO; session: SessionDTO }> {
  const items = (await projects.list()).items
  const personal =
    items.find(item => item.name.startsWith('\u2063')) ??
    items.find(item => item.name.includes('普通对话'))
  if (!personal) {
    throw new Error('请先创建普通对话项目')
  }
  const listed = await sessions.list({ projectId: personal.id })
  const existing = listed.items.find(item => item.title === AUTOMATION_RUNNER_TITLE)
  if (existing) return { project: personal, session: existing }
  const payload = { projectId: personal.id, title: AUTOMATION_RUNNER_TITLE }
  const attempt = createMutationAttempt('session.create', payload)
  const session = await sessions.create(attempt.payload, { attempt })
  return { project: personal, session }
}

export async function loadDefaultModel(providers: ProviderBridge = providerBridge): Promise<{ providerId: string; modelId: string } | undefined> {
  const picked = pickDefaultLLM((await providers.list()).items)
  if (!picked) return undefined
  return { providerId: picked.provider.id, modelId: picked.modelId }
}
