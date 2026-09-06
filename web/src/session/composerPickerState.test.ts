import {expect, test} from 'vitest'
import {composerPickerEmpty, composerPickerFailed, composerPickerLoading} from './composerPickerState'

test('failed picker is not an empty catalog', () => {
  const failed = composerPickerFailed('skill', 'ENGINE_UNAVAILABLE')
  expect(failed.status).toBe('failed')
  expect(failed.message).toContain('技能列表暂时读不到')
  expect(failed.message).not.toBe(composerPickerEmpty('skill'))
  expect(composerPickerLoading().status).toBe('loading')
})

test('empty copy differs by kind', () => {
  expect(composerPickerEmpty('at')).toContain('还没有可引用')
  expect(composerPickerEmpty('expert')).toContain('已启用专家')
})
