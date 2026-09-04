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

export function formatBridgeFailure(error: { message: string; code?: string; correlationId?: string }, fallback: string): string {
  if (!error?.message && !error?.code) return fallback
  const code = error.code?.trim()
  const id = error.correlationId?.trim()
  if (code && id) return `${error.message || fallback}（${code} · ${id}）`
  if (code) return `${error.message || fallback}（${code}）`
  return error.message || fallback
}
