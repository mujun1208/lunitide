import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { MediaAssetDTO, MediaSnapshotDTO } from '../generated/bridge'
import { MediaQueueDrawer } from './MediaQueueDrawer'

afterEach(() => cleanup())

const snapshot: MediaSnapshotDTO = {
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  scopeKind: 'user',
  scopeId: null,
  origin: 'owned',
  phase: 'paused',
  verificationStatus: 'verified_paused',
  verificationSource: 'owned_runtime',
  assetId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  playbackEpoch: 1,
  autoAdvance: true,
  positionMs: 1000,
  durationMs: 4000,
  volume: 80,
  muted: false,
  queueRevision: 1,
  revision: 2,
  updatedAt: '2026-01-01T00:00:00Z',
}

const asset = (id: string, title: string): MediaAssetDTO => ({
  assetId: id,
  sourceKind: 'user_selected',
  kind: 'audio',
  title,
  mime: 'audio/mpeg',
  size: 12,
  state: 'ready',
  revision: 1,
})

it('shows only queued assets, not leftover library titles', () => {
  render(
    <MediaQueueDrawer
      open
      snapshot={snapshot}
      assets={[asset('01ARZ3NDEKTSV4RRFFQ69G5FAW', '夜曲'), asset('01ARZ3NDEKTSV4RRFFQ69G5FAX', '练习曲')]}
      onClose={() => {}}
      onJump={() => {}}
      onRemove={() => {}}
      onClear={() => {}}
    />,
  )
  expect(screen.getByText('夜曲')).toBeInTheDocument()
  expect(screen.getByText('练习曲')).toBeInTheDocument()
  expect(screen.queryByText('Library leftover')).toBeNull()
})
