import { expect, test, vi } from 'vitest'
import type { MessageDTO } from '../../generated/bridge'
import { resolveTalkHandoffMessage, talkHandoffMessages, validTalkHandoffText } from './talkHandoff'

const saved: MessageDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAA', role: 'user', status: 'completed', sequence: 1, text: '打开网页', createdAt: '2026-09-06T00:00:00Z' }

test('reuses only the authoritative user message acknowledged by realtime', async () => {
  const list = vi.fn().mockResolvedValue({ items: [saved] })
  expect(await resolveTalkHandoffMessage({ list }, saved.sessionId, saved.id, saved.text)).toEqual(saved)
  expect(list).toHaveBeenCalledWith(expect.objectContaining({ sessionId: saved.sessionId }))
})

test.each([
  { ...saved, sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAB' },
  { ...saved, role: 'assistant' },
  { ...saved, text: '不同的指令' },
  { ...saved, id: '01ARZ3NDEKTSV4RRFFQ69G5FAC' },
])('rejects a stale or foreign handoff reference %#', async item => {
  const list = vi.fn().mockResolvedValue({ items: [item] })
  await expect(resolveTalkHandoffMessage({ list }, saved.sessionId, saved.id, saved.text)).rejects.toMatchObject({ code: 'TALK_HANDOFF_SCOPE_INVALID' })
})

test('reconstructs an acknowledged long user final across history pages without accepting gaps', async () => {
  const second = {...saved, id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', sequence: 2, text: '并检查结果'}
  const list = vi.fn().mockResolvedValueOnce({items: [second], hasMore: true, nextCursor: 'older'}).mockResolvedValueOnce({items: [saved], hasMore: false})
  expect(await resolveTalkHandoffMessage({list}, saved.sessionId, saved.id, saved.text + second.text)).toEqual(saved)
  const broken = vi.fn().mockResolvedValue({items: [saved, {...second, sequence: 3}]})
  await expect(resolveTalkHandoffMessage({list: broken}, saved.sessionId, saved.id, saved.text + second.text)).rejects.toMatchObject({code: 'TALK_HANDOFF_SCOPE_INVALID'})
})

test('long persisted handoff keeps the whole prompt within each model message contract', () => {
  const text = '完整🙂'.repeat(18000)
  expect(validTalkHandoffText(text)).toBe(true)
  const messages = talkHandoffMessages(text)
  expect(messages.map(item => item.content).join('')).toBe(text)
  expect(messages.every(item => Array.from(item.content).length <= 16384)).toBe(true)
  expect(validTalkHandoffText('x'.repeat(512 * 1024 + 1))).toBe(false)
})
