// companionText.ts implements the M9.5 speech cleaning and segmentation
// rules (T-9.5.3.1): markdown symbols stripped, code blocks replaced
// with 「代码已省略」, URLs reduced to their domain, emoji removed
// (SAPI does not read them), then split on 。？！and newlines with a
// hard 500-char cap per segment (comma re-split for over-long
// sentences) and at most 20 segments per reply.
export const MAX_SEGMENT_CHARS = 500
export const MAX_SEGMENTS = 20
/** First audible chunk: speak almost immediately (2 chars) so voice tracks the stream. */
export const FIRST_SPEAK_CHARS = 2
/** Later chunks prefer a sentence, but do not stall the voice for long unpunctuated runs. */
export const FOLLOW_SPEAK_CHARS = 8

const TRUNCATION_NOTICE = '后续内容请看字幕'

export function cleanForSpeech(raw: string): string {
  let text = raw
  // Code fences first (their bodies must never be read aloud).
  text = text.replace(/```[\s\S]*?```/g, '代码已省略')
  text = text.replace(/`[^`\n]*`/g, '代码已省略')
  // URLs keep only the domain (the whole URL is consumed, path included).
  text = text.replace(/https?:\/\/([^\s/)]+)[^\s)]*/g, '$1')
  // Markdown emphasis / heading / list markers / links / quotes.
  text = text.replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
  text = text.replace(/^#{1,6}\s*/gm, '')
  text = text.replace(/(\*\*|__|\*|~~)/g, '')
  text = text.replace(/^\s*[-*+>]\s+/gm, '')
  text = text.replace(/^\s*\d+\.\s+/gm, '')
  // Tables lose their separator rows.
  text = text.replace(/^\s*\|?[:\-\s|]+\|?\s*$/gm, '')
  text = text.replace(/\|/g, ' ')
  // Emoji and pictographs (SAPI stays silent on them).
  text = text.replace(/[\u{1F300}-\u{1FAFF}\u{1F000}-\u{1F2FF}\u{2600}-\u{27BF}\u{FE0F}\u{200D}]/gu, '')
  // Drop spaces orphaned next to CJK punctuation by emoji removal.
  text = text.replace(/\s+([，。！？；：、）】」』])/gu, '$1')
  // Collapse the whitespace left behind.
  text = text.replace(/[ \t]+/g, ' ')
  text = text.replace(/\n{2,}/g, '\n')
  return text.trim()
}

export function segmentForSpeech(cleaned: string): string[] {
  const segments: string[] = []
  const sentences = cleaned
    .split(/(?<=[。？！\n])/)
    .map(part => part.trim())
    .filter(part => part.length > 0)

  for (const sentence of sentences) {
    if (Array.from(sentence).length <= MAX_SEGMENT_CHARS) {
      segments.push(sentence)
      continue
    }
    // Over-long sentence: re-split on commas, still capped.
    let current = ''
    for (const clause of sentence.split(/(?<=[，,；;:：])/)) {
      if (Array.from(current + clause).length > MAX_SEGMENT_CHARS && current) {
        segments.push(current)
        current = clause
      } else {
        current += clause
      }
    }
    if (current.trim()) segments.push(current.trim())
  }

  if (segments.length > MAX_SEGMENTS) {
    const kept = segments.slice(0, MAX_SEGMENTS)
    kept[MAX_SEGMENTS - 1] = `${kept[MAX_SEGMENTS - 1]}。${TRUNCATION_NOTICE}。`
    return kept
  }
  return segments
}

export function prepareSpeech(text: string): string[] {
  return segmentForSpeech(cleanForSpeech(text))
}

/** Strip spaces and punctuation so TTS and SR transcripts can be compared. */
export function compactSpeech(text: string): string {
  return text.replace(/[\s\p{P}\p{S}]/gu, '').toLowerCase()
}

const LEADING_FILLERS = /^(?:嗯+|啊+|呃+|那个+|就是说+|就是+|然后+|所以说+|你知道+|怎么说呢)+[，,、\s]*/u
const MID_FILLERS = /([，,。！？；;])\s*(?:嗯+|啊+|呃+)(?=\s|[，,。！？；;]|$)/gu
const TRAILING_FILLERS = /[，,、\s]+(?:嗯+|啊+|呃+)\s*$/u

/** Shannon-style local cleanup: drop oral fillers while keeping the user's meaning. */
export function cleanUserTranscript(raw: string): string {
  let text = raw.replace(/\s+/g, ' ').trim()
  if (!text) return ''
  for (let pass = 0; pass < 3; pass++) {
    const next = text.replace(LEADING_FILLERS, '').replace(MID_FILLERS, '$1').replace(TRAILING_FILLERS, '').trim()
    if (next === text) break
    text = next
  }
  return text.replace(/^[，,、]+|[，,、]+$/g, '').trim()
}

/**
 * True when the recognizer almost certainly heard our own TTS playback
 * (speaker → microphone loop) rather than a new user utterance.
 */
export function looksLikePlaybackEcho(heard: string, spoken: string): boolean {
  const a = compactSpeech(heard)
  const b = compactSpeech(spoken)
  if (a.length < 4 || b.length < 4) return false
  if (b.includes(a) || a.includes(b)) return true
  const window = Math.min(6, a.length)
  if (window < 6) return false
  for (let i = 0; i <= a.length - window; i++) {
    if (b.includes(a.slice(i, i + window))) return true
  }
  return false
}

/**
 * Pick the next TTS utterance from a growing LLM stream.
 * Offsets are in the raw `pending` string (same indexing as assistantText)
 * so the caller can advance spokenUpTo without cleaning first.
 *
 * Real-time voice: prefer a complete sentence; on the first chunk speak
 * as soon as two characters land; later chunks wait for punctuation or
 * a slightly longer prefix so playback stays natural.
 */
export function takeSpeakableChunk(pending: string, isFirst: boolean, force = false): { text: string; consumed: number } | null {
  if (!pending.trim()) return null
  const sentence = /^([\s\S]*?[。？！!?\n])/.exec(pending)
  if (sentence && sentence[1].replace(/\s/g, '').length > 0) {
    return { text: sentence[1], consumed: sentence[1].length }
  }
  const forceAt = isFirst ? FIRST_SPEAK_CHARS : FOLLOW_SPEAK_CHARS
  const minClause = isFirst ? 2 : 8
  const clause = /^([\s\S]*?[，,、；;])/.exec(pending)
  if (clause && Array.from(clause[1]).length >= minClause) {
    return { text: clause[1], consumed: clause[1].length }
  }
  if (Array.from(pending).length < forceAt && !force) return null
  if (force) {
    return { text: pending, consumed: pending.length }
  }
  const prefix = Array.from(pending).slice(0, forceAt).join('')
  return { text: prefix, consumed: prefix.length }
}
