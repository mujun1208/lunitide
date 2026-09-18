import React from 'react'
import type { MediaAssetDTO, MediaSnapshotDTO, MediaOperationDTO } from '../generated/bridge'
import { MediaOperationCard } from './MediaOperationCard'
import { MediaQueueDrawer } from './MediaQueueDrawer'
import { MusicPlayerSurface } from './MusicPlayerSurface'
import { VideoPlayerSurface } from './VideoPlayerSurface'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

export function MediaCenterPage({
  snapshot,
  assets,
  operation,
  playbackUrl,
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
  const title = current?.title || copy.untitled
  const surface = view ?? (current?.kind === 'video' ? 'video' : 'music')
  return (
    <div className="media-center">
      <header className="media-center-top">
        <h1>{copy.title}</h1>
        <p>{copy.intro}</p>
        <nav className="media-center-tabs" aria-label={copy.views}>
          <button type="button" className={surface === 'music' ? 'is-current' : ''} onClick={() => setView('music')}>{copy.music}</button>
          <button type="button" className={surface === 'video' ? 'is-current' : ''} onClick={() => setView('video')}>{copy.video}</button>
        </nav>
        <button type="button" className="media-pick" disabled={busy} onClick={onPick}>{copy.pick}</button>
      </header>
      {notice ? <p role="alert">{notice}</p> : null}
      {disabledReason ? <p role="status">{disabledReason}</p> : null}
      {!snapshot ? (
        <section className="media-empty" aria-label={copy.emptyLabel}>
          <p>{copy.empty}</p>
        </section>
      ) : surface === 'video' ? (
        <VideoPlayerSurface snapshot={snapshot} title={title} src={playbackUrl} busy={busy} onPlayPause={onPlayPause} onPrevious={onPrevious} onNext={onNext} onQueue={() => setQueueOpen(true)} onSeek={onSeek} onVolume={onVolume} />
      ) : (
        <MusicPlayerSurface snapshot={snapshot} title={title} busy={busy} onPlayPause={onPlayPause} onPrevious={onPrevious} onNext={onNext} onQueue={() => setQueueOpen(true)} onSeek={onSeek} onVolume={onVolume} />
      )}
      {operation ? <MediaOperationCard operation={operation} /> : null}
      <MediaQueueDrawer open={queueOpen} snapshot={snapshot} assets={assets} onClose={() => setQueueOpen(false)} onJump={onJump} onRemove={onRemove} onClear={onClear} />
    </div>
  )
}
