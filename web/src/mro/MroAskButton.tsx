import React, { useState } from 'react'
import { createMutationAttempt, type ExpertBridge, type ProjectBridge, type SessionBridge } from '../bridge/client'
import type { ProjectDTO, SessionDTO } from '../generated/bridge'
import { useZh } from '../i18n/language'
import { parseMroContext, type MroSessionContext } from './mroContext'

export type MroChatOpened = { sessionId: string; project: ProjectDTO; session: SessionDTO; prompt?: string }

export async function openMroChat(input: {
  ensureProject: (projects: ProjectBridge) => Promise<ProjectDTO>
  projects: ProjectBridge
  sessions: SessionBridge
  experts: Pick<ExpertBridge, 'sessionMountSet' | 'mount'>
  mroExpertId: string
  context: MroSessionContext
  prompt?: string
}): Promise<MroChatOpened> {
  const context = parseMroContext(input.context)
  if (!context) throw new Error('mro context invalid')
  if (input.mroExpertId.length !== 26) throw new Error('mro expert missing')
  const project = await input.ensureProject(input.projects)
  const title = (input.prompt?.trim() || (context.scenario === 'fault' ? '排故' : context.scenario === 'checklist' ? '检查单' : '机务手册')).slice(0, 80)
  const payload = { projectId: project.id, title }
  const session = await input.sessions.create(payload, { attempt: createMutationAttempt('session.create', payload) })
  if (!input.sessions.metadataSet) throw new Error('session metadata unavailable')
  const meta = { sessionId: session.id, mroContext: context }
  await input.sessions.metadataSet(meta, { attempt: createMutationAttempt('session.metadata.set', meta) })
  const mount = { sessionId: session.id, expertIds: [input.mroExpertId] }
  if (input.experts.sessionMountSet) {
    await input.experts.sessionMountSet(mount, { attempt: createMutationAttempt('session.experts.set', mount) })
  }
  return { sessionId: session.id, project, session, prompt: input.prompt }
}

export function MroAskButton({
  mroExpertId, context, prompt, onOpened, openChat, disabled,
}: {
  mroExpertId: string
  context: MroSessionContext
  prompt?: string
  onOpened?: (opened: MroChatOpened) => void
  openChat?: typeof openMroChat
  disabled?: boolean
}): React.JSX.Element {
  const zh = useZh()
  const [busy, setBusy] = useState(false)
  const missing = !mroExpertId
  const blocked = missing || !!disabled
  return (
    <button
      type="button"
      className="primary"
      disabled={blocked || busy}
      title={missing ? (zh ? '先启用航空机务专家' : 'Enable the aviation MRO expert first') : undefined}
      onClick={() => {
        if (blocked || busy || !openChat) return
        setBusy(true)
        void openChat({
          ensureProject: async () => { throw new Error('ensureProject missing') },
          projects: {} as ProjectBridge,
          sessions: {} as SessionBridge,
          experts: {} as Pick<ExpertBridge, 'sessionMountSet' | 'mount'>,
          mroExpertId,
          context,
          prompt,
        }).then(opened => { onOpened?.(opened) }).finally(() => setBusy(false))
      }}
    >
      {zh ? '问月汐' : 'Ask Lunitide'}
    </button>
  )
}
