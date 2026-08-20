import { describe, expect, it } from 'vitest'
import {
  IMPLEMENTATION_PHASES,
  OPERATIONS_PHASES,
  inferActivePhase,
  phaseStepClass,
  phasesForProjectType,
} from './projectPhases'
import type { StageDTO } from '../generated/bridge'

const stage = (phase: number, status: StageDTO['status']): StageDTO => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  projectId: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  phase,
  title: 't',
  status,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  version: 1,
})

describe('phasesForProjectType', () => {
  it('projects eight phases for implementation and enhancement', () => {
    expect(phasesForProjectType('implementation')).toHaveLength(8)
    expect(phasesForProjectType('enhancement')).toEqual(IMPLEMENTATION_PHASES)
    expect(phasesForProjectType('implementation')[0].label).toBe('需求架构规范')
    expect(phasesForProjectType('implementation')[7].label).toBe('发布')
  })

  it('projects six phases for operations', () => {
    expect(phasesForProjectType('operations')).toEqual(OPERATIONS_PHASES)
    expect(phasesForProjectType('operations')).toHaveLength(6)
  })
})

describe('inferActivePhase', () => {
  it('honors preferred phase when valid', () => {
    const map = new Map([
      [3, stage(3, 'in_progress')],
      [1, stage(1, 'completed')],
    ])
    expect(inferActivePhase(IMPLEMENTATION_PHASES, map, 5)).toBe(5)
    expect(inferActivePhase(IMPLEMENTATION_PHASES, map)).toBe(3)
  })

  it('falls back to first incomplete then last phase', () => {
    const done = new Map([[1, stage(1, 'completed')]])
    expect(inferActivePhase(IMPLEMENTATION_PHASES, done)).toBe(2)
    const allDone = new Map(IMPLEMENTATION_PHASES.map(p => [p.phase, stage(p.phase, 'approved')]))
    expect(inferActivePhase(IMPLEMENTATION_PHASES, allDone)).toBe(8)
  })
})

describe('phaseStepClass', () => {
  it('maps stage status to pipeline classes', () => {
    expect(phaseStepClass('completed')).toBe('done')
    expect(phaseStepClass('in_progress')).toBe('run')
    expect(phaseStepClass('not_started')).toBe('')
  })
})
