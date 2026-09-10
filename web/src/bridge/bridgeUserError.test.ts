import { expect, it } from 'vitest'
import { BridgeClientError } from './client'
import { asUserBridgeError } from './bridgeUserError'

it('wraps transport English on BridgeClientError and keeps protocol fields', () => {
  const wrapped = asUserBridgeError(
    new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, '01ARZ3NDEKTSV4RRFFQ69G5FAV'),
    '请求失败',
  )
  expect(wrapped.message).toBe('请求失败')
  expect(wrapped.code).toBe('ENGINE_UNAVAILABLE')
  expect(wrapped.retryable).toBe(true)
  expect(wrapped.correlationId).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAV')
  const chinese = new BridgeClientError('项目清单读取失败', 'ENGINE_UNAVAILABLE', true, 'engine')
  expect(asUserBridgeError(chinese, '请求失败')).toBe(chinese)
  const inspect = asUserBridgeError(
    new BridgeClientError('FEATURE_DISABLED: catalog inspect', 'FEATURE_DISABLED', false, 'engine'),
    '请求失败',
  )
  expect(inspect.message).toBe('FEATURE_DISABLED: catalog inspect')
  expect(inspect.code).toBe('FEATURE_DISABLED')
  const conflict = asUserBridgeError(
    new BridgeClientError('Failed to fetch', 'SETTINGS_VERSION_CONFLICT', false, 'rev-9'),
    '请求失败',
  )
  expect(conflict.message).toBe('请求失败')
  expect(conflict.code).toBe('SETTINGS_VERSION_CONFLICT')
})
