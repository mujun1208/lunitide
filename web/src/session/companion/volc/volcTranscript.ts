import { isolateCurrentUtterance, joinMeetingLines } from '../../../meetings/meetingText'
import { pickTranscriptRevision } from '../transcriptRevision'

export type VolcUtterance = { text: string; startMs: number; endMs: number; final: boolean }

/** Consume full SAUC snapshots by audio position, not by text similarity.
 * Corrections at a known position replace it; repeated words at a later
 * position remain a new utterance. The watermark survives silence and TTS. */
export function createVolcTranscriptCursor() {
  let committedThrough = -1
  let segments = new Map<number, VolcUtterance>()
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
          if (incoming.startMs <= committedThrough || !incoming.text.trim()) continue
          const previous = segments.get(incoming.startMs)
          if (previous && incoming.endMs < previous.endMs) continue
          if (previous?.final && !incoming.final) continue
          segments.set(incoming.startMs, {
            ...incoming,
            text: pickTranscriptRevision(previous?.text ?? '', incoming.text),
          })
        }
        const ordered = [...segments.values()].sort((a, b) => a.startMs - b.startMs)
        current = ordered.reduce((text, segment) => joinMeetingLines(text, segment.text), '')
        final = ordered.at(-1)?.final ?? false
      } else if (!timestamped) {
        // Older engines have no audio positions. Even in this fallback a full
        // snapshot is a replacement, never another clause to append.
        legacySnapshot = pickTranscriptRevision(legacySnapshot, snapshot)
        current = pickTranscriptRevision(current, isolateCurrentUtterance(legacyCommitted, legacySnapshot))
        final = ended
      }
      return { text: current, final }
    },
    current: () => ({ text: current, final, timestamped }),
    commit(): string {
      const out = current
      if (segments.size) committedThrough = Math.max(committedThrough, ...segments.keys())
      segments.clear()
      if (!timestamped) legacyCommitted = legacySnapshot
      current = ''
      final = false
      return out
    },
    reset() {
      committedThrough = -1
      segments = new Map()
      timestamped = false
      legacyCommitted = ''
      legacySnapshot = ''
      current = ''
      final = false
    },
  }
}
