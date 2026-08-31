export interface MentionableMember {
  subjectId: string
  nickname: string
  avatar?: string
}

export type MentionableAgent = MentionableMember

export function mentionQuery(draft: string): string | null {
  const match = /(?:^|\s)@([^\s@]*)$/.exec(draft)
  return match ? match[1] : null
}

export function filterMentionMembers(members: MentionableMember[], query: string): MentionableMember[] {
  const q = query.trim().toLowerCase()
  return members.filter(member => !q || member.nickname.toLowerCase().includes(q) || member.subjectId.toLowerCase().includes(q))
}

export function filterMentionAgents(agents: MentionableMember[], query: string): MentionableMember[] {
  return filterMentionMembers(agents, query)
}

export function insertMention(draft: string, nickname: string): string {
  return draft.replace(/(^|\s)@[^\s@]*$/, `$1@${nickname} `)
}

export function parseClaimedTasks(bodies: string[]): Array<{ task: string; owner: string }> {
  const out: Array<{ task: string; owner: string }> = []
  const seen = new Set<string>()
  let lastTask = ''
  for (const body of bodies) {
    const task = /认领\s+(.+)$/m.exec(body)?.[1]?.trim()
    if (task && !/已经由/.test(body)) lastTask = task
    const owner = /已经由「([^」]+)」认领/.exec(body)?.[1]
    if (owner && lastTask && !seen.has(lastTask)) {
      seen.add(lastTask)
      out.push({ task: lastTask, owner })
    }
  }
  return out
}

export function pendingClaimTask(body: string): string {
  const text = body.trim()
  const explicit = /认领\s+(.+)$/m.exec(text)
  if (explicit) return explicit[1].trim()
  return ''
}
