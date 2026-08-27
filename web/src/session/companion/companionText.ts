// companionText.ts implements the M9.5 speech cleaning and segmentation
// rules (T-9.5.3.1): markdown symbols stripped, code blocks replaced
// with 「代码已省略」, URLs reduced to their domain, emoji removed
// (SAPI does not read them). Conversational replies stay one clip so
// TTS does not pause at every period. Only replies over 1200 chars
// split, with a comma re-split for over-long sentences, at most 20
// segments per reply.
export const MAX_SEGMENT_CHARS = 1200
export const MAX_SEGMENTS = 20
/** First unpunctuated flush: only used when the stream stalls, never to slice commas. */
export const FIRST_SPEAK_CHARS = 4
/** Later stalled tails may flush a bit later so a sentence can still land. */
export const FOLLOW_SPEAK_CHARS = 4
/**
 * First-token watchdog. sendAndChat marks the turn `streaming` before
 * chat.start returns, so 5–6s used to cancel a live DeepSeek V4 request
 * during TTFT. Voice turns wait for a real answer token, not a connecting
 * spinner.
 */
export const COMPANION_FIRST_TOKEN_STREAMING_MS = 12_000
export const COMPANION_FIRST_TOKEN_CONNECTING_MS = 6_000
export const COMPANION_AFTER_TOKEN_MS = 45_000

export function companionInstantAck(userText: string): string {
  const text = userText.trim()
  if (!text) return '嗯，我在。'
  if (/^(你好|您好|嗨|嘿|在吗|在不在)/.test(text)) return '嗨，我在呢。'
  if (/[？?]$/.test(text)) return '嗯，'
  if (/[。！!…]$/.test(text) && text.length >= 4) return '嗯，我听到了。'
  return '嗯，'
}

export function companionReplyStallMs(chatStreaming: boolean, hasAssistantText: boolean): number {
  if (hasAssistantText) return COMPANION_AFTER_TOKEN_MS
  return chatStreaming ? COMPANION_FIRST_TOKEN_STREAMING_MS : COMPANION_FIRST_TOKEN_CONNECTING_MS
}

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
  const text = cleaned.trim()
  if (!text) return []
  const compact = text.replace(/\n+/g, '')
  if (Array.from(compact).length <= MAX_SEGMENT_CHARS) return [compact]

  const segments: string[] = []
  const sentences = text
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
    return mergeShortSegments(kept)
  }
  return mergeShortSegments(segments)
}

/** Glue tiny acknowledgement fragments onto the next clause so playback does not stutter. */
export function mergeShortSegments(segments: string[]): string[] {
  if (segments.length <= 1) return segments
  const merged: string[] = []
  for (const segment of segments) {
    const prev = merged[merged.length - 1]
    if (prev && shouldMergeAckSegment(prev, segment)) {
      merged[merged.length - 1] = prev + segment
    } else {
      merged.push(segment)
    }
  }
  return merged
}

