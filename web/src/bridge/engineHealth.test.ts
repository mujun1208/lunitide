import { expect, it } from 'vitest'
import { formatBridgeFailure } from './engineHealth'

it('wraps transport English for display and keeps protocol codes', () => {
  expect(formatBridgeFailure(
    { message: 'Failed to fetch', code: 'ENGINE_UNAVAILABLE', correlationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV' },
    '技能市场加载失败',
  )).toBe('技能市场加载失败（ENGINE_UNAVAILABLE · 01ARZ3NDEKTSV4RRFFQ69G5FAV）')
  expect(formatBridgeFailure(
    { message: '核心引擎暂时不可用', code: 'ENGINE_UNAVAILABLE', correlationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV' },
    '技能市场加载失败',
  )).toBe('核心引擎暂时不可用（ENGINE_UNAVAILABLE · 01ARZ3NDEKTSV4RRFFQ69G5FAV）')
  const conflict = formatBridgeFailure(
    { message: 'Failed to fetch', code: 'SETTINGS_VERSION_CONFLICT', correlationId: 'rev-9' },
    '专家市场加载失败',
  )
  expect(conflict).toBe('专家市场加载失败（SETTINGS_VERSION_CONFLICT · rev-9）')
  expect(conflict).toContain('SETTINGS_VERSION_CONFLICT')
  expect(conflict).not.toMatch(/版本冲突/)
  expect(formatBridgeFailure(
    { message: 'FEATURE_DISABLED: catalog inspect', code: 'FEATURE_DISABLED' },
    '技能市场加载失败',
  )).toBe('FEATURE_DISABLED: catalog inspect（FEATURE_DISABLED）')
})
