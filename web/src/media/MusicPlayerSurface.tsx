import React from 'react'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { formatClock, mediaTransportPlaying } from './mediaSnapshot'
import { mediaText, playbackStatusText } from './mediaCopy'
import { MediaTransportControls } from './MediaTransportControls'
import { useDirectMedia } from './useDirectMedia'
import { useZh } from '../i18n/language'

export function MusicPlayerSurface({
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
  src?: string | null
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
  const live = Boolean(showFile && src)
  const stage = useDirectMedia(live, src, rate)
  const playing = live ? stage.playing : mediaTransportPlaying(false, snapshot)
  const status = live
    ? (!stage.heard ? copy.soundOff : stage.playing ? copy.playing : copy.paused)
    : playbackStatusText(zh, snapshot.phase, snapshot.verificationStatus)
  return (
    <section className="media-music-surface" aria-label={copy.music}>
      <div ref={stage.boxRef} className={live ? `media-stage is-live${stage.full ? ' is-fullscreen' : ''}` : 'media-stage'}>
        <div className="media-content">
          <div className="album-art" aria-hidden="true" />
          <div className="media-copy">
            <div className="media-kicker">{status}</div>
            <h2 title={title}>{title}</h2>
            {live ? <audio ref={stage.ref as React.RefObject<HTMLAudioElement>} className="media-stage-audio" src={src ?? undefined} autoPlay /> : null}
            {live ? null : <p>{idle ? copy.idleHint : copy.intro}</p>}
            <MediaTransportControls
              zh={zh}
              busy={busy}
              playing={playing}
              positionMs={live ? stage.positionMs : snapshot.positionMs}
              durationMs={live ? stage.durationMs : snapshot.durationMs}
              volume={live ? stage.volume : snapshot.volume}
              direct={live}
              fullscreen={stage.full}
              onPlayPause={live ? stage.toggle : onPlayPause}
              onPrevious={onPrevious}
              onNext={onNext}
              onQueue={onQueue}
              onSeek={live ? stage.seek : onSeek}
              onVolume={live ? stage.setLevel : onVolume}
              onFullscreen={live ? stage.toggleFull : undefined}
              allowSeek={live || (!idle && snapshot.origin === 'owned')}
              allowVolume={live || (!idle && snapshot.origin === 'owned')}
            />
            <p className="media-clock">{formatClock(live ? stage.positionMs : snapshot.positionMs)} / {formatClock(live ? stage.durationMs : snapshot.durationMs)}</p>
          </div>
        </div>
      </div>
    </section>
  )
}
