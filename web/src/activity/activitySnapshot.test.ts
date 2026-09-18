import { expect, it } from 'vitest'
import type { ActivitySnapshotDTO } from '../generated/bridge'
import { activityNeedsAttention, activityQuiet } from './activitySnapshot'

const item = (phase: ActivitySnapshotDTO['phase']): ActivitySnapshotDTO => ({
  activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  domain: 'ocr',
  kind: 'pack',
  phase,
  terminal: phase === 'succeeded' || phase === 'uncertain' || phase === 'failed' || phase === 'cancelled',
  verificationStatus: phase === 'succeeded' ? 'confirmed' : 'unconfirmed',
  verificationSource: 'none',
  title: 'ocr',
  completedUnits: null,
  totalUnits: null,
  errorCode: null,
  retryable: false,
  recoveryAction: 'none',
  scopeKind: 'user',
  scopeId: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
})

it('keeps ordinary success quiet and treats uncertain as needing attention', () => {
  expect(activityQuiet([item('succeeded')])).toBe(true)
  expect(activityNeedsAttention(item('uncertain'))).toBe(true)
  expect(activityQuiet([item('succeeded'), item('uncertain')])).toBe(false)
})
