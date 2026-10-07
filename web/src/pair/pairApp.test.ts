import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearRemoteCredentials, loadRemoteCredentials } from '../bridge/wsTransport'
import { detectDeviceName, detectPlatform, pairWithGateway, parsePairHash } from './pairApp'

const jsonResponse = (status: number, body: unknown) => {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return new Response(text, { status, headers: { 'Content-Type': 'application/json' } })
}

describe('parsePairHash', () => {
  it('extracts code and fingerprint from a canonical QR hash', () => {
    expect(parsePairHash('#c=12345678&fp=abcd1234abcd1234')).toEqual({ code: '12345678', fingerprint: 'abcd1234abcd1234' })
  })
  it('rejects malformed codes and empty hashes', () => {
    expect(parsePairHash('')).toBeUndefined()
    expect(parsePairHash('#')).toBeUndefined()
    expect(parsePairHash('#c=1234&fp=ab')).toBeUndefined()
    expect(parsePairHash('#c=1234567x')).toBeUndefined()
    expect(parsePairHash('#fp=abcd1234abcd1234')).toBeUndefined()
  })
  it('keeps a valid code even without a fingerprint', () => {
    expect(parsePairHash('#c=87654321')).toEqual({ code: '87654321', fingerprint: '' })
  })
})

describe('device detection', () => {
  it('maps user agents to friendly names and platform ids', () => {
    expect(detectDeviceName('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)')).toBe('iPhone')
    expect(detectDeviceName('Mozilla/5.0 (Linux; Android 14; Pixel 8)')).toBe('Android 手机')
    expect(detectDeviceName('Mozilla/5.0 (Windows NT 10.0)')).toBe('Windows 设备')
    expect(detectDeviceName('curl/8.0')).toBe('手机')
    expect(detectPlatform('iPhone')).toBe('ios-pwa')
    expect(detectPlatform('Android')).toBe('android-pwa')
    expect(detectPlatform('Firefox')).toBe('mobile-web')
  })
})

describe('pairWithGateway', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => { localStorage.clear(); vi.restoreAllMocks() })

  it('returns the device token on success', async () => {
    const fetchFn = vi.fn().mockResolvedValue(jsonResponse(200, { deviceToken: 'tok', expiresAt: '2027-01-01T00:00:00Z' }))
    const outcome = await pairWithGateway({ code: '12345678', deviceName: 'iPhone', platform: 'ios-pwa' }, fetchFn as unknown as typeof fetch)
    expect(outcome).toEqual({ ok: true, result: { deviceToken: 'tok', expiresAt: '2027-01-01T00:00:00Z' } })
    const [input, init] = fetchFn.mock.calls[0] as [string, RequestInit]
    expect(input).toBe('/api/pair')
    expect(JSON.parse(String(init.body))).toEqual({ code: '12345678', deviceName: 'iPhone', platform: 'ios-pwa' })
  })

  it('maps network failure, disabled gateway and bad codes to user guidance', async () => {
    const failureMessage = async (response?: Response): Promise<string> => {
      const fetchFn = response
        ? vi.fn().mockResolvedValue(response) as unknown as typeof fetch
        : vi.fn().mockRejectedValue(new TypeError('offline')) as unknown as typeof fetch
      const outcome = await pairWithGateway({ code: '12345678', deviceName: 'x', platform: 'x' }, fetchFn)
      if (outcome.ok) throw new Error('expected failure')
      return outcome.error
    }
    expect(await failureMessage()).toContain('无法连接')
    expect(await failureMessage(new Response('remote access disabled', { status: 503 }))).toContain('远程访问')
    expect(await failureMessage(jsonResponse(403, '配对码无效或已过期'))).toContain('重新生成')
    expect(await failureMessage(new Response('配对失败次数过多，已临时锁定', { status: 403 }))).toContain('锁定')
  })

  it('rejects a success envelope without a token', async () => {
    const outcome = await pairWithGateway({ code: '12345678', deviceName: 'x', platform: 'x' }, vi.fn().mockResolvedValue(jsonResponse(200, { deviceToken: '' })) as unknown as typeof fetch)
    expect(outcome.ok).toBe(false)
  })

  it('credentials round-trip after a successful pairing', async () => {
    const fetchFn = vi.fn().mockResolvedValue(jsonResponse(200, { deviceToken: 'persisted', expiresAt: '2027-01-01T00:00:00Z' }))
    const outcome = await pairWithGateway({ code: '12345678', deviceName: 'iPhone', platform: 'ios-pwa' }, fetchFn as unknown as typeof fetch)
    if (!outcome.ok) throw new Error('expected success')
    const { saveRemoteCredentials } = await import('../bridge/wsTransport')
    saveRemoteCredentials({ wsUrl: 'wss://192.0.2.5:47651/bridge', token: outcome.result.deviceToken })
    expect(loadRemoteCredentials()).toEqual({ wsUrl: 'wss://192.0.2.5:47651/bridge', token: 'persisted' })
    clearRemoteCredentials()
    expect(loadRemoteCredentials()).toBeUndefined()
  })
})
