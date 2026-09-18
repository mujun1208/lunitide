import React from 'react'
import type { MediaSnapshotDTO } from '../generated/bridge'
import { formatClock, type MiniPlayerPhase } from './mediaSnapshot'
import { mediaText, playbackStatusText } from './mediaCopy'
import { useZh } from '../i18n/language'

export function MediaMiniPlayer({
  phase,
  snapshot,
  title,
  error,
  onOpen,
  onPlayPause,
  onClose,
  onRetryClose,
  hub,
}: {
  phase: MiniPlayerPhase
  snapshot: MediaSnapshotDTO | null
  title: string
  error: string
  onOpen: () => void
  onPlayPause: () => void
  onClose: () => void
  onRetryClose: () => void
  hub?: boolean
}): React.JSX.Element | null {
  const zh = useZh()
  const copy = mediaText(zh)
  if (phase === 'hidden' || !snapshot) return null
  const playing = snapshot.phase === 'playing'
  const closing = phase === 'closing'
  const status = closing ? copy.closing : playbackStatusText(zh, snapshot.phase, snapshot.verificationStatus)
  return (
    <aside className={`media-mini-player${hub ? ' is-hub' : ''}`} aria-label={copy.mini}>
      <button type="button" className="media-mini-main" onClick={onOpen}>
        <span className="media-mini-cover" aria-hidden="true" />
        <span>
          <b>{title}</b>
          <small>{status} · {formatClock(snapshot.positionMs)}</small>
        </span>
      </button>
      <button type="button" disabled={closing} onClick={onPlayPause}>{playing ? copy.pause : copy.play}</button>
      <button type="button" disabled={closing} onClick={onClose}>{closing ? copy.closing : copy.close}</button>
      {phase === 'close_error' ? (
        <p role="alert">
          {error || copy.closeError}
          <button type="button" onClick={onRetryClose}>{copy.retryClose}</button>
        </p>
      ) : null}
    </aside>
  )
}
