import { compactMeetingText, isolateCurrentUtterance } from '../../../meetings/meetingText'

/** A full text snapshot with an exact committed prefix proves which words
 * are new, even when they repeat the preceding turn verbatim. Punctuation
 * may settle after a product-side commit, so compare spoken characters. */
export function isolateVolcTextSnapshot(committed: string, incoming: string): string {
  const next = incoming.trim()
  const prefix = compactMeetingText(committed)
  if (!prefix) return next
  const compact = compactMeetingText(next)
  if (compact === prefix) return ''
  if (compact.startsWith(prefix)) {
    let matched = ''
    let offset = 0
    for (const char of next) {
      offset += char.length
      matched += compactMeetingText(char)
      if (matched === prefix) return next.slice(offset).replace(/^[。！？!?，,、；;：:\s]+/u, '').trim()
    }
  }
  // Older providers may revise previous words or return only a recent tail.
  // Preserve the existing reconciliation only when the prefix is uncertain.
  return isolateCurrentUtterance(committed, next)
}
