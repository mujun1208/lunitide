import { expect, it } from 'vitest'
import { displaySessionTitle, isRenameableChatTitle, titleFromFirstTurn } from './sessionTitle'

it('strips the leftover 同事 · prefix from bound colleague session titles', () => {
  expect(displaySessionTitle('同事 · PPT专家')).toBe('PPT专家')
  expect(displaySessionTitle('写周报')).toBe('写周报')
  expect(displaySessionTitle('同事对话')).toBe('同事对话')
})

it('renames placeholder and companion titles from the first turn', () => {
  expect(isRenameableChatTitle('新对话')).toBe(true)
  expect(isRenameableChatTitle('月伴对话')).toBe(true)
  expect(isRenameableChatTitle('写周报')).toBe(false)
  expect(titleFromFirstTurn('  帮我写一份周报  ')).toBe('帮我写一份周报')
  expect([...titleFromFirstTurn('一二三四五六七八九十'.repeat(10))].length).toBe(80)
})