import React from 'react'
import { mediaText } from './mediaCopy'

export function MediaTransportControls({
  zh,
  busy,
  playing,
  positionMs,
  durationMs,
  volume,
  allowSeek = true,
  allowVolume = true,
  onPlayPause,
  onPrevious,
  onNext,
  onQueue,
  onSeek,
  onVolume,
}: {
  zh: boolean
  busy: boolean
  playing: boolean
  positionMs: number
  durationMs: number
  volume: number
  allowSeek?: boolean
  allowVolume?: boolean
  onPlayPause: () => void
  onPrevious: () => void
  onNext: () => void
  onQueue: () => void
  onSeek: (positionMs: number) => void
  onVolume: (volume: number) => void
}): React.JSX.Element {
  const copy = mediaText(zh)
  return (
    <div className="media-controls">
      <button type="button" disabled={busy} onClick={onPrevious}>{copy.previous}</button>
      <button type="button" className="media-play" disabled={busy} onClick={onPlayPause}>{playing ? copy.pause : copy.play}</button>
      <button type="button" disabled={busy} onClick={onNext}>{copy.next}</button>
      <button type="button" disabled={busy} onClick={onQueue}>{copy.queue}</button>
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
            onChange={event => onSeek(Number(event.target.value))}
          />
        </label>
      ) : null}
      {allowVolume ? (
        <label className="media-slider">
          {copy.volume}
          <input
            type="range"
            aria-label={copy.volume}
            min={0}
            max={100}
            value={volume}
            disabled={busy}
            onChange={event => onVolume(Number(event.target.value))}
          />
        </label>
      ) : null}
    </div>
  )
}
