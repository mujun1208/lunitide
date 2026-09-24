import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { MediaAssetDTO, MediaSnapshotDTO } from '../generated/bridge'
import { MediaCenterPage } from './MediaCenterPage'

beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

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

const asset = (kind: MediaAssetDTO['kind'], title: string): MediaAssetDTO => ({
  assetId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  sourceKind: 'user_selected',
  kind,
  title,
  mime: kind === 'video' ? 'video/mp4' : 'audio/mpeg',
  size: 12,
  state: 'ready',
  revision: 1,
})

it('TestNoSessionEmptyState: shows the player chrome before any session exists', () => {
  render(<MediaCenterPage snapshot={null} assets={[]} operation={null} playbackUrl={null} notice="" disabledReason="媒体会话未启用。当前只能选择文件，还不能创建播放会话。" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByRole('heading', { name: '媒体中心' })).toBeInTheDocument()
  expect(screen.getByLabelText('空状态')).toHaveTextContent('选择本地文件后开始播放')
  expect(screen.getByRole('status')).toHaveTextContent('媒体会话未启用')
  expect(screen.getByLabelText('音乐')).toBeInTheDocument()
  expect(document.querySelector('.album-art')).not.toBeNull()
  expect(document.querySelector('video')).toBeNull()
})

it('TestOwnedExternalCapabilities: hides seek and volume for external origin', () => {
  render(<MediaCenterPage snapshot={{ ...snapshot, origin: 'external' }} assets={[asset('audio', '夜曲')]} operation={null} playbackUrl={null} notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByLabelText('音乐')).toBeInTheDocument()
  expect(screen.queryByLabelText('进度')).toBeNull()
  expect(screen.queryByLabelText('音量')).toBeNull()
  expect(screen.getByRole('button', { name: '播放' })).toBeInTheDocument()
})

it('keeps the Play button after pick so a second click retries play, not pause', () => {
  render(<MediaCenterPage snapshot={{ ...snapshot, phase: 'idle', verificationStatus: 'command_dispatched', durationMs: 0, positionMs: 0 }} assets={[asset('audio', 'ringing_shortest.mp3')]} operation={null} playbackUrl={null} notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByRole('heading', { name: 'ringing_shortest.mp3' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '播放' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '暂停' })).toBeNull()
})

it('splits music and video surfaces without a second media element', () => {
  const { rerender } = render(<MediaCenterPage snapshot={snapshot} assets={[asset('audio', '夜曲')]} operation={null} playbackUrl={null} notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByLabelText('音乐')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '夜曲' })).toBeInTheDocument()
  rerender(<MediaCenterPage snapshot={snapshot} assets={[asset('video', '记录片')]} operation={null} playbackUrl="https://media.lunitide.local/v1/assets/token" notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByLabelText('视频')).toBeInTheDocument()
  expect(document.querySelector('video')).toBeNull()
  expect(screen.getByText('画面由本机唯一播放器输出')).toBeInTheDocument()
})

it('shows a public-domain file inside the video theatre', () => {
  render(<MediaCenterPage snapshot={null} assets={[]} operation={null} playbackUrl={null} stageSrc="https://archive.org/download/night/Night.mp4" stageTitle="Night of the Living Dead" stageKind="video" notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByLabelText('视频')).toBeInTheDocument()
  expect(document.querySelector('video')).toHaveAttribute('src', 'https://archive.org/download/night/Night.mp4')
  expect(screen.getByRole('heading', { name: 'Night of the Living Dead' })).toBeInTheDocument()
})
