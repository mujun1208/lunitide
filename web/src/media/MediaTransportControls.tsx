import React, { useState } from 'react'
import { mediaText } from './mediaCopy'

export function levelFromY(clientY: number, node: HTMLElement): number {
  const rect = node.getBoundingClientRect()
  if (!Number.isFinite(clientY) || rect.height <= 0) return 0
  const ratio = 1 - (clientY - rect.top) / rect.height
  return Math.min(100, Math.max(0, Math.round(ratio * 100)))
}

export function MediaTransportControls({
  zh,
  busy,
  playing,
  positionMs,
  durationMs,
  volume,
  allowSeek = true,
  allowVolume = true,
  direct = false,
  fullscreen = false,
  onPlayPause,
  onPrevious,
  onNext,
  onQueue,
  onSeek,
  onVolume,
  onFullscreen,
  onClose,
  onHold,
}: {
  zh: boolean
  busy: boolean
  playing: boolean
  positionMs: number
  durationMs: number
  volume: number
  allowSeek?: boolean
  allowVolume?: boolean
  direct?: boolean
  fullscreen?: boolean
  onPlayPause: () => void
  onPrevious: () => void
  onNext: () => void
  onQueue: () => void
  onSeek: (positionMs: number) => void
  onVolume: (volume: number) => void
  onFullscreen?: () => void
  onClose?: () => void
  onHold?: (held: boolean) => void
}): React.JSX.Element {
  const copy = mediaText(zh)
  const [volumeOpen, setVolumeOpen] = useState(false)
  const toggleVolume = () => {
    setVolumeOpen(open => {
      onHold?.(!open)
      return !open
    })
  }
  const applyLevel = (event: React.PointerEvent<HTMLElement>) => {
    onVolume(levelFromY(event.clientY, event.currentTarget))
  }
  return (
    <div className={direct ? 'media-controls is-direct' : 'media-controls'}>
      {direct ? null : <button type="button" disabled={busy} onClick={onPrevious}>{copy.previous}</button>}
      <button type="button" className="media-play" disabled={busy} onClick={onPlayPause}>{playing ? copy.pause : copy.play}</button>
      {direct ? null : <button type="button" disabled={busy} onClick={onNext}>{copy.next}</button>}
      {direct ? null : <button type="button" disabled={busy} onClick={onQueue}>{copy.queue}</button>}
      {allowSeek ? (
        <label className="media-slider">
          {copy.seek}
          <input
            type="range"
            aria-label={copy.seek}
            min={0}
            max={Math.max(durationMs, positionMs, 1)}
            value={positionMs}
            disabled={busy}
            onChange={event => onSeek(Number(event.currentTarget.value))}
          />
        </label>
      ) : null}
      {allowVolume ? (
        <div className="media-volume-wrap">
          <button type="button" aria-expanded={volumeOpen} aria-label={copy.volume} onClick={toggleVolume}>{copy.volume}</button>
          {volumeOpen ? (
            <div
              className="media-volume-pop"
              role="slider"
              aria-label={copy.volume}
              aria-orientation="vertical"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={volume}
              tabIndex={0}
              onPointerDown={event => {
                event.currentTarget.setPointerCapture?.(event.pointerId)
                applyLevel(event)
              }}
              onPointerMove={event => {
                const captured = event.currentTarget.hasPointerCapture?.(event.pointerId)
                if (!captured && event.buttons !== 1) return
                applyLevel(event)
              }}
              onKeyDown={event => {
                if (event.key === 'ArrowUp' || event.key === 'ArrowRight') onVolume(Math.min(100, volume + 5))
                if (event.key === 'ArrowDown' || event.key === 'ArrowLeft') onVolume(Math.max(0, volume - 5))
              }}
            >
              <i style={{ height: `${volume}%` }} />
            </div>
          ) : null}
        </div>
      ) : null}
      {onFullscreen ? <button type="button" onClick={onFullscreen}>{fullscreen ? copy.restore : copy.full}</button> : null}
      {onClose ? <button type="button" onClick={onClose}>{copy.close}</button> : null}
    </div>
  )
}
