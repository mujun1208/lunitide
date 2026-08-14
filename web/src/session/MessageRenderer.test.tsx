import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type MessageBridge, type SessionBridge } from '../bridge/client'
import type { MessageDTO, ProjectDTO, SessionDTO } from '../generated/bridge'
import { SessionPage } from './SessionPage'

afterEach(cleanup)
const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV', S1 = '01ARZ3NDEKTSV4RRFFQ69G5FAA', S2 = '01ARZ3NDEKTSV4RRFFQ69G5FAB', NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = { id: P, name: 'Messages', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const sessions: SessionDTO[] = [S1, S2].map((id, i) => ({ id, projectId: P, title: `Session ${i + 1}`, pinned: false, status: 'active', createdAt: `2025-01-01T00:00:0${i}Z`, updatedAt: `2025-01-01T00:00:0${i}Z`, version: 1 }))
const message = (sequence: number, overrides: Partial<MessageDTO> = {}): MessageDTO => ({ id: sequence === 1 ? '01ARZ3NDEKTSV4RRFFQ69G5FAC' : sequence === 2 ? '01ARZ3NDEKTSV4RRFFQ69G5FAD' : '01ARZ3NDEKTSV4RRFFQ69G5FAE', sessionId: S1, role: 'user', status: 'completed', sequence, text: `message-${sequence}`, createdAt: `2025-01-01T00:00:0${sequence}Z`, ...overrides })
const sessionBridge: SessionBridge = { list: vi.fn().mockResolvedValue({ items: sessions }), create: vi.fn(), update: vi.fn(), delete: vi.fn() }
const page = (items: MessageDTO[] = [], nextCursor: string | null = null) => ({ items, hasMore: nextCursor !== null, nextCursor, snapshotSequence: items.reduce((n, x) => Math.max(n, x.sequence), 0) })
const messages = (part: Partial<MessageBridge> = {}): MessageBridge => ({ list: vi.fn().mockResolvedValue(page()), append: vi.fn().mockResolvedValue(message(1)), ...part })
async function open(bridge: MessageBridge, title = 'Session 1') { const user = userEvent.setup(); render(<SessionPage project={project} bridge={sessionBridge} messages={bridge} onBack={vi.fn()} />); await user.click(await screen.findByText(title)); return user }

it('Message Renderer merges backward pages into ascending UI order without duplicates', async () => {
  const list = vi.fn().mockResolvedValueOnce(page([message(3), message(2)], 'older')).mockResolvedValueOnce(page([message(2), message(1)]))
  const user = await open(messages({ list }))
  await screen.findByText('message-3')
  await user.click(screen.getByRole('button', { name: '加载更早' }))
  await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
  expect(list.mock.calls[0][0]).toEqual({ sessionId: S1, direction: 'backward', limit: 64, byteBudget: 131072 })
  expect(list.mock.calls[1][0]).toEqual({ sessionId: S1, direction: 'backward', cursor: 'older', limit: 64, byteBudget: 131072 })
  expect(within(screen.getByRole('list')).getAllByRole('listitem').map(x => x.textContent?.match(/message-\d/)?.[0])).toEqual(['message-1', 'message-2', 'message-3'])
})

it('Message Renderer refreshes the latest first page after append succeeds', async () => {
  const list = vi.fn().mockResolvedValueOnce(page([message(1)])).mockResolvedValueOnce(page([message(2), message(1)])), append = vi.fn().mockResolvedValue(message(2)), user = await open(messages({ list, append }))
  await screen.findByText('message-1'); await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), 'new text'); await user.click(screen.getByRole('button', { name: '仅保存' }))
  await screen.findByText('message-2')
  expect(append).toHaveBeenCalledWith({ sessionId: S1, text: 'new text' }, expect.objectContaining({ attempt: expect.any(Object) }))
  expect(list).toHaveBeenLastCalledWith({ sessionId: S1, direction: 'backward', limit: 64, byteBudget: 131072 })
})

it('Message Renderer ignores an old session list response after switching sessions', async () => {
  let oldResolve!: (value: any) => void
  const list = vi.fn().mockReturnValueOnce(new Promise(resolve => { oldResolve = resolve })).mockResolvedValueOnce(page([message(1, { sessionId: S2, text: 'new-session' })]))
  const user = await open(messages({ list })); await user.click(screen.getByRole('button', { name: '关闭' })); await user.click(screen.getByText('Session 2')); expect(await screen.findByText('new-session')).toBeInTheDocument()
  await act(async () => oldResolve(page([message(1, { text: 'stale-session' })])))
  expect(screen.queryByText('stale-session')).toBeNull()
})

