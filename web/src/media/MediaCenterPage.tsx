import React from 'react'
import type { MediaAssetDTO, MediaSnapshotDTO, MediaOperationDTO } from '../generated/bridge'
import { MediaOperationCard } from './MediaOperationCard'
import { MediaQueueDrawer } from './MediaQueueDrawer'
import { MusicPlayerSurface } from './MusicPlayerSurface'
import { VideoPlayerSurface } from './VideoPlayerSurface'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

const IDLE_SNAPSHOT: MediaSnapshotDTO = {
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FA0',
  scopeKind: 'user',
  scopeId: null,
  origin: 'owned',
  phase: 'idle',
  verificationStatus: 'none',
  verificationSource: 'owned_runtime',
  assetId: null,
  playbackEpoch: 0,
  autoAdvance: true,
  positionMs: 0,
  durationMs: 0,
  volume: 80,
  muted: false,
  queueRevision: 0,
  revision: 0,
  updatedAt: '1970-01-01T00:00:00Z',
}

export function MediaCenterPage({
  snapshot,
  assets,
  operation,
  playbackUrl,
  stageSrc,
  stageTitle,
  stageKind,
  stageRate,
  notice,
  disabledReason,
  busy,
  onPick,
  onPlayPause,
  onPrevious,
  onNext,
  onJump,
  onRemove,
  onClear,
  onSeek,
  onVolume,
}: {
  snapshot: MediaSnapshotDTO | null
  assets: MediaAssetDTO[]
  operation: MediaOperationDTO | null
  playbackUrl: string | null
  stageSrc?: string | null
  stageTitle?: string
  stageKind?: 'audio' | 'video'
  stageRate?: number
  notice: string
  disabledReason: string
  busy: boolean
  onPick: () => void
  onPlayPause: () => void
  onPrevious: () => void
  onNext: () => void
  onJump: (assetId: string) => void
  onRemove: (assetId: string) => void
  onClear: () => void
  onSeek: (positionMs: number) => void
  onVolume: (volume: number) => void
}): React.JSX.Element {
  const zh = useZh()
  const copy = mediaText(zh)
  const [view, setView] = React.useState<'music' | 'video'>()
  const [queueOpen, setQueueOpen] = React.useState(false)
  const current = assets.find(item => item.assetId === snapshot?.assetId)
  const staged = Boolean(stageSrc)
  const idle = !snapshot && !staged
  const live = snapshot ?? IDLE_SNAPSHOT
  const title = staged ? (stageTitle || copy.untitled) : (current?.title || copy.untitled)
  const surface = view ?? (staged ? (stageKind === 'audio' ? 'music' : 'video') : (current?.kind === 'video' ? 'video' : 'music'))
  const play = idle ? onPick : onPlayPause
  return (
    <div className="media-center">
      <div className="media-center-stage">
        {surface === 'video' ? (
          <VideoPlayerSurface snapshot={live} title={title} src={stageSrc ?? playbackUrl} showFile={staged && stageKind !== 'audio'} rate={stageRate} busy={busy} idle={idle} onPlayPause={play} onPrevious={onPrevious} onNext={onNext} onQueue={() => setQueueOpen(true)} onSeek={onSeek} onVolume={onVolume} />
        ) : (
          <MusicPlayerSurface snapshot={live} title={title} src={stageSrc} showFile={staged && stageKind === 'audio'} rate={stageRate} busy={busy} idle={idle} onPlayPause={play} onPrevious={onPrevious} onNext={onNext} onQueue={() => setQueueOpen(true)} onSeek={onSeek} onVolume={onVolume} />
        )}
        {notice ? <p className="media-notice" role="alert">{notice}</p> : null}
        {disabledReason ? <p className="media-notice" role="status">{disabledReason}</p> : null}
        {idle ? <p className="media-empty-hint" aria-label={copy.emptyLabel}>{copy.empty}</p> : null}
      </div>
      <header className="media-center-top">
        <h1>{copy.title}</h1>
        <nav className="media-center-tabs" aria-label={copy.views}>
          <button type="button" className={surface === 'music' ? 'is-current' : ''} onClick={() => setView('music')}>{copy.music}</button>
          <button type="button" className={surface === 'video' ? 'is-current' : ''} onClick={() => setView('video')}>{copy.video}</button>
        </nav>
        <button type="button" className="media-pick" disabled={busy} onClick={onPick}>{copy.pick}</button>
      </header>
      {operation ? <MediaOperationCard operation={operation} /> : null}
      <MediaQueueDrawer open={queueOpen} snapshot={snapshot} assets={assets} onClose={() => setQueueOpen(false)} onJump={onJump} onRemove={onRemove} onClear={onClear} />
    </div>
  )
}
