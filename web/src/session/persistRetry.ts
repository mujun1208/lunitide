export const PERSIST_RETRY_SENTINEL = '\u2063persist-retry'
export const persistFailedKey = (sessionId: string) => `lunitide:persist-failed:${sessionId}`

export type PersistFailedRecord = { draft?: string }

export function readPersistFailed(sessionId: string): PersistFailedRecord | undefined {
  try {
    const raw = localStorage.getItem(persistFailedKey(sessionId))
    if (!raw) return undefined
    if (raw === '1') return {}
    const parsed = JSON.parse(raw) as { draft?: unknown }
    if (!parsed || typeof parsed !== 'object') return {}
    return { draft: typeof parsed.draft === 'string' ? parsed.draft : undefined }
  } catch {
    return {}
  }
}

export function writePersistFailed(sessionId: string, draft?: string): void {
  try {
    localStorage.setItem(persistFailedKey(sessionId), JSON.stringify({ draft: draft ?? '' }))
  } catch { /* quota / private mode */ }
}

export function clearPersistFailed(sessionId: string): void {
  try {
    localStorage.removeItem(persistFailedKey(sessionId))
  } catch { /* ignore */ }
}
