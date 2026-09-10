import { BridgeClientError } from '../../bridge/client'

const TRANSPORT = /Failed to fetch|NetworkError|Load failed|network error|fetch failed/i

/** Transport-only wrap. Protocol codes stay on the caller; inspect English stays verbatim. */
export function speechTransportUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  if (/[\u4e00-\u9fff]/.test(detail)) return detail
  const code = err instanceof BridgeClientError ? err.code : ''
  if (code === 'ENGINE_UNAVAILABLE' || code === 'BRIDGE_UNAVAILABLE' || TRANSPORT.test(detail)) return fallback
  return detail || fallback
}

export function asSpeechBridgeError(error: unknown, fallback: string): BridgeClientError {
  const message = speechTransportUserError(error, fallback)
  if (error instanceof BridgeClientError) {
    if (message === error.message) return error
    return new BridgeClientError(message, error.code, error.retryable, error.correlationId)
  }
  return new BridgeClientError(message, 'SPEECH_RECOGNITION_UNAVAILABLE', false, 'renderer')
}
