import { cleanUserTranscript } from '../session/companion/companionText'
import { collapseTandemRepeats } from '../session/companion/speech'

/** Glue sherpa/Web Speech chops that arrive this close together. */
export const MEETING_MERGE_GAP_MS = 400
/** A short unpunctuated clause is probably mid-thought, not a finished line. */
export const MEETING_SHORT_CHARS = 16

const ACRONYMS: Array<[RegExp, string]> = [
  [/\bb\s*r\s*d\b/gi, 'BRD'],
  [/\bp\s*r\s*d\b/gi, 'PRD'],
  [/\bo\s*k\s*r\b/gi, 'OKR'],
  [/\bk\s*p\s*i\b/gi, 'KPI'],
  [/\ba\s*p\s*i\b/gi, 'API'],
]

const ONLY_FILLERS = /^(?:嗯+|啊+|呃+|那个|然后)+[。！？!?，,、\s]*$/u
const MID_AH = /(?<![好是对行可吧呢嘛哦呀])啊+/gu
const THEN_THEN = /(?:然后){2,}/g

export function cleanMeetingTranscript(raw: string): string {
  let text = cleanUserTranscript(raw)
  if (!text) return ''
  for (const [pattern, replacement] of ACRONYMS) {
    text = text.replace(pattern, replacement)
  }
  text = text.replace(THEN_THEN, '然后')
  text = text.replace(/呃+/g, '')
  text = text.replace(MID_AH, '')
  text = text.replace(/嗯+/g, '')
  text = text.replace(/\s+/g, ' ').trim()
  if (!text || ONLY_FILLERS.test(text)) return ''
  text = punctuateMeetingLine(text)
  return text.replace(/^[，,、]+|[，,、]+$/g, '').trim()
}

function punctuateMeetingLine(text: string): string {
  let out = text
  out = out.replace(/([^\s。！？!?，,；;：:])(然后|但是|所以|不过|而且|另外)/g, '$1，$2')
  if (Array.from(out).length >= 8 && !/[。！？!?…]$/.test(out)) out += '。'
  return out
}

export function shouldMergeMeetingLines(prev: string, next: string, gapMs: number): boolean {
  if (!prev.trim() || !next.trim()) return false
  if (gapMs > MEETING_MERGE_GAP_MS) return false
  const left = prev.trim()
  const right = next.trim()
  if (/[。！？!?]$/.test(left) && /[。！？!?]$/.test(right)) return false
  if (/[。！？!?]$/.test(left) && Array.from(left).length >= MEETING_SHORT_CHARS) return false
  return true
}

export function joinMeetingLines(prev: string, next: string): string {
  const a = prev.trimEnd()
  const b = next.trimStart()
  if (!a) return b
  if (!b) return a
  if (/[。！？!?，,、；;]$/.test(a)) return a + b
  const last = a.charAt(a.length - 1)
  const first = b.charAt(0)
  if (/[A-Za-z0-9]/.test(last) && /[A-Za-z0-9]/.test(first)) return `${a} ${b}`
  return a + b
}

function suffixPrefixOverlap(prev: string, next: string): number {
  const max = Math.min(prev.length, next.length)
  for (let len = max; len > 0; len--) {
    if (prev.endsWith(next.slice(0, len))) return len
  }
  return 0
}

/**
 * Sherpa replaces `latest` when a new segment starts. Meeting notes keep the
 * earlier clause and glue the new one so a 1.2s engine endpoint does not drop
 * the first half of the thought.
 */
export function absorbHeldTranscript(sealed: string, incoming: string): string {
  const next = incoming.trim()
  if (!next) return sealed.trim()
  const prev = sealed.trim()
  if (!prev) return next
  if (next.startsWith(prev) || next.includes(prev)) return next
  if (prev.startsWith(next)) return prev
  // A streaming finish often returns only the last sherpa segment — not a revision.
  if (prev.includes(next) && Array.from(next).length < Array.from(prev).length * 0.6) return prev
  const overlap = suffixPrefixOverlap(prev, next)
  if (overlap > 0) return prev + next.slice(overlap)
  return joinMeetingLines(prev, next)
}

/**
 * When holdUtterance is on, the caption accumulates streaming segments in
 * `carried`, but commit() may return only the last segment unless the offline
 * refiner ran. Prefer the held caption unless the commit clearly covers it.
 */
export function pickMeetingFinalText(carried: string, settled: string): string {
  const c = carried.trim()
  const s = settled.trim()
  if (!c) return s
  if (!s) return c
  const cLen = Array.from(c).length
  const sLen = Array.from(s).length
  if (sLen >= cLen * 0.75 || (sLen > cLen && !c.includes(s))) return s
  if (c.includes(s) && sLen < cLen * 0.6) return c
  return absorbHeldTranscript(c, s)
}

export type MeetingLineBuffer = {
  push: (raw: string) => void
  flush: () => void
}

export function compactMeetingText(raw: string): string {
  return raw.replace(/[。！？!?，,、；;：:\s]+/gu, '')
}

/**
 * Take the current utterance out of a Volc `result_type=full` dump.
 * Never join unrelated clauses — that is what glued turn one onto turn two.
 */
