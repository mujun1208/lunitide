import React, { useEffect, useImperativeHandle, useRef } from 'react'
import type { MediaAssetDTO, MediaSnapshotDTO } from '../generated/bridge'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

export type OwnedMediaEvent = 'playing' | 'pause' | 'ended' | 'stalled' | 'error' | 'position'

export type OwnedMediaPlayerHandle = {
  playNow: (nextSrc?: string | null) => void
  pauseNow: () => void
  syncObserved: () => void
}

export function OwnedMediaPlayer({
  ref,
  snapshot,
  src,
  kind,
  wantPlay,
  onEnded,
  onError,
  onObserved,
}: {
  ref?: React.Ref<OwnedMediaPlayerHandle>
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
  const onErrorRef = useRef(onError)
  const srcRef = useRef(src)
  onObservedRef.current = onObserved
  onErrorRef.current = onError
  srcRef.current = src
  const owned = snapshot?.origin === 'owned'
  const playing = owned && wantPlay
  const paused = owned && !playing
  const video = kind === 'video'

  const attachSrc = (node: HTMLAudioElement | HTMLVideoElement, next: string) => {
    if (node.getAttribute('src') !== next) node.src = next
  }

  useImperativeHandle(ref, () => ({
    playNow(nextSrc) {
      const node = nodeRef.current
      const next = nextSrc || srcRef.current
      if (!node || !next) return
      attachSrc(node, next)
      void Promise.resolve(node.play()).catch(() => {})
    },
    pauseNow() {
      nodeRef.current?.pause()
    },
    syncObserved() {
      const node = nodeRef.current
      if (!node) return
      const positionMs = Number.isFinite(node.currentTime) ? Math.max(0, Math.round(node.currentTime * 1000)) : 0
      const durationMs = Number.isFinite(node.duration) ? Math.max(0, Math.round(node.duration * 1000)) : 0
      onObservedRef.current?.(node.paused ? 'pause' : 'playing', positionMs, durationMs)
    },
  }))

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
    attachSrc(node, src)
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
        void Promise.resolve(node.play()).catch(() => {})
      } catch {
        // jsdom has no media engine; browsers return a Promise from play().
        // play() fulfillment or rejection is not evidence of playing.
      }
    } else if (paused || !owned) {
      node.pause()
    }
  }, [src, playing, paused, owned, video, snapshot])

  if (!owned || !snapshot) return null
  return (
    <div className="owned-media-player">
      {video ? (
        <video ref={nodeRef as React.RefObject<HTMLVideoElement>} onEnded={onEnded} onError={() => onErrorRef.current(copy.channelDown)} />
      ) : (
        <audio ref={nodeRef as React.RefObject<HTMLAudioElement>} onEnded={onEnded} onError={() => onErrorRef.current(copy.channelDown)} />
      )}
    </div>
  )
}
