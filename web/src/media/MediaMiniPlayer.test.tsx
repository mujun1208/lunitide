import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { MediaMiniPlayer } from './MediaMiniPlayer'

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
  positionMs: 90000,
  durationMs: 180000,
  volume: 100,
  muted: false,
  queueRevision: 1,
  revision: 3,
  updatedAt: '2026-01-01T00:00:00Z',
}

it('stays hidden when the overlay is not requested', () => {
  const { container } = render(<MediaMiniPlayer phase="hidden" snapshot={snapshot} title="夜曲" error="" onOpen={() => {}} onPlayPause={() => {}} onClose={() => {}} onRetryClose={() => {}} />)
  expect(container).toBeEmptyDOMElement()
})

it('TestMediaMiniPlayerContract: keeps pause visible and does not hide while close is unverified', () => {
  render(<MediaMiniPlayer phase="active" snapshot={{ ...snapshot, phase: 'paused' }} title="夜曲" error="" onOpen={() => {}} onPlayPause={() => {}} onClose={() => {}} onRetryClose={() => {}} />)
  expect(screen.getByLabelText('迷你播放器')).toHaveTextContent('已暂停')
  expect(screen.getByRole('button', { name: '播放' })).toBeInTheDocument()
  cleanup()
  render(<MediaMiniPlayer phase="close_error" snapshot={snapshot} title="夜曲" error="停止未确认" hub onOpen={() => {}} onPlayPause={() => {}} onClose={() => {}} onRetryClose={() => {}} />)
  expect(screen.getByRole('alert')).toHaveTextContent('停止未确认')
  expect(screen.getByRole('button', { name: '重试结束' })).toBeInTheDocument()
  expect(screen.getByLabelText('迷你播放器').className).toMatch(/is-hub/)
})

it('TestMediaMiniPlayer: still renders chrome if a caller asks for active', () => {
  render(<MediaMiniPlayer phase="active" snapshot={snapshot} title="夜曲" error="" onOpen={() => {}} onPlayPause={() => {}} onClose={() => {}} onRetryClose={() => {}} />)
  expect(screen.getByLabelText('迷你播放器')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '暂停' })).toBeInTheDocument()
})
