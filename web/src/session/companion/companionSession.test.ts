import { beforeEach, describe, expect, test, vi } from 'vitest'
import type { SessionDTO } from '../../generated/bridge'
import { COMPANION_SESSION_ID_KEY, ensureCompanionSession, pickCompanionSession } from './companionSession'

function session(over: Partial<SessionDTO>): SessionDTO {
  return {
    id: 'S',
    projectId: 'P',
    title: '月伴对话',
    pinned: false,
    status: 'active',
    createdAt: '2026-01-01T00:00:00.000Z',
    updatedAt: '2026-01-01T00:00:00.000Z',
    version: 1,
    ...over,
  }
}

describe('pickCompanionSession', () => {
  test('prefers the remembered id when it still exists', () => {
    const list = [session({ id: 'a', createdAt: '2026-01-01T00:00:00Z' }), session({ id: 'b', createdAt: '2026-02-01T00:00:00Z' })]
    expect(pickCompanionSession('b', list)?.id).toBe('b')
  })

  test('falls back to the oldest 月伴 session when the id is stale', () => {
    const list = [
      session({ id: 'newer', createdAt: '2026-03-01T00:00:00Z' }),
      session({ id: 'oldest', createdAt: '2026-01-01T00:00:00Z' }),
      session({ id: 'plain', title: '写周报', createdAt: '2025-01-01T00:00:00Z' }),
    ]
    expect(pickCompanionSession('gone', list)?.id).toBe('oldest')
  })

  test('recognizes the English companion title', () => {
    const list = [session({ id: 'en', title: 'Companion talk' })]
    expect(pickCompanionSession('', list)?.id).toBe('en')
  })

  test('returns undefined when there is no companion session', () => {
    expect(pickCompanionSession('', [session({ id: 'x', title: '普通对话' })])).toBeUndefined()
  })
})

describe('ensureCompanionSession', () => {
  beforeEach(() => localStorage.clear())

  test('reuses the stored singleton and pins it if needed', async () => {
    localStorage.setItem(COMPANION_SESSION_ID_KEY, 'keep')
    const update = vi.fn(async (p: { id: string; version: number }) => session({ id: p.id, pinned: true, version: p.version + 1 }))
    const sessions = {
      list: vi.fn(async () => ({ items: [session({ id: 'keep', pinned: false })] })),
      create: vi.fn(),
      update,
    } as never
    const result = await ensureCompanionSession(sessions, 'P', true)
    expect(result.id).toBe('keep')
    expect(result.pinned).toBe(true)
    expect((sessions as { create: ReturnType<typeof vi.fn> }).create).not.toHaveBeenCalled()
    expect(update).toHaveBeenCalledOnce()
  })

  test('does not re-pin an already-pinned singleton', async () => {
    localStorage.setItem(COMPANION_SESSION_ID_KEY, 'keep')
    const update = vi.fn()
    const sessions = {
      list: vi.fn(async () => ({ items: [session({ id: 'keep', pinned: true })] })),
      create: vi.fn(),
      update,
    } as never
    const result = await ensureCompanionSession(sessions, 'P', true)
    expect(result.id).toBe('keep')
    expect(update).not.toHaveBeenCalled()
  })

  test('creates and remembers a pinned singleton when none exists', async () => {
    const created = session({ id: 'new', pinned: false })
    const sessions = {
      list: vi.fn(async () => ({ items: [] })),
      create: vi.fn(async () => created),
      update: vi.fn(async (p: { id: string; version: number }) => session({ id: p.id, pinned: true, version: p.version + 1 })),
    } as never
    const result = await ensureCompanionSession(sessions, 'P', true)
    expect(result.id).toBe('new')
    expect(result.pinned).toBe(true)
    expect(localStorage.getItem(COMPANION_SESSION_ID_KEY)).toBe('new')
    expect((sessions as { create: ReturnType<typeof vi.fn> }).create).toHaveBeenCalledOnce()
  })
})
