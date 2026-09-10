import { BridgeClientError } from '../bridge/client'

const TRANSPORT = /Failed to fetch|NetworkError|Load failed|network error|fetch failed/i

/** Transport-only wrap. Inspect notices and FEATURE_DISABLED English stay verbatim. */
export function officeStudioUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  if (/[\u4e00-\u9fff]/.test(detail)) return detail
  const code = err instanceof BridgeClientError ? err.code : ''
  if (code === 'ENGINE_UNAVAILABLE' || code === 'BRIDGE_UNAVAILABLE' || TRANSPORT.test(detail)) return fallback
  return detail || fallback
}
