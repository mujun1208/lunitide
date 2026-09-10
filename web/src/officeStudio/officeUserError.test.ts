import { expect, it } from 'vitest'
import { BridgeClientError } from '../bridge/client'
import { officeStudioUserError } from './officeUserError'

it('wraps only transport or generic English and keeps inspect or FEATURE_DISABLED text', () => {
  expect(officeStudioUserError(new Error('Failed to fetch'), '操作没有完成，请重试。')).toBe('操作没有完成，请重试。')
  expect(officeStudioUserError(new Error('NetworkError when attempting to fetch resource.'), '版本对比暂时不可用，请重试。')).toBe('版本对比暂时不可用，请重试。')
  expect(officeStudioUserError(new BridgeClientError('engine down', 'ENGINE_UNAVAILABLE', true, 'engine'), '原对话加载失败。')).toBe('原对话加载失败。')
  expect(officeStudioUserError(new Error('FEATURE_DISABLED: office inspect unavailable'), '操作没有完成，请重试。')).toBe('FEATURE_DISABLED: office inspect unavailable')
  expect(officeStudioUserError(new Error('FEATURE_DISABLED: 办公工作台已关闭'), '操作没有完成，请重试。')).toBe('FEATURE_DISABLED: 办公工作台已关闭')
  expect(officeStudioUserError(new Error('对比结果与所选版本不一致，请重试。'), '版本对比暂时不可用，请重试。')).toBe('对比结果与所选版本不一致，请重试。')
})
