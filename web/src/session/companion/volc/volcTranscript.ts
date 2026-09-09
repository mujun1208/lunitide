import { joinMeetingLines } from '../../../meetings/meetingText'
import { pickTranscriptRevision } from '../transcriptRevision'
import { isolateVolcTextSnapshot } from './volcTextSnapshot'

export type VolcUtterance = { text: string; startMs: number; endMs: number; final: boolean }

/** Consume full SAUC snapshots by audio position, not by text similarity.
 * Corrections at a known position replace it; repeated words at a later
 * position remain a new utterance. The watermark survives silence and TTS. */
export function createVolcTranscriptCursor() {
  let committedThrough = -1
  let committedAudioEnd = -1
  // Keep just the last submitted provider segment, not the meeting history.
  // A provider can continue this same nonfinal segment after our silence timer
  // already submitted its prefix. Exact prefix + later audio proves new words.
  let committedSource: VolcUtterance | undefined
  let segments = new Map<number, VolcUtterance>()
  let sourceSnapshots = new Map<number, VolcUtterance>()
  let timestamped = false
  let legacyCommitted = ''
  let legacySnapshot = ''
  let current = ''
  let final = false

  return {
    update(snapshot: string, ended: boolean, utterances?: VolcUtterance[]): { text: string; final: boolean } {
      if (utterances?.length) {
        timestamped = true
        for (const incoming of utterances) {
          // Final VAD packets can split one submitted sentence into new IDs.
          // A new start timestamp inside already committed audio is still old
          // speech. Checking only the original start revived a 1–2 word tail
          // as another user turn and overwrote the complete sentence on screen.
          if (incoming.endMs <= committedAudioEnd || !incoming.text.trim()) continue
          let text = incoming.text.trim()
          if (incoming.startMs <= committedThrough) {
            if (!committedSource || incoming.startMs !== committedSource.startMs || incoming.endMs <= committedSource.endMs || !text.startsWith(committedSource.text)) continue
            text = text.slice(committedSource.text.length).trim()
            // Changed punctuation alone is a revision, not a spoken turn.
            if (!/[\p{L}\p{N}]/u.test(text)) continue
          }
          const previous = segments.get(incoming.startMs)
          if (previous && incoming.endMs < previous.endMs) continue
          if (previous?.final && !incoming.final) continue
          segments.set(incoming.startMs, {
            ...incoming,
            text: pickTranscriptRevision(previous?.text ?? '', text),
          })
          sourceSnapshots.set(incoming.startMs, { ...incoming, text: pickTranscriptRevision(sourceSnapshots.get(incoming.startMs)?.text ?? '', incoming.text) })
        }
        const ordered = [...segments.values()].sort((a, b) => a.startMs - b.startMs)
        current = ordered.reduce((text, segment) => joinMeetingLines(text, segment.text), '')
        final = ordered.at(-1)?.final ?? false
      } else if (!timestamped) {
        // Older engines have no audio positions. Even in this fallback a full
        // snapshot is a replacement, never another clause to append.
        legacySnapshot = pickTranscriptRevision(legacySnapshot, snapshot)
        current = pickTranscriptRevision(current, isolateVolcTextSnapshot(legacyCommitted, legacySnapshot))
        final = ended
      }
      return { text: current, final }
    },
    current: () => ({ text: current, final, timestamped }),
    commit(): string {
      const out = current
      if (segments.size) {
        committedThrough = Math.max(committedThrough, ...segments.keys())
        committedAudioEnd = Math.max(committedAudioEnd, ...[...segments.values()].map(segment => segment.endMs))
        committedSource = sourceSnapshots.get(committedThrough)
      }
      segments.clear()
      sourceSnapshots.clear()
      if (!timestamped) legacyCommitted = legacySnapshot
      current = ''
      final = false
      return out
    },
    reset() {
      committedThrough = -1
      committedAudioEnd = -1
      committedSource = undefined
      segments = new Map()
      sourceSnapshots = new Map()
      timestamped = false
      legacyCommitted = ''
      legacySnapshot = ''
      current = ''
      final = false
    },
  }
}
