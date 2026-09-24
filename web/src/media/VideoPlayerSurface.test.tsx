import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

it('plays a handed-off file in the theatre', async () => {
  const play = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(HTMLMediaElement.prototype, 'play', { configurable: true, value: play })
  render(
    <VideoPlayerSurface
      snapshot={snapshot}
      title="Night of the Living Dead"
      src="https://archive.org/download/night_of_the_living_dead/Night.mp4"
      showFile
      busy={false}
      onPlayPause={() => {}}
      onPrevious={() => {}}
      onNext={() => {}}
      onQueue={() => {}}
      onSeek={() => {}}
      onVolume={() => {}}
    />,
  )
  const video = document.querySelector('video')
  expect(video).not.toBeNull()
  expect(video?.getAttribute('src')).toBe('https://archive.org/download/night_of_the_living_dead/Night.mp4')
  expect(play).toHaveBeenCalled()
  expect(video).not.toHaveAttribute('controls')
  await waitFor(() => expect(screen.getByRole('button', { name: '暂停' })).toBeInTheDocument())
  expect(screen.getByRole('status')).toHaveTextContent('正在播放')
  expect(video?.muted).toBe(false)
})

it('pauses the film on screen and sets its volume from the vertical slider', async () => {
  const play = vi.fn().mockRejectedValueOnce(new Error('autoplay')).mockResolvedValue(undefined)
  const pause = vi.fn()
  Object.defineProperty(HTMLMediaElement.prototype, 'play', { configurable: true, value: play })
  Object.defineProperty(HTMLMediaElement.prototype, 'pause', { configurable: true, value: pause })
  const onPlayPause = vi.fn()
  const onVolume = vi.fn()
  render(
    <VideoPlayerSurface
      snapshot={snapshot}
      title="Nosferatu (1922)"
      src="https://upload.wikimedia.org/wikipedia/commons/nosferatu.webm"
      showFile
      busy={false}
      onPlayPause={onPlayPause}
      onPrevious={() => {}}
      onNext={() => {}}
      onQueue={() => {}}
      onSeek={() => {}}
      onVolume={onVolume}
    />,
  )
  const video = document.querySelector('video') as HTMLVideoElement
  await waitFor(() => expect(video.muted).toBe(true))
  expect(screen.getByRole('status')).toHaveTextContent('向上拖音量即可出声')
  const volume = screen.getByLabelText('音量')
  expect(volume).toHaveAttribute('aria-orientation', 'vertical')
  fireEvent.change(volume, { target: { value: '70' } })
  expect(video.muted).toBe(false)
  expect(video.volume).toBeCloseTo(0.7)
  expect(onVolume).not.toHaveBeenCalled()
  expect(screen.queryByRole('button', { name: '上一首' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '全屏' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '暂停' })).toBeInTheDocument())
  fireEvent.click(screen.getByRole('button', { name: '暂停' }))
  expect(pause).toHaveBeenCalled()
  expect(onPlayPause).not.toHaveBeenCalled()
})

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
  const seek = screen.getByLabelText('进度')
  fireEvent.pointerDown(seek)
  ;(seek as HTMLInputElement).value = '2000'
  fireEvent.pointerUp(seek)
  const volume = screen.getByLabelText('音量')
  fireEvent.pointerDown(volume)
  ;(volume as HTMLInputElement).value = '10'
  fireEvent.pointerUp(volume)
  expect(onSeek).toHaveBeenCalledWith(2000)
  expect(onVolume).toHaveBeenCalledWith(10)
  fireEvent.click(screen.getByRole('button', { name: '播放' }))
  expect(onPlayPause).toHaveBeenCalledTimes(1)
})
