import { describe, expect, it, vi } from 'vitest'
import type { SessionBridge } from '../bridge/client'
import type { SessionDTO } from '../generated/bridge'
import {
  ensureProjectPhaseSession,
  findPhaseSession,
  legacyProjectSessions,
  parsePhaseSessionTitle,
  phaseSessionTitle,
} from './projectPhaseSession'

const NOW = '2025-01-01T00:00:00Z'
const PROJECT = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

const session = (overrides: Partial<SessionDTO> = {}): SessionDTO => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  projectId: PROJECT,
  title: '你好',
  pinned: false,
  status: 'active',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
  ...overrides,
})

describe('phaseSessionTitle', () => {
  it('encodes and parses phase sessions', () => {
    expect(phaseSessionTitle(7, '集成')).toBe('phase:7:集成')
    expect(parsePhaseSessionTitle('phase:7:集成')).toBe(7)
    expect(parsePhaseSessionTitle('在线电商')).toBeUndefined()
  })
})

describe('ensureProjectPhaseSession', () => {
  it('reuses an existing phase session', async () => {
    const phase7 = session({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
      title: phaseSessionTitle(7, '集成'),
    })
    const sessions = {
      list: vi.fn().mockResolvedValue({ items: [phase7] }),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    } as unknown as SessionBridge

    const resolved = await ensureProjectPhaseSession(sessions, PROJECT, 7, '集成')
    expect(resolved).toBe(phase7)
    expect(sessions.create).not.toHaveBeenCalled()
  })

  it('migrates a legacy session into phase 1', async () => {
    const legacy = session({ title: '在线电商' })
    const renamed = session({ title: phaseSessionTitle(1, '需求架构规范'), version: 2 })
    const sessions = {
      list: vi.fn().mockResolvedValue({ items: [legacy] }),
      create: vi.fn(),
      update: vi.fn().mockResolvedValue(renamed),
      delete: vi.fn(),
    } as unknown as SessionBridge

    const resolved = await ensureProjectPhaseSession(sessions, PROJECT, 1, '需求架构规范', {
      initialSession: legacy,
    })
    expect(resolved).toEqual(renamed)
    expect(sessions.update).toHaveBeenCalledOnce()
    expect(sessions.create).not.toHaveBeenCalled()
  })

  it('creates a new session for phases without history', async () => {
    const phase1 = session({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', title: phaseSessionTitle(1, '需求架构规范') })
    const created = session({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAC',
      title: phaseSessionTitle(7, '集成'),
    })
    const sessions = {
      list: vi.fn().mockResolvedValue({ items: [phase1] }),
      create: vi.fn().mockResolvedValue(created),
      update: vi.fn(),
      delete: vi.fn(),
    } as unknown as SessionBridge

    const resolved = await ensureProjectPhaseSession(sessions, PROJECT, 7, '集成')
    expect(resolved).toBe(created)
    expect(sessions.create).toHaveBeenCalledOnce()
  })
})

describe('legacyProjectSessions', () => {
  it('returns only sessions without phase titles', () => {
    const items = [
      session({ title: '在线电商' }),
      session({ id: '02', title: phaseSessionTitle(2, '方案和UI设计') }),
    ]
    expect(legacyProjectSessions(items)).toHaveLength(1)
    expect(findPhaseSession(items, 2)?.id).toBe('02')
  })
})
