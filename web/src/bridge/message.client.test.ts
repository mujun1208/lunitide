import { expect, it, vi } from 'vitest'
import { createMessageBridge, createMutationAttempt, type WebViewTransport } from './client'
import type { MessageAppendPayload, MessageDTO } from '../generated/bridge'

const SESSION = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const MESSAGE = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const OTHER = '01ARZ3NDEKTSV4RRFFQ69G5FAB'
const NOW = '2025-01-01T00:00:00Z'
const dto = (overrides: Partial<MessageDTO> = {}): MessageDTO => ({ id: MESSAGE, sessionId: SESSION, role: 'user', status: 'completed', sequence: 1, text: 'hello', createdAt: NOW, ...overrides })
const page = (items: MessageDTO[] = [dto()], overrides: Record<string, unknown> = {}) => ({ items, hasMore: false, nextCursor: null, snapshotSequence: Math.max(0, ...items.map(x => x.sequence)), ...overrides })

function controlled() {
  let listener: (event: MessageEvent) => void = () => {}
  const sent: any[] = []
  const transport: WebViewTransport = {
    addEventListener: (_type, next) => { listener = next as (event: MessageEvent) => void },
    removeEventListener: vi.fn(),
    postMessage: value => { sent.push(value) }
  }
  const reply = (request: any, payload: unknown) => listener(new MessageEvent('message', { data: { v: '1.0', kind: 'response', id: MESSAGE, requestId: request.id, ok: true, payload } }))
  return { sent, reply, bridge: createMessageBridge(transport) }
}

it('message bridge sends exact typed list/append payloads, applies list defaults, and reuses append idempotency on retry', async () => {
  const h = controlled()
  const listed = h.bridge.list({ sessionId: SESSION })
  expect(h.sent[0]).toMatchObject({ method: 'message.list', payload: { sessionId: SESSION } })
  expect(Object.keys(h.sent[0].payload)).toEqual(['sessionId'])
  expect(h.sent[0]).not.toHaveProperty('idempotencyKey')
  h.reply(h.sent[0], page())
  await listed

  const payload: MessageAppendPayload = { sessionId: SESSION, text: 'retry me' }
  const attempt = createMutationAttempt('message.append', payload)
  for (let index = 0; index < 2; index++) {
    const appended = h.bridge.append(payload, { attempt })
    expect(h.sent[index + 1]).toMatchObject({ method: 'message.append', payload, idempotencyKey: attempt.idempotencyKey })
    h.reply(h.sent[index + 1], dto({ text: payload.text }))
    await appended
  }
  expect(h.sent[1].id).not.toBe(h.sent[2].id)
  expect(h.sent[1].traceId).not.toBe(h.sent[2].traceId)
  expect(h.sent[1].idempotencyKey).toBe(h.sent[2].idempotencyKey)
})

it.each([
  ['ULID', { id: 'bad' }],
  ['session parent', { sessionId: OTHER }],
  ['role', { role: 'system' }],
  ['status', { status: 'pending' }],
  ['zero sequence', { sequence: 0 }],
  ['unsafe sequence', { sequence: Number.MAX_SAFE_INTEGER + 1 }],
  ['empty text', { text: '' }],
  ['DTO text overflow', { text: 'a'.repeat(65537) }],
  ['invalid time', { createdAt: '2025-02-30T00:00:00Z' }],
  ['time without zone', { createdAt: '2025-01-01T00:00:00' }]
])('message bridge rejects malformed MessageDTO: %s', async (_name, mutation) => {
  const h = controlled(), promise = h.bridge.append({ sessionId: SESSION, text: 'hello' })
  h.reply(h.sent[0], { ...dto(), ...mutation })
  await expect(promise).rejects.toMatchObject({ code: 'INVALID_BRIDGE_RESULT' })
})

it.each(['assistant', 'tool'] as const)('message bridge accepts persisted %s history', async role => {
  const h = controlled(), promise = h.bridge.list({ sessionId: SESSION, direction: 'backward' })
  h.reply(h.sent[0], page([dto({ role })]))
  await expect(promise).resolves.toMatchObject({ items: [{ role }] })
})

