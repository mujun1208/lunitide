import { expect, it } from 'vitest'
import { messageSize, validMessageText } from './messageLimits'
import { validPrompt } from '../app/appHelpers'
it('preserves long descriptions and counts Unicode code points rather than UTF-16 units', () => {
  expect(validMessageText('描述'.repeat(2000))).toBe(true)
  const full = '😀'.repeat(32768)
  expect(messageSize(full)).toEqual({ characters: 32768, bytes: 131072 })
  expect(validMessageText(full)).toBe(true)
  expect(validPrompt(full)).toBe(true)
  expect(validPrompt(full + 'a')).toBe(false)
  expect(validMessageText('a'.repeat(32769))).toBe(false)
  expect(validMessageText('a\0b')).toBe(false)
  expect(validMessageText('')).toBe(false)
})