it('Message Renderer ignores an old session append response and does not refresh or pollute the next session', async () => {
  let appendResolve!: (value: MessageDTO) => void
  const list = vi.fn().mockResolvedValueOnce(page()).mockResolvedValueOnce(page([message(1, { sessionId: S2, text: 'session-two' })])), append = vi.fn().mockReturnValue(new Promise(resolve => { appendResolve = resolve })), user = await open(messages({ list, append }))
  await screen.findByText('还没有消息'); await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'), 'old append'); await user.click(screen.getByRole('button', { name: '仅保存' })); await user.click(screen.getByRole('button', { name: '关闭' })); await user.click(screen.getByText('Session 2')); expect(await screen.findByText('session-two')).toBeInTheDocument()
  await act(async () => appendResolve(message(1, { text: 'late append' })))
  expect(screen.queryByText('late append')).toBeNull(); expect(list).toHaveBeenCalledTimes(2)
})

it('Message Renderer shows loading and error, then retries from latest page and recovers from an invalid cursor', async () => {
  let initialReject!: (error: unknown) => void
  const list = vi.fn().mockReturnValueOnce(new Promise((_r, reject) => { initialReject = reject })).mockResolvedValueOnce(page([message(2)], 'invalid-cursor')).mockRejectedValueOnce(new BridgeClientError('cursor invalid', 'INVALID_CURSOR', false, 'trace')).mockResolvedValueOnce(page([message(3)]))
  const user = await open(messages({ list })); expect(screen.getByRole('status')).toHaveTextContent('正在载入消息')
  await act(async () => initialReject(new BridgeClientError('temporary', 'TEMPORARY', true, 'trace'))); expect(await screen.findByRole('alert')).toHaveTextContent('temporary')
  await user.click(screen.getByRole('button', { name: '从最新页重试' })); expect(await screen.findByText('message-2')).toBeInTheDocument(); await user.click(screen.getByRole('button', { name: '加载更早' })); expect(await screen.findByRole('alert')).toHaveTextContent('cursor invalid')
  await user.click(screen.getByRole('button', { name: '从最新页重试' })); expect(await screen.findByText('message-3')).toBeInTheDocument(); expect(screen.queryByText('message-2')).toBeNull(); expect(list.mock.calls[3][0]).not.toHaveProperty('cursor')
})

it('Message Renderer renders XSS payloads as inert text without element injection', async () => {
  const xss = '<img src=x onerror=alert(1)><script>alert(2)</script><svg onload=alert(3)>'
  await open(messages({ list: vi.fn().mockResolvedValue(page([message(1, { text: xss })])) }))
  expect(await screen.findByText(xss)).toBeInTheDocument(); expect(document.querySelector('.message-list img, .message-list script, .message-list svg')).toBeNull()
})

it('Message Renderer restores assistant messages with agent presentation', async () => {
  await open(messages({ list: vi.fn().mockResolvedValue(page([message(1), message(2, { role: 'assistant', text: 'durable assistant' })])) }))
  const assistant = await screen.findByText('durable assistant')
  expect(assistant.closest('li')).toHaveClass('agent')
  expect(assistant.closest('.bubble')).toHaveClass('message-body')
  expect(assistant.closest('li')).toHaveTextContent('AGENT')
})

it('Message Renderer distinguishes tool history and marks user round starts', async () => {
  await open(messages({ list: vi.fn().mockResolvedValue(page([message(1, { text: 'question' }), message(2, { role: 'tool', text: 'read result' })])) }))
  const user = await screen.findByText('question')
  expect(user.closest('li')).toHaveClass('round-start')
  const tool = screen.getByText('read result')
  expect(tool.closest('li')).toHaveClass('tool')
  expect(tool.closest('li')).toHaveTextContent('TOOL')
  expect(tool.closest('li')).not.toHaveTextContent('AGENT')
})

it('Message Renderer accepts exact flat Unicode boundaries and rejects rune/byte overflow and NUL', async () => {
  const append = vi.fn().mockResolvedValue(message(1)), user = await open(messages({ append }))
  await screen.findByText('还没有消息')
  const input = screen.getByLabelText('向月汐提问，或描述你想完成的任务…')
  for (const text of ['a'.repeat(2048), '😀'.repeat(2048)]) {
    await user.clear(input)
    await fireEvent.change(input, { target: { value: text } })
    expect(screen.getByText('2048/2048 字符 ·', { exact: false })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '仅保存' }))
    await waitFor(() => expect(append).toHaveBeenCalledWith({ sessionId: S1, text }, expect.anything()))
  }
  for (const text of ['a'.repeat(2049), '😀'.repeat(2048) + 'a', 'a\0b']) {
    await user.clear(input)
    await fireEvent.change(input, { target: { value: text } })
    await user.click(screen.getByRole('button', { name: '仅保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('消息需为')
  }
  expect(append).toHaveBeenCalledTimes(2)
})

it('linkifies HTTPS in stored chat messages without linkifying unsafe schemes',async()=>{
 await open(messages({list:vi.fn().mockResolvedValue(page([message(1,{role:'assistant',text:'百度 https://www.baidu.com/，不安全 javascript:alert(1) http://plain.test'})]))}))
 const link=await screen.findByRole('link',{name:'https://www.baidu.com/'})
 expect(link).toHaveAttribute('target','_blank')
 expect(screen.queryByRole('link',{name:/javascript|http:\/\/plain/})).toBeNull()
 expect(screen.queryByLabelText('统一工作区')).toBeNull()
})
