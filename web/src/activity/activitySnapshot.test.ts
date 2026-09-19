import { expect, it } from 'vitest'
import type { ActivitySnapshotDTO } from '../generated/bridge'
import { activityLampTone, activityLampToneWithDiagnostics, activityLampWorst, activityLampWorstWithDiagnostics, activityNeedsAttention, activityQuiet, diagnosticLampTone } from './activitySnapshot'

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

it('quarantined MCP turns tool and overall yellow without inventing a failed activity', () => {
  expect(diagnosticLampTone({ id: 'mcp', state: 'degraded', code: 'MCP_QUARANTINED' })).toBe('warn')
  expect(diagnosticLampTone({ id: 'memory', state: 'degraded', code: 'AUDIT_CHAIN_BROKEN' })).toBe('error')
  expect(diagnosticLampTone({ id: 'gui', state: 'not_configured' })).toBe('ready')
  expect(activityLampToneWithDiagnostics([], 'tool', [{ id: 'mcp', state: 'degraded', code: 'MCP_QUARANTINED' }])).toBe('warn')
  expect(activityLampWorstWithDiagnostics([], [{ id: 'mcp', state: 'degraded', code: 'MCP_QUARANTINED' }])).toBe('warn')
})

it('lamps stay green while work is running and unused domains stay green', () => {
  expect(activityLampTone([item('running')])).toBe('ready')
  expect(activityLampTone([], 'media')).toBe('ready')
  expect(activityLampTone([item('uncertain')])).toBe('warn')
  expect(activityLampTone([item('failed')])).toBe('error')
  expect(activityLampWorst([
    { ...item('succeeded'), domain: 'tool' },
    { ...item('uncertain'), activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', domain: 'ocr' },
    { ...item('failed'), activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', domain: 'media' },
  ])).toBe('error')
})
