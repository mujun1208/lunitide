// companionText.ts implements the M9.5 speech cleaning and segmentation
// rules (T-9.5.3.1): markdown symbols stripped, code blocks replaced
// with 「代码已省略」, URLs reduced to their domain, emoji removed
// (SAPI does not read them), then split on 。？！and newlines with a
// hard 500-char cap per segment (comma re-split for over-long
// sentences) and at most 20 segments per reply.
export const MAX_SEGMENT_CHARS = 500
export const MAX_SEGMENTS = 20
/** First audible chunk: a short complete sentence, else this many characters. */
export const FIRST_SPEAK_CHARS = 6
/** Later chunks prefer a sentence, but do not stall the voice for long unpunctuated runs. */
export const FOLLOW_SPEAK_CHARS = 14

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

/**
 * Pick the next TTS utterance from a growing LLM stream.
 * Offsets are in the raw `pending` string (same indexing as assistantText)
 * so the caller can advance spokenUpTo without cleaning first.
 *
 * Doubao-style: prefer a complete sentence; never speak a 2–3 word
 * comma fragment; only force a prefix when the model dumps a long
 * unpunctuated run and the user would otherwise wait in silence.
 */
export function takeSpeakableChunk(pending: string, isFirst: boolean): { text: string; consumed: number } | null {
  if (!pending.trim()) return null
  const sentence = /^([\s\S]*?[。？！!?\n])/.exec(pending)
  if (sentence && sentence[1].replace(/\s/g, '').length > 0) {
    return { text: sentence[1], consumed: sentence[1].length }
  }
  const forceAt = isFirst ? FIRST_SPEAK_CHARS : FOLLOW_SPEAK_CHARS
  const minClause = isFirst ? 4 : 12
  const clause = /^([\s\S]*?[，,、；;])/.exec(pending)
  if (clause && Array.from(clause[1]).length >= minClause) {
    return { text: clause[1], consumed: clause[1].length }
  }
  if (Array.from(pending).length < forceAt) return null
  const prefix = Array.from(pending).slice(0, forceAt).join('')
  return { text: prefix, consumed: prefix.length }
}
