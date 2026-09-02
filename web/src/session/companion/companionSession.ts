// The 月伴 (companion) voice mode is one long-lived singleton dialogue, not a
// new session per visit. Every entry points at the same conversation so the
// user's whole from-install-to-uninstall voice history lives in one place,
// switchable and viewable from the sidebar like any other chat.
//
// The singleton id is remembered locally (WebView2 persists localStorage
// across runs). If that pointer is ever lost — cleared storage, a fresh
// machine, or the user deleted the session — we re-discover the existing 月伴
// dialogue by its stable title before creating a new one, so a stale pointer
// never silently splinters the history into two.
import type { SessionBridge } from '../../bridge/client'
import { createMutationAttempt } from '../../bridge/client'
import type { SessionDTO } from '../../generated/bridge'
import { isCompanionChatTitle } from '../sessionTitle'

export const COMPANION_SESSION_ID_KEY = 'lunitide:companion-session-id'

/**
 * Pick the persistent 月伴 session out of a project's sessions.
 *
 * Preference order:
 *  1. the remembered id, when it still exists;
 *  2. otherwise the oldest 月伴-titled session (oldest = the original home, so
 *     repeated re-discovery always converges on the same one rather than the
 *     most recent accidental duplicate).
 *
 * Returns undefined when there is no companion session yet.
 */
export function pickCompanionSession(storedId: string, sessions: readonly SessionDTO[]): SessionDTO | undefined {
  const trimmed = storedId.trim()
  if (trimmed) {
    const remembered = sessions.find(s => s.id === trimmed)
    if (remembered) return remembered
  }
  const companions = sessions.filter(s => isCompanionChatTitle(s.title))
  if (companions.length === 0) return undefined
  return [...companions].sort((a, b) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id))[0]
}

function readStoredCompanionId(): string {
  try {
    return localStorage.getItem(COMPANION_SESSION_ID_KEY)?.trim() ?? ''
  } catch {
    return ''
  }
}

function rememberCompanionId(id: string): void {
  try {
    localStorage.setItem(COMPANION_SESSION_ID_KEY, id)
  } catch {
    /* private mode / storage disabled — the title fallback still re-discovers it */
  }
}

/**
 * Resolve the single 月伴 dialogue for a project, creating it once if needed.
 * The created session is pinned so it stays at the top of the sidebar as the
 * fixed home the user returns to, and keeps its stable 月伴对话 title (never
 * auto-renamed to a first utterance).
 */
export async function ensureCompanionSession(
  sessions: SessionBridge,
  projectId: string,
  zh: boolean,
): Promise<SessionDTO> {
  const title = zh ? '月伴对话' : 'Companion talk'
  const listed = await sessions.list({ projectId })
  const existing = pickCompanionSession(readStoredCompanionId(), listed.items)
  if (existing) {
    rememberCompanionId(existing.id)
    if (!existing.pinned) {
      try {
        const payload = { id: existing.id, title: existing.title, pinned: true, version: existing.version }
        const updated = await sessions.update(payload, { attempt: createMutationAttempt('session.update', payload) })
        // Pinning is a nice-to-have; the returned session must always be a real
        // one. A bridge/mock that resolves undefined must not blank the session.
        return updated ?? existing
      } catch {
        return existing
      }
    }
    return existing
  }
  const payload = { projectId, title }
  const attempt = createMutationAttempt('session.create', payload)
  const created = await sessions.create(attempt.payload, { attempt })
  rememberCompanionId(created.id)
  try {
    const pinPayload = { id: created.id, title: created.title, pinned: true, version: created.version }
    const pinned = await sessions.update(pinPayload, { attempt: createMutationAttempt('session.update', pinPayload) })
    // As above: never return undefined from a resolved-but-empty update.
    return pinned ?? created
  } catch {
    return created
  }
}
