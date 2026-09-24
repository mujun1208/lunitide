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
  direct = false,
  fullscreen = false,
  onPlayPause,
  onPrevious,
  onNext,
  onQueue,
  onSeek,
  onVolume,
  onFullscreen,
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
}): React.JSX.Element {
  const copy = mediaText(zh)
  const seekDrag = useRef(false)
  const volumeDrag = useRef(false)
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
        <label className="media-volume">
          <input
            type="range"
            aria-label={copy.volume}
            aria-orientation="vertical"
            min={0}
            max={100}
            value={volume}
            disabled={busy}
            onChange={event => onVolume(Number(event.currentTarget.value))}
            onPointerDown={() => { volumeDrag.current = true }}
            onPointerUp={event => {
              if (!volumeDrag.current) return
              volumeDrag.current = false
              onVolume(Number(event.currentTarget.value))
            }}
            onKeyUp={event => {
              if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown' && event.key !== 'ArrowLeft' && event.key !== 'ArrowRight' && event.key !== 'Home' && event.key !== 'End') return
              onVolume(Number(event.currentTarget.value))
            }}
          />
          <span>{copy.volume}</span>
        </label>
      ) : null}
      {onFullscreen ? <button type="button" onClick={onFullscreen}>{fullscreen ? copy.restore : copy.full}</button> : null}
    </div>
  )
}
