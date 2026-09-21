import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { BridgeClientError, createMutationAttempt, getActivityBridge, getMediaBridge, newBridgeULID, type ActivityBridge, type MediaBridge } from '../bridge/client'
import type { ActivitySnapshotDTO, MediaAssetDTO, MediaOperationDTO, MediaSessionCommandPayload, MediaSnapshotDTO } from '../generated/bridge'
import { useNavStore } from '../app/navStore'
import { MediaCenterPage } from './MediaCenterPage'
import { MediaMiniPlayer } from './MediaMiniPlayer'
import { OwnedMediaPlayer, type OwnedMediaPlayerHandle } from './OwnedMediaPlayer'
import { mediaTransportCommand, miniPlayerPhase, needsPlaybackOpen, shouldDetachPlayback } from './mediaSnapshot'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

type MediaState = {
  snapshot: MediaSnapshotDTO | null
  assets: MediaAssetDTO[]
  activities: ActivitySnapshotDTO[]
  operation: MediaOperationDTO | null
  stopOperation: MediaOperationDTO | null
  playbackUrl: string | null
  notice: string
  disabledReason: string
  busy: boolean
}

const EMPTY: MediaState = {
  snapshot: null,
  assets: [],
  activities: [],
  operation: null,
  stopOperation: null,
  playbackUrl: null,
  notice: '',
  disabledReason: '',
  busy: false,
}

type MediaStoreValue = MediaState & {
  wantPlay: boolean
  pick: () => Promise<void>
  playPause: () => Promise<void>
  previous: () => Promise<void>
  next: () => Promise<void>
  stop: () => Promise<void>
  jump: (assetId: string) => Promise<void>
  remove: (assetId: string) => Promise<void>
  clear: () => Promise<void>
  seek: (positionMs: number) => Promise<void>
  volume: (volume: number) => Promise<void>
}

const MediaStoreContext = createContext<MediaStoreValue | null>(null)

function failMessage(error: unknown, fallback: string, disabled: string): string {
  if (error instanceof BridgeClientError) {
    if (error.code === 'MEDIA_SESSION_V2_DISABLED') return disabled
    return error.message || fallback
  }
  return fallback
}

