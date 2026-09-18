import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { BridgeClientError, createMutationAttempt, getActivityBridge, getMediaBridge, newBridgeULID, type ActivityBridge, type MediaBridge } from '../bridge/client'
import type { ActivitySnapshotDTO, MediaAssetDTO, MediaOperationDTO, MediaSessionCommandPayload, MediaSnapshotDTO } from '../generated/bridge'
import { useNavStore } from '../app/navStore'
import { OCR_SETTINGS_TARGET } from '../settings/ocrActivityAdapter'
import { ActivityCenter } from '../activity/ActivityCenter'
import { ActivityStatusButton } from '../activity/ActivityStatusButton'
import { MediaCenterPage } from './MediaCenterPage'
import { MediaMiniPlayer } from './MediaMiniPlayer'
import { OwnedMediaPlayer } from './OwnedMediaPlayer'
import { miniPlayerPhase } from './mediaSnapshot'
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
  const setSettingsCategory = useNavStore(s => s.setSettingsCategory)
  const setSettingsIntelligenceView = useNavStore(s => s.setSettingsIntelligenceView)
  const setTarget = useNavStore(s => s.setTarget)
  const [state, setState] = useState<MediaState>(EMPTY)
  const [activityOpen, setActivityOpen] = useState(false)
  const [wantPlay, setWantPlay] = useState(false)
  const snapshotRef = useRef(state.snapshot)
  snapshotRef.current = state.snapshot
  const zh = useZh()
  const copy = mediaText(zh)

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
      if (current?.origin === 'owned' && current.assetId && (current.phase === 'playing' || current.phase === 'paused' || current.verificationStatus === 'command_dispatched')) {
        try {
          const opened = await media.openAsset({ assetId: current.assetId, mediaSessionId: current.mediaSessionId })
          setState(prev => ({ ...prev, playbackUrl: opened.playbackUrl }))
        } catch {
          setState(prev => ({ ...prev, playbackUrl: null, notice: prev.notice || copy.channelDown }))
        }
      }
    } catch (error) {
      setState(prev => ({ ...prev, notice: failMessage(error, copy.refreshFailed, copy.disabled) }))
    }
  }, [activity, copy.channelDown, copy.disabled, copy.refreshFailed, media])

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
      if (action === 'play' || (action === 'toggle' && snapshot.phase !== 'playing')) setWantPlay(true)
      if (action === 'pause' || action === 'stop') setWantPlay(false)
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
        setState(prev => ({ ...prev, snapshot: created.snapshot, operation: created.operation, assets: picked.assets, busy: false }))
        await refresh()
      } catch (error) {
        setState(prev => ({ ...prev, assets: picked.assets, busy: false, notice: failMessage(error, copy.createFailed, copy.disabled) }))
      }
    } catch (error) {
      setState(prev => ({ ...prev, busy: false, notice: failMessage(error, copy.pickFailed, copy.disabled) }))
    }
  }, [copy.createFailed, copy.disabled, copy.noFile, copy.pickFailed, media, refresh])

  const queue = useCallback(async (action: 'jump' | 'remove' | 'clear', itemId?: string) => {
    const snapshot = snapshotRef.current
    if (!snapshot) return
    setState(prev => ({ ...prev, busy: true, notice: '' }))
    const payload = action === 'clear'
      ? { mediaSessionId: snapshot.mediaSessionId, action, expectedQueueRevision: snapshot.queueRevision, operationId: newBridgeULID() }
      : { mediaSessionId: snapshot.mediaSessionId, action, itemId: itemId!, expectedQueueRevision: snapshot.queueRevision, operationId: newBridgeULID() }
    try {
      const result = await media.queueCommand(payload, { attempt: createMutationAttempt('media.queue.command', payload) })
      setState(prev => ({ ...prev, snapshot: result.snapshot, operation: result.operation, busy: false }))
      await refresh()
    } catch (error) {
      setState(prev => ({ ...prev, busy: false, notice: failMessage(error, copy.queueFailed, copy.disabled) }))
    }
  }, [copy.disabled, copy.queueFailed, media, refresh])

  const value = useMemo<MediaStoreValue>(() => ({
    ...state,
    pick,
    playPause: () => runCommand(state.snapshot?.phase === 'playing' || wantPlay ? 'pause' : 'play'),
    previous: () => runCommand('previous'),
    next: () => runCommand('next'),
    stop: () => runCommand('stop', { asStop: true }),
    jump: assetId => queue('jump', assetId),
    remove: assetId => queue('remove', assetId),
    clear: () => queue('clear'),
    seek: positionMs => runCommand('seek', { positionMs }),
    volume: volume => runCommand('set_volume', { volume }),
  }), [pick, queue, runCommand, state, wantPlay])

  const phase = miniPlayerPhase(page, state.snapshot, state.stopOperation)
  const current = state.assets.find(item => item.assetId === state.snapshot?.assetId)
  const title = current?.title || copy.untitled
  const recover = (item: ActivitySnapshotDTO) => {
    if (item.recoveryAction === 'open_settings') {
      setTarget(undefined)
      setSettingsCategory(OCR_SETTINGS_TARGET.settingsCategory)
      setSettingsIntelligenceView(OCR_SETTINGS_TARGET.settingsIntelligenceView)
      setPage('settings')
      setActivityOpen(false)
      return
    }
    if (item.recoveryAction === 'open_player') {
      setTarget(undefined)
      setPage('media')
      setActivityOpen(false)
      return
    }
    if (item.recoveryAction === 'retry' && item.retryable) {
      void runCommand('play')
      setActivityOpen(false)
    }
  }

  return (
    <MediaStoreContext.Provider value={value}>
      {children}
      <ActivityStatusButton items={state.activities} hub={page === 'agentHub'} open={activityOpen} onToggle={() => setActivityOpen(open => !open)} />
      <ActivityCenter items={state.activities} open={activityOpen} onClose={() => setActivityOpen(false)} onRecover={recover} />
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
        snapshot={state.snapshot}
        src={state.playbackUrl}
        kind={current?.kind ?? null}
        wantPlay={wantPlay}
        onEnded={() => { void runCommand('next') }}
        onError={message => setState(prev => ({ ...prev, notice: message }))}
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
