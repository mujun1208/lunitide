import { describe, expect, test, vi } from 'vitest'
import { errorLooksInfraBusy, isCompanionInfraBusy, withInfraBusyRetry } from './companionBusy'

describe('companion infra busy', () => {
  test('names the host/engine busy codes', () => {
    expect(isCompanionInfraBusy('HOST_BUSY')).toBe(true)
    expect(isCompanionInfraBusy('ENGINE_BUSY')).toBe(true)
    expect(isCompanionInfraBusy('STREAM_LIMIT_REACHED')).toBe(true)
    expect(isCompanionInfraBusy('UPSTREAM_FAILED')).toBe(false)
  })

  test('retries HOST_BUSY then succeeds', async () => {
    vi.useFakeTimers()
    let n = 0
    const pending = withInfraBusyRetry(async () => {
      n += 1
      if (n < 3) {
        const error = new Error('桌面主机正忙，请稍后重试') as Error & { code: string }
        error.code = 'HOST_BUSY'
        throw error
      }
      return 'ok'
    })
    await vi.advanceTimersByTimeAsync(2000)
    await expect(pending).resolves.toBe('ok')
    expect(n).toBe(3)
    vi.useRealTimers()
  })

  test('does not retry a real tool failure', async () => {
    const error = new Error('找不到证件号码') as Error & { code: string }
    error.code = 'DESKTOP_TYPE_FAILED'
    await expect(withInfraBusyRetry(async () => {
      throw error
    })).rejects.toBe(error)
    expect(errorLooksInfraBusy(error)).toBe(false)
  })
})
