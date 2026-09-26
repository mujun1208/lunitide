import React, { useState } from 'react'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { formatClock, mediaTransportPlaying } from './mediaSnapshot'
import { mediaText, playbackStatusText } from './mediaCopy'
import { MediaTransportControls } from './MediaTransportControls'
import { useDirectMedia, useStageChrome } from './useDirectMedia'
import { isHlsSource } from './mediaCenterPlay'
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
  onClose,
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
  onClose?: () => void
}): React.JSX.Element {
  const zh = useZh()
  const copy = mediaText(zh)
  const live = Boolean(showFile && src)
  const stage = useDirectMedia(live, src, rate)
  const [hold, setHold] = useState(false)
  const playing = live ? stage.playing : mediaTransportPlaying(false, snapshot)
  const chrome = useStageChrome(live && playing, hold)
  const close = onClose ? () => {
    if (stage.full) void document.exitFullscreen?.()
    stage.ref.current?.pause()
    onClose()
  } : undefined
  const status = live
    ? (!stage.heard ? copy.soundOff : stage.playing ? copy.playing : copy.paused)
    : playbackStatusText(zh, snapshot.phase, snapshot.verificationStatus)
  return (
    <section className="media-video-surface" aria-label={copy.video}>
      <div ref={stage.boxRef} className={live ? `video-theatre is-live${stage.full ? ' is-fullscreen' : ''}${chrome.shown ? '' : ' is-chrome-hidden'}` : 'video-theatre'} onPointerMove={live ? chrome.poke : undefined}>
        {/* m3u8 由 useDirectMedia 交给 hls.js 接管，src 属性必须留空避免原生加载冲突 */}
        {live ? <video ref={stage.ref as React.RefObject<HTMLVideoElement>} className="media-theatre-video" src={isHlsSource(src ?? '') ? undefined : (src ?? undefined)} autoPlay playsInline /> : null}
        {live ? null : (
          <button type="button" className="video-play" disabled={busy} onClick={onPlayPause} aria-hidden="true" tabIndex={-1}>
            {playing ? '❚❚' : '▶'}
          </button>
        )}
        {live ? (
          <div className="media-stage-chrome">
            <div className="video-heading">
              <h2 title={title}>{title}</h2>
              <p role="status">{status}</p>
            </div>
            <MediaTransportControls
              zh={zh}
              busy={busy}
              playing={playing}
              positionMs={stage.positionMs}
              durationMs={stage.durationMs}
              volume={stage.volume}
              direct
              fullscreen={stage.full}
              onPlayPause={stage.toggle}
              onPrevious={onPrevious}
              onNext={onNext}
              onQueue={onQueue}
              onSeek={stage.seek}
              onVolume={stage.setLevel}
              onFullscreen={stage.toggleFull}
              onClose={close}
              onHold={setHold}
            />
          </div>
        ) : (
          <div className="video-controls" aria-hidden="true">
            <span>{playing ? '❚❚' : '▶'}</span>
            <i />
            <span className="small">{formatClock(snapshot.positionMs)} / {formatClock(snapshot.durationMs)}</span>
          </div>
        )}
        {live ? null : <p className="media-video-empty">{src ? copy.videoStage : (idle ? copy.idleHint : copy.videoEmpty)}</p>}
      </div>
      {live ? null : (
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
          onClose={onClose}
          allowSeek={!idle && snapshot.origin === 'owned'}
          allowVolume={!idle && snapshot.origin === 'owned'}
        />
      )}
    </section>
  )
}