export function isolateCurrentUtterance(committed: string, incoming: string, lastCurrent = ''): string {
  const next = incoming.trim()
  if (!next) return lastCurrent
  const prev = committed.trim()
  if (!prev) return collapseTandemRepeats(next)
  const prevC = compactMeetingText(prev)
  const nextC = compactMeetingText(next)
  if (!nextC || nextC === prevC) return ''
  if (nextC.startsWith(prevC) && prevC.length >= 1) {
    return finishIsolated(prevC, collapseTandemRepeats(sliceAfterCompactPrefix(next, prevC)))
  }
  if (prevC.startsWith(nextC) && nextC.length >= 4) return lastCurrent
  const overlap = suffixPrefixOverlap(prevC, nextC)
  if (overlap >= Math.min(8, Math.floor(prevC.length / 2))) {
    return finishIsolated(prevC, collapseTandemRepeats(sliceAfterCompactPrefix(next, prevC.slice(0, overlap))))
  }
  if (next.includes(prev)) {
    const cut = next.indexOf(prev) + prev.length
    return finishIsolated(prevC, collapseTandemRepeats(next.slice(cut).replace(/^[，,、。.!！？?\s]+/u, '').trim()))
  }
  return collapseTandemRepeats(next)
}

/** After peeling committed text, a tandem leftover of the same unit is not a new clause. */
function finishIsolated(prevCompact: string, current: string): string {
  if (!current) return ''
  const curC = compactMeetingText(current)
  if (!curC || curC === prevCompact) return ''
  return current
}

function sliceAfterCompactPrefix(next: string, prevCompact: string): string {
  if (!prevCompact) return next
  const chars = Array.from(next)
  let compact = ''
  for (let i = 0; i < chars.length; i++) {
    const ch = chars[i]!
    if (/[。！？!?，,、；;：:\s]/u.test(ch)) continue
    compact += ch
    if (compact === prevCompact) {
      return chars.slice(i + 1).join('').replace(/^[，,、。.!！？?\s]+/u, '').trim()
    }
  }
  return next
}

/** Keep only the new clause when ASR re-sends the whole history as one final. */
export function meetingLineDelta(prior: string, incoming: string): string {
  const prev = prior.trim()
  const next = collapseTandemRepeats(incoming.trim())
  if (!next) return ''
  if (!prev) return next
  if (next === prev) return ''
  const prevC = compactMeetingText(prev)
  const nextC = compactMeetingText(next)
  if (!nextC || nextC === prevC) return ''
  if (prevC.startsWith(nextC) && nextC.length >= 4) return ''
  if (nextC.startsWith(prevC) && prevC.length >= 4) {
    return collapseTandemRepeats(sliceAfterCompactPrefix(next, prevC))
  }
  if (prev.includes(next) && Array.from(next).length < Array.from(prev).length * 0.6) return ''
  const overlap = suffixPrefixOverlap(prevC, nextC)
  if (overlap >= Math.min(8, Math.floor(prevC.length / 2))) {
    return collapseTandemRepeats(sliceAfterCompactPrefix(next, prevC.slice(0, overlap)))
  }
  return next
}

/** Live captions: drop exact/prefix replays and intra-line tandem copies. */
export function collapseLiveTranscriptLines(lines: string[]): string[] {
  const out: string[] = []
  for (const raw of lines) {
    const text = collapseTandemRepeats(raw.trim())
    if (!text) continue
    if (out.length === 0) {
      out.push(text)
      continue
    }
    const last = out[out.length - 1]!
    const delta = meetingLineDelta(last, text)
    if (!delta) continue
    const lastC = compactMeetingText(last)
    const nextC = compactMeetingText(text)
    if (nextC.startsWith(lastC) && lastC.length >= 4) {
      out[out.length - 1] = text
      continue
    }
    out.push(delta)
  }
  return out
}

export function createMeetingLineBuffer(emit: (line: string) => void): MeetingLineBuffer {
  let pending = ''
  let lastEmitted = ''
  let lastAt = 0
  let timer = 0

  const flush = () => {
    window.clearTimeout(timer)
    timer = 0
    const line = pending.trim()
    pending = ''
    lastAt = 0
    if (!line) return
    const delta = meetingLineDelta(lastEmitted, line)
    if (!delta) return
    lastEmitted = lastEmitted && compactMeetingText(line).startsWith(compactMeetingText(lastEmitted))
      ? line
      : lastEmitted ? `${lastEmitted}${delta}` : delta
    emit(delta)
  }

  return {
    push(raw: string) {
      const cleaned = meetingLineDelta(lastEmitted, cleanMeetingTranscript(raw))
      if (!cleaned) return
      const now = Date.now()
      const gap = lastAt ? now - lastAt : Number.POSITIVE_INFINITY
      if (pending && shouldMergeMeetingLines(pending, cleaned, gap)) {
        pending = cleanMeetingTranscript(joinMeetingLines(pending, cleaned)) || joinMeetingLines(pending, cleaned)
      } else {
        if (pending) {
          const prev = pending
          pending = ''
          const delta = meetingLineDelta(lastEmitted, prev)
          if (delta) {
            lastEmitted = lastEmitted && compactMeetingText(prev).startsWith(compactMeetingText(lastEmitted))
              ? prev
              : lastEmitted ? `${lastEmitted}${delta}` : delta
            emit(delta)
          }
        }
        pending = cleaned
      }
      lastAt = now
      window.clearTimeout(timer)
      const hold = /[。！？!?]$/.test(pending) && Array.from(pending).length >= MEETING_SHORT_CHARS
        ? 80
        : MEETING_MERGE_GAP_MS
      timer = window.setTimeout(flush, hold)
    },
    flush,
  }
}
