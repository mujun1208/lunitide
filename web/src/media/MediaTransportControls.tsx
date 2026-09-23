import React, { useRef } from 'react'
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
  const seekDrag = useRef(false)
  const volumeDrag = useRef(false)
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
            onPointerDown={() => { seekDrag.current = true }}
            onPointerUp={event => {
              if (!seekDrag.current) return
              seekDrag.current = false
              onSeek(Number(event.currentTarget.value))
            }}
            onKeyUp={event => {
              if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight' && event.key !== 'Home' && event.key !== 'End') return
              onSeek(Number(event.currentTarget.value))
            }}
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
            onPointerDown={() => { volumeDrag.current = true }}
            onPointerUp={event => {
              if (!volumeDrag.current) return
              volumeDrag.current = false
              onVolume(Number(event.currentTarget.value))
            }}
            onKeyUp={event => {
              if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight' && event.key !== 'Home' && event.key !== 'End') return
              onVolume(Number(event.currentTarget.value))
            }}
          />
        </label>
      ) : null}
    </div>
  )
}
