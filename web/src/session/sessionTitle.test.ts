import { expect, it } from 'vitest'
import { displaySessionTitle, isColleagueChatTitle, isCompanionChatTitle, isProtectedSidebarChat, isRenameableChatTitle, titleFromFirstTurn } from './sessionTitle'

it('strips the leftover 同事 · prefix from bound colleague session titles', () => {
  expect(displaySessionTitle('同事 · PPT专家')).toBe('PPT专家')
  expect(displaySessionTitle('写周报')).toBe('写周报')
  expect(displaySessionTitle('同事对话')).toBe('同事对话')
})

it('treats leftover colleague titles as 同事聊天, not ordinary chats', () => {
  expect(isColleagueChatTitle('同事 · PPT专家')).toBe(true)
  expect(isColleagueChatTitle('同事·Excel表格制作专家')).toBe(true)
  expect(isColleagueChatTitle('同事对话')).toBe(true)
  expect(isColleagueChatTitle('你好月汐')).toBe(false)
  expect(isColleagueChatTitle('写周报')).toBe(false)
})

it('renames only placeholder titles — 月伴 stays a stable singleton title', () => {
  expect(isRenameableChatTitle('新对话')).toBe(true)
  expect(isRenameableChatTitle('New chat')).toBe(true)
  // 月伴对话 is the long-lived singleton: it must NOT be auto-renamed.
  expect(isRenameableChatTitle('月伴对话')).toBe(false)
  expect(isRenameableChatTitle('写周报')).toBe(false)
  expect(titleFromFirstTurn('  帮我写一份周报  ')).toBe('帮我写一份周报')
  expect([...titleFromFirstTurn('一二三四五六七八九十'.repeat(10))].length).toBe(80)
})

it('protects companion, placeholder, and fixed 对话 titles from delete/rename', () => {
  expect(isProtectedSidebarChat('月伴对话')).toBe(true)
  expect(isProtectedSidebarChat('Companion talk')).toBe(true)
  expect(isProtectedSidebarChat('新对话')).toBe(true)
  expect(isProtectedSidebarChat('对话')).toBe(true)
  expect(isProtectedSidebarChat('Chat')).toBe(true)
  expect(isProtectedSidebarChat('上海天气')).toBe(false)
})

it('identifies the 月伴 singleton title in both languages', () => {
  expect(isCompanionChatTitle('月伴对话')).toBe(true)
  expect(isCompanionChatTitle('Companion talk')).toBe(true)
  expect(isCompanionChatTitle(' 月伴对话 ')).toBe(true)
  expect(isCompanionChatTitle('写周报')).toBe(false)
})