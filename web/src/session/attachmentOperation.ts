export const attachmentCancelled = () => new DOMException('附件操作已取消', 'AbortError')

// Time limits and cancellation also cover browser decoding and native file reads,
// which do not necessarily settle when the engine request deadline expires.
export function attachmentOperation<T>(operation: PromiseLike<T>, signal?: AbortSignal, timeoutMs = 30_000, message = '附件处理超时，请重试'): Promise<T> {
  return new Promise((resolve, reject) => {
    let settled = false
    const finish = (action: () => void) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      signal?.removeEventListener('abort', cancel)
      action()
    }
    const cancel = () => finish(() => reject(attachmentCancelled()))
    const timer = setTimeout(() => finish(() => reject(new Error(message))), timeoutMs)
    Promise.resolve(operation).then(value => finish(() => resolve(value)), error => finish(() => reject(error)))
    signal?.addEventListener('abort', cancel, {once: true})
    if (signal?.aborted) cancel()
  })
}
