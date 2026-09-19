import type { MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import type { Page } from '../app/appTypes'

export type MiniPlayerPhase = 'hidden' | 'active' | 'closing' | 'close_error'

export function miniPlayerPhase(_page: Page, _snapshot: MediaSnapshotDTO | null, _stopOp: MediaOperationDTO | null): MiniPlayerPhase {
  // Chat / Settings / Hub must not pin a floating bar. Playback chrome lives on Media Center.
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
