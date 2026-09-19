import type { MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import type { Page } from '../app/appTypes'

export type MiniPlayerPhase = 'hidden' | 'active' | 'closing' | 'close_error'

const STOP_PENDING = new Set(['requested', 'awaiting_approval', 'dispatching', 'verifying'])

export function miniPlayerPhase(page: Page, snapshot: MediaSnapshotDTO | null, stopOp: MediaOperationDTO | null): MiniPlayerPhase {
  if (stopOp) {
    if (STOP_PENDING.has(stopOp.phase)) return 'closing'
    if ((stopOp.phase === 'failed' || stopOp.phase === 'uncertain') && snapshot?.phase !== 'stopped') return 'close_error'
    if (stopOp.phase === 'succeeded' && snapshot?.phase === 'stopped') return 'hidden'
    if (stopOp.phase === 'cancelled' && (snapshot?.phase === 'playing' || snapshot?.phase === 'paused')) return page === 'media' ? 'hidden' : 'active'
  }
  if (page === 'media' || !snapshot) return 'hidden'
  if (snapshot.phase === 'playing' || snapshot.phase === 'paused') return 'active'
  return 'hidden'
}

export function needsPlaybackOpen(snapshot: MediaSnapshotDTO | null, playbackUrl: string | null, openedAssetId: string | null, wantPlay: boolean, openedEpoch?: number | null): boolean {
  if (!wantPlay || !snapshot || snapshot.origin !== 'owned' || !snapshot.assetId) return false
  if (snapshot.phase !== 'playing' && snapshot.phase !== 'paused' && snapshot.verificationStatus !== 'command_dispatched') return false
  if (playbackUrl && openedAssetId === snapshot.assetId && (openedEpoch == null || openedEpoch === snapshot.playbackEpoch)) return false
  return true
}

export function formatClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}
