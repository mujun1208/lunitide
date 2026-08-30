/** Host/engine admission and stream-limit failures are retryable, not “无法执行”. */
export function isCompanionInfraBusy(code: string): boolean {
  return code === 'HOST_BUSY' || code === 'ENGINE_BUSY' || code === 'STREAM_LIMIT_REACHED'
}

export function errorLooksInfraBusy(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const code = 'code' in error ? String((error as { code: unknown }).code) : ''
  return isCompanionInfraBusy(code)
}

export async function withInfraBusyRetry<T>(run: () => Promise<T>, attempts = 4): Promise<T> {
  let last: unknown
  for (let i = 1; i <= attempts; i++) {
    try {
      return await run()
    } catch (error) {
      last = error
      if (!errorLooksInfraBusy(error) || i === attempts) throw error
      await new Promise<void>(resolve => {
        window.setTimeout(resolve, 160 * i)
      })
    }
  }
  throw last
}
