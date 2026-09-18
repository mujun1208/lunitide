import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { VideoPlayerSurface } from './VideoPlayerSurface'

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
  positionMs: 1500,
  durationMs: 4000,
  volume: 40,
  muted: false,
  queueRevision: 1,
  revision: 2,
  updatedAt: '2026-01-01T00:00:00Z',
}

it('TestVideoCoreControls: exposes play, seek and volume without a second video element', () => {
  const onPlayPause = vi.fn()
  const onSeek = vi.fn()
  const onVolume = vi.fn()
  render(
    <VideoPlayerSurface
      snapshot={snapshot}
      title="记录片"
      src="https://media.lunitide.local/v1/assets/token"
      busy={false}
      onPlayPause={onPlayPause}
      onPrevious={() => {}}
      onNext={() => {}}
      onQueue={() => {}}
      onSeek={onSeek}
      onVolume={onVolume}
    />,
  )
  expect(document.querySelector('video')).toBeNull()
  expect(screen.getByRole('button', { name: '播放' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '上一首' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '下一首' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '队列' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('进度'), { target: { value: '2000' } })
  fireEvent.change(screen.getByLabelText('音量'), { target: { value: '10' } })
  expect(onSeek).toHaveBeenCalledWith(2000)
  expect(onVolume).toHaveBeenCalledWith(10)
  fireEvent.click(screen.getByRole('button', { name: '播放' }))
  expect(onPlayPause).toHaveBeenCalledTimes(1)
})
