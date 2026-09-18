import { afterEach, expect, it, vi } from 'vitest'
import type { MemoryOpsBridge } from '../bridge/client'
import { loadMemorySettings, saveMemorySettings } from './memorySettings'

afterEach(() => vi.restoreAllMocks())

const version = 'a'.repeat(64)

function ops(overrides: Partial<MemoryOpsBridge> = {}): MemoryOpsBridge {
  return {
    getSettings: vi.fn().mockResolvedValue({
      subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14,
      updatedAt: '2026-01-01T00:00:00Z', version, captureMode: 'auto', revision: 2,
      personalMemoryEnabled: true, projectMemoryEnabled: false,
    }),
    updateSettings: vi.fn().mockResolvedValue({
      subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14,
      updatedAt: '2026-01-01T00:00:00Z', version, captureMode: 'manual', revision: 3,
      personalMemoryEnabled: true, projectMemoryEnabled: false,
    }),
    stats: vi.fn(), listFacts: vi.fn(), flagFact: vi.fn(), listTraces: vi.fn(),
    listGrowth: vi.fn(), decideGrowth: vi.fn(), export: vi.fn(), purge: vi.fn(),
    ...overrides,
  } as MemoryOpsBridge
}

it('loads R3 settings with an empty get payload', async () => {
  const bridge = ops()
  const draft = await loadMemorySettings(bridge)
  expect(bridge.getSettings).toHaveBeenCalledWith({})
  expect(draft).toEqual({
    captureMode: 'auto',
    personalMemoryEnabled: true,
    projectMemoryEnabled: false,
    revision: 2,
  })
})

it('saves only the four R3 fields plus expectedRevision', async () => {
  const bridge = ops()
  const saved = await saveMemorySettings(bridge, {
    captureMode: 'manual', personalMemoryEnabled: true, projectMemoryEnabled: false, revision: 2,
  })
  expect(bridge.updateSettings).toHaveBeenCalledWith(
    {
      captureMode: 'manual',
      personalMemoryEnabled: true,
      projectMemoryEnabled: false,
      expectedRevision: 2,
    },
    expect.objectContaining({ attempt: expect.objectContaining({ method: 'memory.settings.update' }) }),
  )
  expect(saved.captureMode).toBe('manual')
  expect(saved.revision).toBe(3)
  expect(saved.projectMemoryEnabled).toBe(false)
})
