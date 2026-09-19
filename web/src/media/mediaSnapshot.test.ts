import { expect, it } from 'vitest'
import type { MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import { formatClock, miniPlayerPhase, needsPlaybackOpen } from './mediaSnapshot'

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

it('does not pin a floating bar on Chat, Settings, or Hub; media center is the only control surface', () => {
  expect(miniPlayerPhase('home', snap('paused'), null)).toBe('hidden')
  expect(miniPlayerPhase('home', snap('playing'), null)).toBe('hidden')
  expect(miniPlayerPhase('settings', snap('playing'), op('verifying'))).toBe('hidden')
  expect(miniPlayerPhase('home', snap('playing'), op('uncertain'))).toBe('hidden')
  expect(miniPlayerPhase('agentHub', snap('playing'), null)).toBe('hidden')
  expect(miniPlayerPhase('home', snap('stopped'), op('succeeded'))).toBe('hidden')
})

it('formats clocks without claiming duration', () => {
  expect(formatClock(65000)).toBe('1:05')
})

it('does not mint a new playback ticket while the same asset is already open', () => {
  const playing = snap('playing')
  expect(needsPlaybackOpen(playing, 'https://media.lunitide.local/v1/assets/tok', playing.assetId, true)).toBe(false)
  expect(needsPlaybackOpen(playing, null, null, true)).toBe(true)
  expect(needsPlaybackOpen({ ...playing, assetId: '01ARZ3NDEKTSV4RRFFQ69G5FAZ' }, 'https://media.lunitide.local/v1/assets/tok', playing.assetId, true)).toBe(true)
  expect(needsPlaybackOpen({ ...playing, playbackEpoch: 2 }, 'https://media.lunitide.local/v1/assets/tok', playing.assetId, true, 1)).toBe(true)
  expect(needsPlaybackOpen({ ...playing, playbackEpoch: 2 }, 'https://media.lunitide.local/v1/assets/tok', playing.assetId, true, 2)).toBe(false)
  expect(needsPlaybackOpen(snap('idle'), null, null, true)).toBe(false)
})

it('does not open local media on launch until the user plays', () => {
  expect(needsPlaybackOpen(snap('playing'), null, null, false)).toBe(false)
  expect(needsPlaybackOpen(snap('paused'), null, null, false)).toBe(false)
})
