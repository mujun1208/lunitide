import { looksIncompleteUtterance } from './companionText'

const compact = (text: string) => text.replace(/[\s。！？!?，,、；;：:]+/gu, '')

const SHORT_ORDER = /^(?:暂停|停下|取消|停)$/

function sharedPrefixRunes(a: string, b: string): number {
  const left = [...a]
  const right = [...b]
  let i = 0
  while (i < left.length && i < right.length && left[i] === right[i]) i++
  return i
}

/** A finish/poll may repeat only a short prefix or tail of the same caption.
 * Keep the already heard words in that case. A shorter fragment is a cut
 * packet. A non-overlapping next packet is the rest of this utterance. */
export function pickTranscriptRevision(previous: string, incoming: string): string {
  const next = incoming.trim()
  const prev = previous.trim()
  if (!next) return prev
  if (!prev) return next
  const a = compact(prev)
  const b = compact(next)
  if (b && (a.startsWith(b) || a.endsWith(b) || b.startsWith(a) || b.endsWith(a))) {
    if ([...a].length !== [...b].length) return [...a].length > [...b].length ? prev : next
    return next.length >= prev.length ? next : prev
  }
  if (SHORT_ORDER.test(b)) return next
  const aRunes = [...a]
  const bRunes = [...b]
  const shared = sharedPrefixRunes(a, b)
  // A near-same-length shared prefix is a correction. A much shorter
  // fragment is a cut packet and must not replace the heard sentence.
  if (shared >= 4 && bRunes.length + 1 >= aRunes.length) return next
  // An unfinished command plus a later packet that does not overlap it is
  // the rest of the same utterance. Both strings came from the recognizer.
  if (looksIncompleteUtterance(prev) && shared === 0 && !/[。？！?!…]$/u.test(prev)) return prev + next
  if (bRunes.length < aRunes.length) return prev
  return next
}
