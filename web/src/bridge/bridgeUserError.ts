import { BridgeClientError } from './client'

const TRANSPORT = /Failed to fetch|NetworkError|Load failed|network error|fetch failed/i

/** Transport-only wrap. FEATURE_DISABLED inspect English and protocol codes stay. */
export function bridgeTransportUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  if (/[\u4e00-\u9fff]/.test(detail)) return detail
  const code = err instanceof BridgeClientError ? err.code : ''
  if (code === 'ENGINE_UNAVAILABLE' || code === 'BRIDGE_UNAVAILABLE' || TRANSPORT.test(detail)) return fallback
  return detail || fallback
}

export function asUserBridgeError(error: unknown, fallback: string): BridgeClientError {
  const message = bridgeTransportUserError(error, fallback)
  if (error instanceof BridgeClientError) {
    if (message === error.message) return error
    return new BridgeClientError(message, error.code, error.retryable, error.correlationId)
  }
  return new BridgeClientError(message, 'CLIENT_ERROR', false, 'renderer')
}
