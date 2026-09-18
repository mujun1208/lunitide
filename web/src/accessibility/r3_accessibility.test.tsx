import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import { MediaCenterPage } from '../media/MediaCenterPage'
import { ActivityStatusButton } from '../activity/ActivityStatusButton'

afterEach(() => cleanup())

const snapshot: MediaSnapshotDTO = {
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  scopeKind: 'user',
  scopeId: null,
  origin: 'owned',
  phase: 'paused',
  verificationStatus: 'command_dispatched',
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

const operation: MediaOperationDTO = {
  operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  mediaSessionId: snapshot.mediaSessionId,
  parentOperationId: null,
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  action: 'play',
  phase: 'uncertain',
  verificationStatus: 'unconfirmed',
  verificationSource: 'none',
  errorCode: 'MEDIA_UNVERIFIED',
  revision: 1,
}

it('TestR3StateAccessibility: failed and unverified states keep text, status, and labels', () => {
  render(
    <>
      <MediaCenterPage snapshot={snapshot} assets={[]} operation={operation} playbackUrl={null} notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />
      <ActivityStatusButton items={[]} open={false} onToggle={() => {}} />
    </>,
  )
  expect(screen.getByText('命令已发送，待核验')).toBeInTheDocument()
  expect(screen.getByLabelText('媒体操作')).toHaveTextContent('未确认')
  expect(screen.getByLabelText('媒体操作')).not.toHaveTextContent('已确认')
  expect(screen.getByRole('button', { name: '运行状态' })).toBeInTheDocument()
  expect(screen.getByText('MEDIA_UNVERIFIED')).toBeInTheDocument()
})