function shouldMergeAckSegment(prev: string, next: string): boolean {
  if (!next.trim()) return false
  if (Array.from(prev + next).length > MAX_SEGMENT_CHARS) return false
  if (/[？！?!]/.test(prev)) return false
  return /^(?:好的|嗯|对|行|好|是的|可以|明白|收到)[。！？，,]?$/u.test(prev.trim()) || Array.from(prev.trim()).length <= 2
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
/**
 * Words that still need their object: 「合肥的」「帮我打开」 really are
 * mid-sentence, so endpointing waits for the rest.
 */
const INCOMPLETE_TAIL =
  /(?:儿|的|把|给|和|与|或|从|往|向|在|到|去|来|做|说|问|查|看|听|用|帮|请|要|想|能|会|可|以|这|那|哪|啥|一个|一下|怎么)$/u

/**
 * Sentence-final particles and question words. 「我知道了」「你在干什么」
 * are whole turns; treating them as mid-sentence added 1.6–2.2s of dead
 * air to a large share of ordinary Chinese speech.
 */
const COMPLETE_TAIL =
  /(?:了|吗|吧|呢|啊|呀|哦|嘛|什么|怎么样|怎么办|哪里|哪儿|多少|几点|为什么|好不好|行不行)$/u

/** Bare modal/auxiliary starts — Windows often marks these isFinal mid-sentence. */
const INCOMPLETE_STARTERS =
  /^(?:你可以|你能|帮我|请你|能不能|是不是|要不要|我想|我要)$/u
/** Command still waiting for the action: 「你可以帮我…」 */
const INCOMPLETE_OPENERS =
  /^(?:你可以|你能|能不能|请你帮|麻烦你)/u
/** Phrase ended on the helper, not the task. */
const INCOMPLETE_ENDINGS = /(?:帮我|给我|为我)$/u

const SPEECH_CORRECTIONS: Array<[RegExp, string]> = [
  [/岳西|越席|月西|悦溪|跃溪|月息|悦西|悦希|月希|月夕|月惜|越汐/g, '月汐'],
  [/你好岳西|你好月西|你好悦溪|你好月夕/g, '你好月汐'],
  [/店面文件|店面的/g, '桌面文件'],
  [/打开店面/g, '打开桌面'],
  [/店面/g, '桌面'],
  [/气水音乐|起水音乐|七水音乐|汽水音月/g, '汽水音乐'],
  [/帮我打开桌面的/g, '帮我打开桌面'],
  [/帮我打开一个/g, '帮我打开'],
]

/** Whole greetings / acknowledgements — commit quickly even without punctuation. */
const COMPLETE_SHORT_UTTERANCE =
  /^(?:你好(?:月汐|啊|呀)?|嗨|嘿|在吗|在不在|听到了|谢谢|再见|拜拜|早上好|晚上好|下午好|好的|好啊|嗯嗯|月汐|停|停下|别说了|继续)$/

/** True when the recognizer likely stopped mid-thought — wait longer before commit. */
export function looksIncompleteUtterance(text: string): boolean {
  const trimmed = text.trim()
  if (!trimmed) return false
  if (/[。？！?!…]$/.test(trimmed)) return false
  if (COMPLETE_SHORT_UTTERANCE.test(trimmed)) return false
  if (INCOMPLETE_STARTERS.test(trimmed)) return true
  if (INCOMPLETE_ENDINGS.test(trimmed) && Array.from(trimmed).length <= 6) return true
  if (INCOMPLETE_OPENERS.test(trimmed) && Array.from(trimmed).length <= 5) return true
  if (COMPLETE_TAIL.test(trimmed)) return false
  if (INCOMPLETE_TAIL.test(trimmed)) return true
  // Short unpunctuated fragments like「你可以」are mid-command, not a turn.
  return Array.from(trimmed).length <= 3
}

/** Drop machine self-reports. The user asked not to hear 「我做完了」. */
export function stripTaskDonePhrases(raw: string): string {
  const trimmed = raw
    .replace(/我已经做完了[。.!！]?\s*/g, '')
    .replace(/我做完了[。.!！]?\s*/g, '')
    .replace(/任务已完成[。.!！]?\s*/g, '')
    .trim()
  if (looksLikeOmniPersonaCaption(trimmed)) return ''
  return trimmed
}

/**
 * Whether a transcript is a new user turn, not her reply coming back
 * through the microphone, and not a leftover from the previous round.
 */
export function shouldAcceptUserTranscript(input: {
  state: 'idle' | 'listening' | 'thinking' | 'speaking'
  text: string
  lastSpoken: string
  lastAssistant: string
}): boolean {
  if (input.state === 'speaking' || input.state === 'thinking') return false
  if (!input.text.trim()) return false
  if (looksLikeOmniPersonaCaption(input.text)) return false
  if (looksLikePlaybackEcho(input.text, input.lastSpoken)) return false
  if (input.lastAssistant && looksLikePlaybackEcho(input.text, input.lastAssistant)) return false
  return true
}

/** Settings/clone labels must never become a dialogue round. */
export function looksLikeOmniPersonaCaption(text: string): boolean {
  const compact = text.replace(/\s+/g, '')
  return compact.startsWith('人生：') || compact.startsWith('人生:') || compact.includes('月汐/人生')
}

/** The hands-free loop stays armed until the user exits or pauses the mic. */
export function shouldKeepHandsFreeLoop(input: {
  exited: boolean
  userPausedMic: boolean
  errorCode?: string
}): boolean {
  if (input.exited || input.userPausedMic) return false
  if (input.errorCode === 'MICROPHONE_PERMISSION_DENIED') return false
  return true
}

export function handsFreeRetryDelayMs(silentRestarts: number): number {
  const n = Math.min(Math.max(0, silentRestarts), 5)
  return Math.min(8000, 400 * 2 ** n)
}

export function cleanUserTranscript(raw: string): string {
  let text = raw.replace(/\s+/g, ' ').trim()
  if (!text) return ''
  for (const [pattern, replacement] of SPEECH_CORRECTIONS) {
    text = text.replace(pattern, replacement)
  }
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
  if (!a || !b) return false
  // Laptop speaker bleed often comes back as a short fragment of the last line.
  if (a.length >= 2 && b.includes(a)) return true
  if (b.length >= 4 && a.includes(b)) return true
  const window = Math.min(6, a.length)
  if (window < 4) return false
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
 * Speak whole sentences so TTS is one continuous reading, not comma clips.
 * Commas stay inside the sentence. If the stream stalls with no period,
 * `force` flushes the leftover as a single clip.
 */
export function takeSpeakableChunk(pending: string, isFirst: boolean, force = false): { text: string; consumed: number } | null {
  if (!pending.trim()) return null
  // Buffer every complete sentence currently in hand so punctuation does
  // not become a clip boundary — one synth of “你好呀。我是月汐。”
  // sounds like one breath, not two readings.
  const sentences = /^((?:[\s\S]*?[。？！!?\n])+)/.exec(pending)
  if (sentences && sentences[1].replace(/\s/g, '').length > 0) {
    return { text: sentences[1], consumed: sentences[1].length }
  }
  if (!force) return null
  const minChars = isFirst ? FIRST_SPEAK_CHARS : FOLLOW_SPEAK_CHARS
  if (Array.from(pending).length < minChars) return null
  return { text: pending, consumed: pending.length }
}
