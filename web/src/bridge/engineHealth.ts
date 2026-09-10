export const ENGINE_UNAVAILABLE_EVENT = 'lunitide:engine-unavailable'
export const ENGINE_RECOVERED_EVENT = 'lunitide:engine-recovered'

export type EngineUnavailableDetail = { code: string; correlationId: string }

export function emitEngineUnavailable(detail: EngineUnavailableDetail): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(ENGINE_UNAVAILABLE_EVENT, { detail }))
}

export function emitEngineRecovered(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(ENGINE_RECOVERED_EVENT))
}

const TRANSPORT = /Failed to fetch|NetworkError|Load failed|network error|fetch failed/i

function visibleBridgeMessage(error: { message: string; code?: string }, fallback: string): string {
  const detail = (error.message || '').trim()
  if (/[\u4e00-\u9fff]/.test(detail)) return detail
  const code = (error.code || '').trim()
  if (code === 'ENGINE_UNAVAILABLE' || code === 'BRIDGE_UNAVAILABLE' || TRANSPORT.test(detail)) return fallback
  return detail || fallback
}

export function formatBridgeFailure(error: { message: string; code?: string; correlationId?: string }, fallback: string): string {
  if (!error?.message && !error?.code) return fallback
  const code = error.code?.trim()
  const id = error.correlationId?.trim()
  const message = visibleBridgeMessage(error, fallback)
  if (code && id) return `${message}（${code} · ${id}）`
  if (code) return `${message}（${code}）`
  return message || fallback
}
