import React, { useEffect, useRef } from 'react'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { formatClock, mediaTransportPlaying } from './mediaSnapshot'
import { mediaText, playbackStatusText } from './mediaCopy'
import { MediaTransportControls } from './MediaTransportControls'
import { useZh } from '../i18n/language'

export function VideoPlayerSurface({
  snapshot,
  title,
  src,
  showFile,
  rate,
  busy,
  idle,
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
  showFile?: boolean
  rate?: number
  busy: boolean
  idle?: boolean
  onPlayPause: () => void
  onPrevious: () => void
  onNext: () => void
  onQueue: () => void
  onSeek: (positionMs: number) => void
  onVolume: (volume: number) => void
}): React.JSX.Element {
  const zh = useZh()
  const copy = mediaText(zh)
  const playing = mediaTransportPlaying(false, snapshot)
  const videoRef = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    if (videoRef.current && rate) videoRef.current.playbackRate = rate
  }, [rate, src])
  return (
    <section className="media-video-surface" aria-label={copy.video}>
      <div className="video-heading">
        <h2 title={title}>{title}</h2>
        <p role="status">{playbackStatusText(zh, snapshot.phase, snapshot.verificationStatus)}</p>
      </div>
      <div className={showFile && src ? 'video-theatre is-live' : 'video-theatre'}>
        {showFile && src ? <video ref={videoRef} className="media-theatre-video" src={src} autoPlay controls playsInline /> : null}
        <button type="button" className="video-play" disabled={busy} onClick={onPlayPause} aria-hidden="true" tabIndex={-1}>
          {playing ? '❚❚' : '▶'}
        </button>
        <div className="video-controls" aria-hidden="true">
          <span>{playing ? '❚❚' : '▶'}</span>
          <i />
          <span className="small">{formatClock(snapshot.positionMs)} / {formatClock(snapshot.durationMs)}</span>
        </div>
        {showFile && src ? null : <p className="media-video-empty">{src ? copy.videoStage : (idle ? copy.idleHint : copy.videoEmpty)}</p>}
      </div>
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
        allowSeek={!idle && snapshot.origin === 'owned'}
        allowVolume={!idle && snapshot.origin === 'owned'}
      />
    </section>
  )
}
