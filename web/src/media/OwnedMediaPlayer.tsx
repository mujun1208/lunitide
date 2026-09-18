import React, { useEffect, useRef } from 'react'
import type { MediaAssetDTO, MediaSnapshotDTO } from '../generated/bridge'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

export type OwnedMediaEvent = 'playing' | 'pause' | 'ended' | 'stalled' | 'error' | 'position'

export function OwnedMediaPlayer({
  snapshot,
  src,
  kind,
  wantPlay,
  onEnded,
  onError,
  onObserved,
}: {
  snapshot: MediaSnapshotDTO | null
  src: string | null
  kind: MediaAssetDTO['kind'] | null
  wantPlay: boolean
  onEnded: () => void
  onError: (message: string) => void
  onObserved?: (event: OwnedMediaEvent, positionMs: number, durationMs: number) => void
}): React.JSX.Element | null {
  const zh = useZh()
  const copy = mediaText(zh)
  const nodeRef = useRef<HTMLAudioElement | HTMLVideoElement>(null)
  const lastSeek = useRef<number | null>(null)
  const lastPositionAt = useRef(0)
  const onObservedRef = useRef(onObserved)
  onObservedRef.current = onObserved
  const owned = snapshot?.origin === 'owned'
  const playing = owned && (wantPlay || snapshot?.phase === 'playing')
  const paused = owned && !playing
  const video = kind === 'video'

  useEffect(() => {
    const node = nodeRef.current
    if (!node || !snapshot) return
    const emit = (event: OwnedMediaEvent) => {
      const positionMs = Number.isFinite(node.currentTime) ? Math.max(0, Math.round(node.currentTime * 1000)) : 0
      const durationMs = Number.isFinite(node.duration) ? Math.max(0, Math.round(node.duration * 1000)) : 0
      onObservedRef.current?.(event, positionMs, durationMs)
    }
    const onPlaying = () => emit('playing')
    const onPause = () => emit('pause')
    const onStalled = () => emit('stalled')
    const onMediaError = () => emit('error')
    const onTimeUpdate = () => {
      const now = Date.now()
      if (now - lastPositionAt.current < 1000) return
      lastPositionAt.current = now
      emit('position')
    }
    node.addEventListener('playing', onPlaying)
    node.addEventListener('pause', onPause)
    node.addEventListener('stalled', onStalled)
    node.addEventListener('error', onMediaError)
    node.addEventListener('timeupdate', onTimeUpdate)
    return () => {
      node.removeEventListener('playing', onPlaying)
      node.removeEventListener('pause', onPause)
      node.removeEventListener('stalled', onStalled)
      node.removeEventListener('error', onMediaError)
      node.removeEventListener('timeupdate', onTimeUpdate)
    }
  }, [snapshot, src, video])

  useEffect(() => {
    const node = nodeRef.current
    if (!node || !snapshot) return
    if (!src) {
      node.removeAttribute('src')
      node.load()
      return
    }
    if (node.getAttribute('src') !== src) node.src = src
    node.volume = Math.min(1, Math.max(0, snapshot.volume / 100))
    node.muted = snapshot.muted
    if (lastSeek.current !== snapshot.positionMs) {
      const next = snapshot.positionMs / 1000
      if (Number.isFinite(next) && Math.abs(node.currentTime - next) > 0.4) {
        node.currentTime = next
      }
      lastSeek.current = snapshot.positionMs
    }
    if (playing) {
      try {
        void Promise.resolve(node.play()).catch(() => onError(copy.channelDown))
      } catch {
        // jsdom has no media engine; browsers return a Promise from play().
        // play() fulfillment is not evidence of playing.
      }
    } else if (paused || !owned) {
      node.pause()
    }
  }, [src, playing, paused, owned, onError, video, snapshot, copy.channelDown])

  if (!owned || !snapshot) return null
  return (
    <div className="owned-media-player" hidden>
      {video ? (
        <video ref={nodeRef as React.RefObject<HTMLVideoElement>} onEnded={onEnded} onError={() => onError(copy.channelDown)} />
      ) : (
        <audio ref={nodeRef as React.RefObject<HTMLAudioElement>} onEnded={onEnded} onError={() => onError(copy.channelDown)} />
      )}
    </div>
  )
}
