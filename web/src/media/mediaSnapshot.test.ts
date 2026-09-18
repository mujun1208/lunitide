import { expect, it } from 'vitest'
import type { MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import { formatClock, miniPlayerPhase } from './mediaSnapshot'

const snap = (phase: MediaSnapshotDTO['phase']): MediaSnapshotDTO => ({
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  scopeKind: 'user',
  scopeId: null,
  origin: 'owned',
  phase,
  verificationStatus: 'none',
  verificationSource: 'none',
  assetId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  playbackEpoch: 0,
  autoAdvance: true,
  positionMs: 0,
  durationMs: 0,
  volume: 100,
  muted: false,
  queueRevision: 1,
  revision: 1,
  updatedAt: '2026-01-01T00:00:00Z',
})

const op = (phase: MediaOperationDTO['phase']): MediaOperationDTO => ({
  operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  parentOperationId: null,
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  action: 'stop',
  phase,
  verificationStatus: 'pending',
  verificationSource: 'owned_runtime',
  errorCode: phase === 'failed' || phase === 'uncertain' ? 'MEDIA_UNVERIFIED' : null,
  revision: 1,
})

it('hides the mini player on Media Center and before any session exists', () => {
  expect(miniPlayerPhase('media', snap('playing'), null)).toBe('hidden')
  expect(miniPlayerPhase('home', null, null)).toBe('hidden')
  expect(miniPlayerPhase('home', snap('idle'), null)).toBe('hidden')
})

it('keeps pause visible off-page and does not hide while stop is unverified', () => {
  expect(miniPlayerPhase('home', snap('paused'), null)).toBe('active')
  expect(miniPlayerPhase('settings', snap('playing'), op('verifying'))).toBe('closing')
  expect(miniPlayerPhase('home', snap('playing'), op('uncertain'))).toBe('close_error')
  expect(miniPlayerPhase('home', snap('stopped'), op('succeeded'))).toBe('hidden')
})

it('formats clocks without claiming duration', () => {
  expect(formatClock(65000)).toBe('1:05')
})
