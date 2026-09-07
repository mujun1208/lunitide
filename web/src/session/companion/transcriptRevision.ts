const compact = (text: string) => text.replace(/[\s。！？!?，,、；;：:]+/gu, '')

/** A finish/poll may repeat only a short prefix or tail of the same caption.
 * Keep the already heard words in that case; ordinary ASR corrections replace
 * the hypothesis, including a genuinely shorter but different sentence. */
export function pickTranscriptRevision(previous: string, incoming: string): string {
  const next = incoming.trim()
  const prev = previous.trim()
  if (!next) return prev
  if (!prev) return next
  const a = compact(prev)
  const b = compact(next)
  if (b && a.includes(b) && b.length < a.length * 0.6) return prev
  return next
}