it('message bridge accepts model output above the user input limit', async () => {
  const h = controlled(), text = 'a'.repeat(4096), promise = h.bridge.list({ sessionId: SESSION, direction: 'backward' })
  h.reply(h.sent[0], page([dto({ role: 'assistant', text })]))
  await expect(promise).resolves.toMatchObject({ items: [{ text }] })
})

it.each([
  ['items is not an array', { items: null }],
  ['too many items', { items: Array.from({ length: 257 }, (_, i) => dto({ id: i === 0 ? MESSAGE : `01ARZ3NDEKTSV4RRFFQ69G${String(i).padStart(3, '0')}` as any })) }],
  ['hasMore mismatch', { hasMore: true }],
  ['empty next cursor', { hasMore: true, nextCursor: '' }],
  ['oversized next cursor', { hasMore: true, nextCursor: 'x'.repeat(1025) }],
  ['negative snapshot', { snapshotSequence: -1 }],
  ['item beyond snapshot', { snapshotSequence: 0 }],
  ['non-contiguous backward sequence', { items: [dto({ sequence: 3 }), dto({ id: OTHER, sequence: 1 })], snapshotSequence: 3 }],
  ['extra page property', { extra: true }]
])('message bridge rejects malformed page: %s', async (_name, mutation) => {
  const h = controlled(), promise = h.bridge.list({ sessionId: SESSION, direction: 'backward' })
  h.reply(h.sent[0], { ...page(), ...mutation })
  await expect(promise).rejects.toMatchObject({ code: 'INVALID_BRIDGE_RESULT' })
})

it('message bridge rejects malformed request cursors and pagination before transport', async () => {
  const h = controlled()
  for (const payload of [
    { sessionId: SESSION, cursor: '' }, { sessionId: SESSION, cursor: 'x'.repeat(1025) },
    { sessionId: SESSION, limit: 0 }, { sessionId: SESSION, limit: 257 }, { sessionId: SESSION, limit: 1.5 },
    { sessionId: SESSION, byteBudget: 16383 }, { sessionId: SESSION, byteBudget: 245761 }, { sessionId: SESSION, byteBudget: 16384.5 }
  ]) await expect(h.bridge.list(payload as any)).rejects.toMatchObject({ code: 'INVALID_BRIDGE_REQUEST' })
  expect(h.sent).toHaveLength(0)
})

it('message bridge binds returned cursors to session, direction, and snapshot', async () => {
  const h = controlled(), first = h.bridge.list({ sessionId: SESSION, direction: 'backward' })
  h.reply(h.sent[0], page([dto({ sequence: 2 })], { hasMore: true, nextCursor: 'opaque', snapshotSequence: 2 }))
  await first
  const next = h.bridge.list({ sessionId: SESSION, direction: 'backward', cursor: 'opaque' })
  h.reply(h.sent[1], page([dto({ sequence: 1 })], { snapshotSequence: 3 }))
  await expect(next).rejects.toMatchObject({ code: 'INVALID_BRIDGE_RESULT' })
})

it('message text accepts exact 2048-rune and 8192-byte flat Unicode boundaries and rejects either overflow', async () => {
  const h = controlled()
  for (const text of ['a'.repeat(2048), '😀'.repeat(2048)]) {
    expect(Array.from(text)).toHaveLength(2048)
    if (text.startsWith('😀')) expect(new TextEncoder().encode(text)).toHaveLength(8192)
    const promise = h.bridge.append({ sessionId: SESSION, text })
    h.reply(h.sent.at(-1), dto({ text }))
    await expect(promise).resolves.toMatchObject({ text })
  }
  await expect(h.bridge.append({ sessionId: SESSION, text: 'a'.repeat(2049) })).rejects.toMatchObject({ code: 'INVALID_BRIDGE_REQUEST' })
  await expect(h.bridge.append({ sessionId: SESSION, text: '😀'.repeat(2048) + 'a' })).rejects.toMatchObject({ code: 'INVALID_BRIDGE_REQUEST' })
})
