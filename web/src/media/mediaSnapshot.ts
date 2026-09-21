import type { MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import type { Page } from '../app/appTypes'

export type MiniPlayerPhase = 'hidden' | 'active' | 'closing' | 'close_error'

export function miniPlayerPhase(_page: Page, _snapshot: MediaSnapshotDTO | null, _stopOp: MediaOperationDTO | null): MiniPlayerPhase {
  // Chat / Settings / Hub must not pin a floating bar. Playback chrome lives on Media Center.
  return 'hidden'
}

const ticketRenewAheadMs = 10_000

export function shouldDetachPlayback(snapshot: MediaSnapshotDTO | null): boolean {
  return !snapshot || snapshot.origin !== 'owned' || !snapshot.assetId || snapshot.phase === 'stopped'
}

export function playbackTicketStale(expiresAt: string | null | undefined, nowMs = Date.now()): boolean {
  if (expiresAt == null || expiresAt === '') return false
  const exp = Date.parse(expiresAt)
  if (!Number.isFinite(exp)) return false
  return exp - nowMs < ticketRenewAheadMs
}

export function needsPlaybackOpen(snapshot: MediaSnapshotDTO | null, playbackUrl: string | null, openedAssetId: string | null, openedEpoch?: number | null, expiresAt?: string | null): boolean {
  if (shouldDetachPlayback(snapshot) || !snapshot) return false
  const sameAsset = Boolean(playbackUrl && openedAssetId === snapshot.assetId && (openedEpoch == null || openedEpoch === snapshot.playbackEpoch))
  if (sameAsset) {
    if (snapshot.phase === 'playing' || snapshot.phase === 'paused') return false
    return playbackTicketStale(expiresAt)
  }
  return true
}

export function mediaTransportPlaying(_wantPlay: boolean, snapshot: MediaSnapshotDTO | null): boolean {
  return snapshot?.phase === 'playing'
}

export function mediaTransportCommand(snapshot: MediaSnapshotDTO | null): 'play' | 'pause' {
  return mediaTransportPlaying(false, snapshot) ? 'pause' : 'play'
}

export function formatClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}