export function MediaRuntime({
  children,
  media = getMediaBridge(),
  activity = getActivityBridge(),
}: {
  children: React.ReactNode
  media?: MediaBridge
  activity?: ActivityBridge
}): React.JSX.Element {
  const page = useNavStore(s => s.page)
  const setPage = useNavStore(s => s.setPage)
  const setTarget = useNavStore(s => s.setTarget)
  const [state, setState] = useState<MediaState>(EMPTY)
  const [wantPlay, setWantPlay] = useState(false)
  const wantPlayRef = useRef(wantPlay)
  wantPlayRef.current = wantPlay
  const snapshotRef = useRef(state.snapshot)
  const openedAssetIdRef = useRef<string | null>(null)
  const openedEpochRef = useRef<number | null>(null)
  const openedExpiresAtRef = useRef<string | null>(null)
  const playbackUrlRef = useRef<string | null>(null)
  const playerRef = useRef<OwnedMediaPlayerHandle>(null)
  snapshotRef.current = state.snapshot
  playbackUrlRef.current = state.playbackUrl
  const zh = useZh()
  const copy = mediaText(zh)

  const dropPlayback = useCallback(() => {
    openedAssetIdRef.current = null
    openedEpochRef.current = null
    openedExpiresAtRef.current = null
    playbackUrlRef.current = null
    setState(prev => prev.playbackUrl ? { ...prev, playbackUrl: null } : prev)
  }, [])

  const openPlayback = useCallback(async (current: MediaSnapshotDTO) => {
    if (!current.assetId) return
    if (!needsPlaybackOpen(current, playbackUrlRef.current, openedAssetIdRef.current, openedEpochRef.current, openedExpiresAtRef.current)) return
    try {
      const opened = await media.openAsset({ assetId: current.assetId, mediaSessionId: current.mediaSessionId })
      openedAssetIdRef.current = current.assetId
      openedEpochRef.current = current.playbackEpoch
      openedExpiresAtRef.current = opened.expiresAt
      playbackUrlRef.current = opened.playbackUrl
      setState(prev => ({ ...prev, playbackUrl: opened.playbackUrl }))
    } catch {
      dropPlayback()
      setState(prev => ({ ...prev, notice: prev.notice || copy.channelDown }))
    }
  }, [copy.channelDown, dropPlayback, media])

  const refresh = useCallback(async () => {
    try {
      const [sessions, activities] = await Promise.all([
        media.list({ scopeKind: 'user' }).catch((error: unknown) => {
          if (error instanceof BridgeClientError && error.code === 'MEDIA_SESSION_V2_DISABLED') {
            return { items: [] as MediaSnapshotDTO[], nextCursor: null as string | null, disabled: true as const }
          }
          throw error
        }),
        activity.list({ scopeKind: 'user' }).catch(() => ({ items: [] as ActivitySnapshotDTO[], nextCursor: null as string | null, snapshotAt: new Date().toISOString(), hasMore: false })),
      ])
      const current = sessions.items[0] ?? null
      const listed = current
        ? await media.listAssets({ scopeKind: 'user', mediaSessionId: current.mediaSessionId }).catch(() => ({ items: [] as MediaAssetDTO[], nextCursor: null as string | null }))
        : { items: [] as MediaAssetDTO[], nextCursor: null as string | null }
      setState(prev => ({
        ...prev,
        snapshot: current,
        assets: listed.items,
        activities: activities.items,
        disabledReason: 'disabled' in sessions && sessions.disabled ? copy.disabled : '',
        notice: prev.notice,
      }))
      if (shouldDetachPlayback(current)) {
        dropPlayback()
        if (current?.phase === 'stopped') {
          wantPlayRef.current = false
          setWantPlay(false)
        }
      } else if (current) {
        await openPlayback(current)
      }
    } catch (error) {
      if (error instanceof BridgeClientError && error.message === '请求超时参数无效') {
        return
      }
      setState(prev => ({ ...prev, notice: failMessage(error, copy.refreshFailed, copy.disabled) }))
    }
  }, [activity, copy.disabled, copy.refreshFailed, dropPlayback, media, openPlayback])

  useEffect(() => {
    void refresh()
    let disposed = false
    let disposeWatch: (() => void) | undefined
    void Promise.resolve(media.watch({ scopeKind: 'user' }, () => { void refresh() })).then(handle => {
      if (!handle || typeof handle.dispose !== 'function') return
      if (disposed) {
        handle.dispose()
        return
      }
      disposeWatch = () => handle.dispose()
    }).catch(() => {})
    const timer = window.setInterval(() => { void refresh() }, 4000)
    return () => {
      disposed = true
      disposeWatch?.()
      window.clearInterval(timer)
    }
  }, [refresh, media])

  const runCommand = useCallback(async (action: MediaSessionCommandPayload['action'], extra: { positionMs?: number; volume?: number; asStop?: boolean } = {}) => {
    const snapshot = snapshotRef.current
    if (!snapshot) return
    setState(prev => ({ ...prev, busy: true, notice: '' }))
    const payload: MediaSessionCommandPayload = extra.positionMs != null
      ? { mediaSessionId: snapshot.mediaSessionId, action, positionMs: extra.positionMs, expectedRevision: snapshot.revision, operationId: newBridgeULID() }
      : extra.volume != null
        ? { mediaSessionId: snapshot.mediaSessionId, action, volume: extra.volume, expectedRevision: snapshot.revision, operationId: newBridgeULID() }
        : { mediaSessionId: snapshot.mediaSessionId, action, expectedRevision: snapshot.revision, operationId: newBridgeULID() }
    try {
      const result = await media.command(payload, { attempt: createMutationAttempt('media.session.command', payload) })
      if (action === 'play' || action === 'next' || action === 'previous' || (action === 'toggle' && snapshot.phase !== 'playing')) {
        wantPlayRef.current = true
        setWantPlay(true)
      }
      if (action === 'pause' || action === 'stop') {
        wantPlayRef.current = false
        setWantPlay(false)
      }
      setState(prev => ({
        ...prev,
        snapshot: result.snapshot,
        operation: result.operation,
        stopOperation: extra.asStop || action === 'stop' ? result.operation : prev.stopOperation,
        busy: false,
      }))
      if (action === 'next' || action === 'previous' || action === 'play') {
        await refresh()
      }
    } catch (error) {
      setState(prev => ({ ...prev, busy: false, notice: failMessage(error, copy.commandFailed, copy.disabled) }))
    }
  }, [copy.commandFailed, copy.disabled, media, refresh])

  const playPause = useCallback(async () => {
    const snapshot = snapshotRef.current
    if (!snapshot) return
    const action = mediaTransportCommand(snapshot)
    if (action === 'play') {
      wantPlayRef.current = true
      setWantPlay(true)
      await openPlayback(snapshot)
      playerRef.current?.playNow(playbackUrlRef.current)
    } else {
      wantPlayRef.current = false
      setWantPlay(false)
      playerRef.current?.pauseNow()
    }
    await runCommand(action)
  }, [openPlayback, runCommand])

  const pick = useCallback(async () => {
    setState(prev => ({ ...prev, busy: true, notice: '' }))
    try {
      const picked = await media.pick({ scopeKind: 'user', multiple: true })
      if (picked.canceled) {
        setState(prev => ({ ...prev, busy: false }))
        return
      }
      const first = picked.assets[0]
      if (!first) {
        setState(prev => ({ ...prev, busy: false, notice: copy.noFile }))
        return
      }
      const createPayload = {
        assetId: first.assetId,
        queueAssetIds: picked.assets.map(asset => asset.assetId),
        scopeKind: 'user' as const,
        operationId: newBridgeULID(),
      }
      try {
        const created = await media.create(createPayload, { attempt: createMutationAttempt('media.session.create', createPayload) })
        snapshotRef.current = created.snapshot
        setState(prev => ({ ...prev, snapshot: created.snapshot, operation: created.operation, assets: picked.assets, busy: false }))
        await openPlayback(created.snapshot)
        await refresh()
      } catch (error) {
        setState(prev => ({ ...prev, assets: picked.assets, busy: false, notice: failMessage(error, copy.createFailed, copy.disabled) }))
      }
    } catch (error) {
      setState(prev => ({ ...prev, busy: false, notice: failMessage(error, copy.pickFailed, copy.disabled) }))
    }
  }, [copy.createFailed, copy.disabled, copy.noFile, copy.pickFailed, media, openPlayback, refresh])

  const queue = useCallback(async (action: 'jump' | 'remove' | 'clear', itemId?: string) => {
    const snapshot = snapshotRef.current
    if (!snapshot) return
    setState(prev => ({ ...prev, busy: true, notice: '' }))
    const payload = action === 'clear'
      ? { mediaSessionId: snapshot.mediaSessionId, action, expectedQueueRevision: snapshot.queueRevision, operationId: newBridgeULID() }
      : { mediaSessionId: snapshot.mediaSessionId, action, itemId: itemId!, expectedQueueRevision: snapshot.queueRevision, operationId: newBridgeULID() }
    try {
      const result = await media.queueCommand(payload, { attempt: createMutationAttempt('media.queue.command', payload) })
      if (action === 'jump') {
        wantPlayRef.current = true
        setWantPlay(true)
      }
      setState(prev => ({ ...prev, snapshot: result.snapshot, operation: result.operation, busy: false }))
      await refresh()
    } catch (error) {
      setState(prev => ({ ...prev, busy: false, notice: failMessage(error, copy.queueFailed, copy.disabled) }))
    }
  }, [copy.disabled, copy.queueFailed, media, refresh])

  const value = useMemo<MediaStoreValue>(() => ({
    ...state,
    wantPlay,
    pick,
    playPause,
    previous: () => runCommand('previous'),
    next: () => runCommand('next'),
    stop: () => runCommand('stop', { asStop: true }),
    jump: assetId => queue('jump', assetId),
    remove: assetId => queue('remove', assetId),
    clear: () => queue('clear'),
    seek: positionMs => runCommand('seek', { positionMs }),
    volume: volume => runCommand('set_volume', { volume }),
  }), [pick, playPause, queue, runCommand, state, wantPlay])

  const phase = miniPlayerPhase(page, state.snapshot, state.stopOperation)
  const current = state.assets.find(item => item.assetId === state.snapshot?.assetId)
  const title = current?.title || copy.untitled
  return (
    <MediaStoreContext.Provider value={value}>
      {children}
      <MediaMiniPlayer
        phase={phase}
        snapshot={state.snapshot}
        title={title}
        error={state.notice}
        hub={page === 'agentHub'}
        onOpen={() => { setTarget(undefined); setPage('media') }}
        onPlayPause={() => { void value.playPause() }}
        onClose={() => { void value.stop() }}
        onRetryClose={() => { void value.stop() }}
      />
      <OwnedMediaPlayer
        ref={playerRef}
        snapshot={state.snapshot}
        src={state.playbackUrl}
        kind={current?.kind ?? null}
        wantPlay={wantPlay}
        onEnded={() => { void runCommand('next') }}
        onError={message => {
          openedAssetIdRef.current = null
          openedEpochRef.current = null
          openedExpiresAtRef.current = null
          playbackUrlRef.current = null
          setState(prev => ({ ...prev, notice: message, playbackUrl: null }))
        }}
        onObserved={(event, positionMs, durationMs) => {
          const snapshot = snapshotRef.current
          if (!snapshot || snapshot.origin !== 'owned') return
          void media.reportElement?.({ mediaSessionId: snapshot.mediaSessionId, event, positionMs, durationMs }).catch(() => {})
        }}
      />
    </MediaStoreContext.Provider>
  )
}

export function useMediaStore(): MediaStoreValue {
  const value = useContext(MediaStoreContext)
  if (!value) return {
    ...EMPTY,
    wantPlay: false,
    pick: async () => {},
    playPause: async () => {},
    previous: async () => {},
    next: async () => {},
    stop: async () => {},
    jump: async () => {},
    remove: async () => {},
    clear: async () => {},
    seek: async () => {},
    volume: async () => {},
  }
  return value
}

export function MediaCenterRoute(): React.JSX.Element {
  const media = useMediaStore()
  return (
    <MediaCenterPage
      snapshot={media.snapshot}
      assets={media.assets}
      operation={media.operation}
      playbackUrl={media.playbackUrl}
      notice={media.notice}
      disabledReason={media.disabledReason}
      busy={media.busy}
      onPick={() => { void media.pick() }}
      onPlayPause={() => { void media.playPause() }}
      onPrevious={() => { void media.previous() }}
      onNext={() => { void media.next() }}
      onJump={assetId => { void media.jump(assetId) }}
      onRemove={assetId => { void media.remove(assetId) }}
      onClear={() => { void media.clear() }}
      onSeek={positionMs => { void media.seek(positionMs) }}
      onVolume={volume => { void media.volume(volume) }}
    />
  )
}
