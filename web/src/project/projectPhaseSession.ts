import { createMutationAttempt, type SessionBridge } from '../bridge/client'
import type { SessionDTO } from '../generated/bridge'

/** Machine-readable prefix so each workbench stage owns its own chat session. */
export const PHASE_SESSION_PREFIX = 'phase:'

export function phaseSessionTitle(phase: number, label: string): string {
  return `${PHASE_SESSION_PREFIX}${phase}:${label.trim()}`
}

export function parsePhaseSessionTitle(title: string): number | undefined {
  if (!title.startsWith(PHASE_SESSION_PREFIX)) return undefined
  const rest = title.slice(PHASE_SESSION_PREFIX.length)
  const colon = rest.indexOf(':')
  if (colon <= 0) return undefined
  const phase = Number.parseInt(rest.slice(0, colon), 10)
  return Number.isInteger(phase) && phase >= 1 && phase <= 9 ? phase : undefined
}

export function isPhaseSession(session: SessionDTO): boolean {
  return parsePhaseSessionTitle(session.title) !== undefined
}

export function findPhaseSession(items: readonly SessionDTO[], phase: number): SessionDTO | undefined {
  return items.find(item => parsePhaseSessionTitle(item.title) === phase)
}

export function legacyProjectSessions(items: readonly SessionDTO[]): SessionDTO[] {
  return items.filter(item => !isPhaseSession(item))
}

function pickLatest(sessions: readonly SessionDTO[]): SessionDTO | undefined {
  return [...sessions].sort(
    (a, b) =>
      (b.updatedAt || b.createdAt).localeCompare(a.updatedAt || a.createdAt) ||
      b.id.localeCompare(a.id),
  )[0]
}

async function renameLegacySession(
  sessions: SessionBridge,
  session: SessionDTO,
  phase: number,
  label: string,
): Promise<SessionDTO> {
  const title = phaseSessionTitle(phase, label)
  if (session.title === title) return session
  const payload = { id: session.id, title, pinned: session.pinned, version: session.version }
  return sessions.update(payload, { attempt: createMutationAttempt('session.update', payload) })
}

/** Resolve or create the chat session bound to one project workbench phase. */
export async function ensureProjectPhaseSession(
  sessions: SessionBridge,
  projectId: string,
  phase: number,
  label: string,
  options: { initialSession?: SessionDTO; migrateLegacy?: boolean } = {},
): Promise<SessionDTO> {
  const listed = await sessions.list({ projectId })
  const existing = findPhaseSession(listed.items, phase)
  if (existing) return existing

  const migrateLegacy = options.migrateLegacy ?? true
  if (migrateLegacy && phase === 1) {
    const legacy = legacyProjectSessions(listed.items)
    const initial =
      options.initialSession && !isPhaseSession(options.initialSession)
        ? options.initialSession
        : undefined
    const candidate = initial ?? (legacy.length === 1 ? legacy[0] : pickLatest(legacy))
    if (candidate) {
      return renameLegacySession(sessions, candidate, phase, label)
    }
  }

  const payload = { projectId, title: phaseSessionTitle(phase, label) }
  return sessions.create(payload, { attempt: createMutationAttempt('session.create', payload) })
}
