import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { OwnedMediaPlayer } from './OwnedMediaPlayer'

afterEach(() => cleanup())

const snapshot: MediaSnapshotDTO = {
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  scopeKind: 'user',
  scopeId: null,
  origin: 'owned',
  phase: 'playing',
  verificationStatus: 'command_dispatched',
  verificationSource: 'owned_runtime',
  assetId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  playbackEpoch: 1,
  autoAdvance: true,
  positionMs: 0,
  durationMs: 0,
  volume: 100,
  muted: false,
  queueRevision: 1,
  revision: 1,
  updatedAt: '2026-01-01T00:00:00Z',
}

it('owns exactly one media element for the current asset kind', () => {
  const { rerender } = render(<OwnedMediaPlayer snapshot={snapshot} src="https://media.lunitide.local/v1/assets/t" kind="audio" wantPlay onEnded={() => {}} onError={() => {}} />)
  expect(document.querySelectorAll('audio')).toHaveLength(1)
  expect(document.querySelectorAll('video')).toHaveLength(0)
  rerender(<OwnedMediaPlayer snapshot={snapshot} src="https://media.lunitide.local/v1/assets/t" kind="video" wantPlay onEnded={() => {}} onError={() => {}} />)
  expect(document.querySelectorAll('audio')).toHaveLength(0)
  expect(document.querySelectorAll('video')).toHaveLength(1)
})

it('reports native playing and never treats play() as verified', () => {
  const onObserved = vi.fn()
  render(<OwnedMediaPlayer snapshot={snapshot} src="https://media.lunitide.local/v1/assets/t" kind="audio" wantPlay onEnded={() => {}} onError={() => {}} onObserved={onObserved} />)
  expect(onObserved).not.toHaveBeenCalled()
  document.querySelector('audio')?.dispatchEvent(new Event('playing'))
  expect(onObserved).toHaveBeenCalledWith('playing', expect.any(Number), expect.any(Number))
})
