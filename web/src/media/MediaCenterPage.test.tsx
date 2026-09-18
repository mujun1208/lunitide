import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { MediaAssetDTO, MediaSnapshotDTO } from '../generated/bridge'
import { MediaCenterPage } from './MediaCenterPage'

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

it('TestNoSessionEmptyState: shows an empty state before any session exists', () => {
  render(<MediaCenterPage snapshot={null} assets={[]} operation={null} playbackUrl={null} notice="" disabledReason="媒体会话未启用。当前只能选择文件，还不能创建播放会话。" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByRole('heading', { name: '媒体中心' })).toBeInTheDocument()
  expect(screen.getByLabelText('空状态')).toHaveTextContent('还没有播放会话')
  expect(screen.getByRole('status')).toHaveTextContent('媒体会话未启用')
  expect(screen.queryByLabelText('音乐')).toBeNull()
})

it('TestOwnedExternalCapabilities: hides seek and volume for external origin', () => {
  render(<MediaCenterPage snapshot={{ ...snapshot, origin: 'external' }} assets={[asset('audio', '夜曲')]} operation={null} playbackUrl={null} notice="" disabledReason="" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />)
  expect(screen.getByLabelText('音乐')).toBeInTheDocument()
  expect(screen.queryByLabelText('进度')).toBeNull()
  expect(screen.queryByLabelText('音量')).toBeNull()
  expect(screen.getByRole('button', { name: '播放' })).toBeInTheDocument()
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
