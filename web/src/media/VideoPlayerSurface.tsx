import React from 'react'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { formatClock } from './mediaSnapshot'
import { mediaText, playbackStatusText } from './mediaCopy'
import { MediaTransportControls } from './MediaTransportControls'
import { useZh } from '../i18n/language'

export function VideoPlayerSurface({
  snapshot,
  title,
  src,
  busy,
  onPlayPause,
  onPrevious,
  onNext,
  onQueue,
  onSeek,
  onVolume,
}: {
  snapshot: MediaSnapshotDTO
  title: string
  src: string | null
  busy: boolean
  onPlayPause: () => void
  onPrevious: () => void
  onNext: () => void
  onQueue: () => void
  onSeek: (positionMs: number) => void
  onVolume: (volume: number) => void
}): React.JSX.Element {
  const zh = useZh()
  const copy = mediaText(zh)
  const playing = snapshot.phase === 'playing'
  return (
    <section className="media-video-surface" aria-label={copy.video}>
      <div className="media-video-stage">
        {src ? <div className="media-video-empty">{copy.videoStage}</div> : <div className="media-video-empty">{copy.videoEmpty}</div>}
      </div>
      <h2 title={title}>{title}</h2>
      <p role="status">{playbackStatusText(zh, snapshot.phase, snapshot.verificationStatus)}</p>
      <p className="media-clock">{formatClock(snapshot.positionMs)} / {formatClock(snapshot.durationMs)}</p>
      <MediaTransportControls
        zh={zh}
        busy={busy}
        playing={playing}
        positionMs={snapshot.positionMs}
        durationMs={snapshot.durationMs}
        volume={snapshot.volume}
        onPlayPause={onPlayPause}
        onPrevious={onPrevious}
        onNext={onNext}
        onQueue={onQueue}
        onSeek={onSeek}
        onVolume={onVolume}
        allowSeek={snapshot.origin === 'owned'}
        allowVolume={snapshot.origin === 'owned'}
      />
    </section>
  )
}
