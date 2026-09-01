// companionText.ts implements the M9.5 speech cleaning and segmentation
// rules (T-9.5.3.1): markdown symbols stripped, code blocks replaced
// with 「代码已省略」, URLs reduced to their domain, emoji removed
// (SAPI does not read them). Conversational replies stay one clip so
// TTS does not pause at every period. Only replies over 1200 chars
// split, with a comma re-split for over-long sentences, at most 20
// segments per reply.
import { correctAsrText } from './asrCorrections'
export const MAX_SEGMENT_CHARS = 1200
export const MAX_SEGMENTS = 20
/** First unpunctuated flush: greetings may speak immediately; other first clips wait for a phrase. */
export const FIRST_SPEAK_CHARS = 16
/** First-clip force flush (W2): 8 chars, not the old 16. */
export const FIRST_FORCE_SPEAK_CHARS = 8
/** Later stalled tails may flush a shorter leftover so the turn does not hang. */
export const FOLLOW_SPEAK_CHARS = 8
/** Streaming stall before we force-flush an unpunctuated first clause. */
export const FIRST_SPEAK_STALL_MS = 350
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

/** Spoken pad while the model thinks. One syllable; matches the silent TTS warmup. */
export const COMPANION_PAD_SPEECH = '嗯'

export function companionPadSpeech(): string {
  return COMPANION_PAD_SPEECH
}

export function isCompanionPadSpeech(text: string): boolean {
  return compactSpeech(text) === compactSpeech(COMPANION_PAD_SPEECH)
}

/**
 * Local ASR heard the user cutting in — not a one-character crumb, and not
 * the sentence currently coming out of the speaker.
 */
export function looksLikeBargeInSpeech(heard: string, spoken: string): boolean {
  if (compactSpeech(heard).length < 2) return false
  return !looksLikePlaybackEcho(heard, spoken)
}

/** ChatBridge companion turns refuse more than 2048 Unicode characters. */
export const COMPANION_PROMPT_MAX_CHARS = 2048

export function clipCompanionPrompt(text: string, maxChars = COMPANION_PROMPT_MAX_CHARS): string {
  const cleaned = text.replace(/\0/g, '')
  const chars = Array.from(cleaned)
  if (chars.length <= maxChars) return cleaned
  return chars.slice(0, maxChars).join('')
}

export function companionReplyStallMs(chatStreaming: boolean, hasAssistantText: boolean): number {
  if (hasAssistantText) return COMPANION_AFTER_TOKEN_MS
  return chatStreaming ? COMPANION_FIRST_TOKEN_STREAMING_MS : COMPANION_FIRST_TOKEN_CONNECTING_MS
}

/** Leftover previous-turn captions are not this turn's first token. */
export function companionHasFreshAssistantText(assistantText: string, staleReply = ''): boolean {
  const live = assistantText.trim()
  if (!live) return false
  return live !== staleReply.trim()
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
  return stripOralFillers(text.trim())
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
const FILLER_ONLY = /^(?:嗯+|啊+|呃+)[。.!！？?，,、\s]*$/u

/** Drop oral 嗯/啊/呃 pads. A clip that is only fillers is not spoken. */
function stripOralFillers(text: string): string {
  let next = text.trim()
  if (!next) return ''
  for (let pass = 0; pass < 3; pass++) {
    const stripped = next.replace(LEADING_FILLERS, '').replace(MID_FILLERS, '$1').replace(TRAILING_FILLERS, '').trim()
    if (stripped === next) break
    next = stripped
  }
  next = next.replace(/^[，,、]+|[，,、]+$/g, '').trim()
  if (FILLER_ONLY.test(next) || /^[。.!！？?，,、\s]*$/u.test(next)) return ''
  return next
}

/** Shannon-style local cleanup: drop oral fillers while keeping the user's meaning. */
/**
 * Words that still need their object: 「合肥的」「帮我打开」 really are
 * mid-sentence, so endpointing waits for the rest.
 */
const INCOMPLETE_TAIL =
  /(?:儿|的|把|给|和|与|或|从|往|向|在|到|去|来|做|说|问|查|看|听|用|帮|请|要|想|能|会|可|以|这|那|哪|啥|一个|一下|怎么|号码)$/u
/** Waiting for the value after a field name — 「文档的身份证号码」. */
const INCOMPLETE_FIELD_WAIT =
  /(?:身份证号码|证件号码|手机号|电话号码|联系电话|电话|文档的|文件里的|表格中的|这一栏的|那一格的)$/u

/** True when a comma lead is still waiting for an ID / phone / path slot. */
export function looksLikeIncompleteFieldWait(text: string): boolean {
  const compact = text.trim().replace(/\s+/g, '').replace(/[，,。？！.!?]+$/u, '')
  return INCOMPLETE_FIELD_WAIT.test(compact)
}

/** Closeout text already covered by streaming TTS — do not speak() it again. */
export function alreadySpokenCloseout(spoken: string, caption: string, spokenUpTo: number): boolean {
  if (spokenUpTo <= 0) return false
  const next = compactSpeech(spoken)
  if (!next) return false
  return compactSpeech(caption.slice(0, spokenUpTo)).includes(next)
}

/** Interrupt persist / history: keep only what was read aloud. */
export function clipAssistantToSpoken(full: string, spokenUpTo: number): string {
  if (spokenUpTo <= 0) return ''
  return full.slice(0, Math.min(spokenUpTo, full.length)).trim()
}

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
  /^(?:你可以|你能|能不能|请你帮|麻烦你|你帮我)/u
/** Phrase ended on the helper, not the task. */
const INCOMPLETE_ENDINGS = /(?:帮我|给我|为我)$/u
/** 「打开」「播放」with no object yet. */
const INCOMPLETE_BARE_COMMAND = /(?:打开|启动|运行|播放|把开)$/u
/** Windows finals 「把开了」 as complete (ends with 了) while they still say 桌面. */
const INCOMPLETE_OPEN_GARBLE =
  /^(?:把开了?|打开了|把它|把开了我|打开了我)$/u
/** Waiting for the filename: 「打开桌面上的」. */
const INCOMPLETE_DESKTOP_WAITING =
  /(?:桌面上的|桌面的|桌面上)$/u
/** App-name prefixes Windows often finals before the rest arrives. */
const INCOMPLETE_APP_PREFIX =
  /(?:打开|启动|把开)(?:网易云|网易|汽水|qq|QQ)$/u
/** One-syllable object that is almost certainly a cut-off app name. */
const INCOMPLETE_TRUNCATED_OPEN =
  /(?:打开|启动|运行|把开)(?:网|微|汽|酷|支|淘|钉|飞|邮|Q|q)$/u
/** Whole launch targets — do not wait as if the object were still coming. */
const COMPLETE_OPEN_OBJECTS =
  /(?:打开|启动|运行)(?:桌面|设置|文件|网页|微信|日历|浏览器|网易云音乐|汽水音乐|qq音乐|QQ音乐|协议文档|协议)$/u
/** Finished edit/fill commands — 「写进去」must not wait on trailing 去. */
const COMPLETE_ACTION_ENDINGS =
  /(?:写进去|填进去|填上|写入|加上|改好|做好|完成了|打开)$/u
/** Label + value after 后面写/填 — e.g. 身份证号码后面写204040 */
const COMPLETE_TYPE_AFTER_LABEL =
  /(?:身份证号码|证件号码|手机号|电话号码|通讯住址|联系电话|地址|姓名)(?:后面|之后)(?:写上|写入|写|填|输入)(.+)/u
/** Generic 后面写/填 with trailing value */
const COMPLETE_TYPE_AFTER_WRITE =
  /(?:后面|之后)(?:写上|写入|写|填|输入)([\dA-Za-z\u4e00-\u9fff]{2,})/u

const SPEECH_CORRECTIONS: Array<[RegExp, string]> = [
  [/岳西|越席|月西|悦溪|跃溪|月息|悦西|悦希|月希|月夕|月惜|越汐/g, '月汐'],
  [/你好岳西|你好月西|你好悦溪|你好月夕/g, '你好月汐'],
  [/店面文件|店面的/g, '桌面文件'],
  [/打开店面/g, '打开桌面'],
  [/店面/g, '桌面'],
  [/气水音乐|起水音乐|七水音乐|汽水音月/g, '汽水音乐'],
  [/网易云音(?!乐)/g, '网易云音乐'],
  [/打开网易云(?!音乐)/g, '打开网易云音乐'],
  [/打开网易(?!云)/g, '打开网易云音乐'],
  [/把开了?/g, '打开'],
  [/把它(?=桌面)/g, '打开'],
  [/打开了我/g, '打开'],
  [/写意文档|协意文档/g, '协议文档'],
  [/帮我打开桌面的/g, '帮我打开桌面'],
  [/帮我打开一个/g, '帮我打开'],
  [/\bb\s*r\s*d\b/gi, 'BRD'],
  [/\bp\s*r\s*d\b/gi, 'PRD'],
  [/\bo\s*k\s*r\b/gi, 'OKR'],
  [/\bk\s*p\s*i\b/gi, 'KPI'],
]

/** Windows often hears 「打开」 as 「把开」. Collapse that into a real command. */
export function repairOpenCommandTranscript(text: string): string {
  let t = text
  t = t.replace(/把开了?/g, '打开')
  t = t.replace(/把它(?=桌面)/g, '打开')
  t = t.replace(/打开了我/g, '打开')
  for (let i = 0; i < 4; i++) {
    const next = t.replace(/打开(?:我)?打开/g, '打开')
    if (next === t) break
    t = next
  }
  t = t.replace(/写意文档|协意文档/g, '协议文档')
  return t
}

/** Whole greetings / acknowledgements — commit quickly even without punctuation. */
const COMPLETE_SHORT_UTTERANCE =
  /^(?:你好(?:月汐|啊|呀)?|嗨(?:我在呢|我在)?|嘿|在吗|在不在|听到了|谢谢|再见|拜拜|早上好|晚上好|下午好|好的|好啊|嗯嗯|月汐|停|停下|别说了|继续)$/

/** True when the recognizer likely stopped mid-thought — wait longer before commit. */
export function looksIncompleteUtterance(text: string): boolean {
  const trimmed = text.trim()
  if (!trimmed) return false
  if (/[。？！?!…]$/.test(trimmed)) return false
  if (COMPLETE_SHORT_UTTERANCE.test(trimmed)) return false
  const compact = trimmed.replace(/\s+/g, '')
  if (INCOMPLETE_OPEN_GARBLE.test(compact)) return true
  if (INCOMPLETE_DESKTOP_WAITING.test(compact) && !/(?:协议|文档|文件)/.test(compact)) return true
  if (COMPLETE_OPEN_OBJECTS.test(compact)) return false
  if (COMPLETE_ACTION_ENDINGS.test(compact)) return false
  if (COMPLETE_TYPE_AFTER_LABEL.test(compact)) return false
  if (COMPLETE_TYPE_AFTER_WRITE.test(compact)) return false
  if (INCOMPLETE_BARE_COMMAND.test(compact)) return true
  if (INCOMPLETE_APP_PREFIX.test(compact)) return true
  if (INCOMPLETE_TRUNCATED_OPEN.test(compact)) return true
  if (INCOMPLETE_STARTERS.test(trimmed)) return true
  if (INCOMPLETE_ENDINGS.test(trimmed) && Array.from(trimmed).length <= 6) return true
  if (INCOMPLETE_OPENERS.test(trimmed) && Array.from(trimmed).length <= 6) return true
  if (COMPLETE_TAIL.test(trimmed)) return false
  if (INCOMPLETE_FIELD_WAIT.test(compact)) return true
  if (INCOMPLETE_TAIL.test(trimmed)) return true
  // Unpunctuated mid-thought: wait for a real stop or the rest of the clause.
  if (!/[。？！?!…]$/.test(trimmed) && Array.from(compact).length >= 8 && /(?:的|在|和|与)$/.test(compact)) {
    return true
  }
  // Short unpunctuated fragments like「你可以」are mid-command, not a turn.
  return Array.from(trimmed).length <= 3
}

const COMPANION_LEAD_IN_ONLY = new Set([
  '等一下',
  '稍等',
  '稍等我一下',
  '嗯，我在呢，稍等我一下',
  '我在呢，稍等我一下',
  '好，我帮你查一下',
  '好，我来执行',
  '好，我来打开',
  '好，我来播放',
  '好，我来输入',
  '好，我马上处理',
  '好，我来操作电脑',
  '好，我来发消息',
  '好，我用技能处理一下',
  '好，我先看一下技能约定',
  '好，我来生成图片',
  '好，我来生成视频',
  '嗯，',
  '嗯',
])

/** Host-injected “好，我来输入。” is not a task result. Returning to listen on it is silence. */
export function isCompanionLeadInOnly(text: string): boolean {
  const t = text.trim().replace(/[。.!！\s]+$/g, '')
  if (!t) return true
  if (COMPANION_LEAD_IN_ONLY.has(t)) return true
  return Array.from(t).length <= 6 && (t.includes('等') || t.includes('好'))
}

/** Exact host pad only — do not use the short 好/等 heuristic on user transcripts. */
export function looksLikeInjectedLeadIn(text: string): boolean {
  const t = text.trim().replace(/[。.!！\s]+$/g, '')
  return COMPANION_LEAD_IN_ONLY.has(t)
}

/** Drop machine self-reports. The user asked not to hear 「我做完了」. */
export function stripTaskDonePhrases(raw: string): string {
  const trimmed = raw
    .replace(/我已经做完了[。.!！]?\s*/g, '')
    .replace(/我做完了[。.!！]?\s*/g, '')
    .replace(/任务已完成[。.!！]?\s*/g, '')
    .trim()
  if (looksLikeOmniPersonaCaption(trimmed)) return ''
  if (looksLikeOmniUnavailable(trimmed)) return ''
  return trimmed
}

/** True while a computer tool is in flight (status like 「打开桌面文件中…」). */
export function companionToolsExecuting(chatStatus: string, activityStatus?: string): boolean {
  if (chatStatus !== 'streaming') return false
  const line = activityStatus?.trim() ?? ''
  if (!line) return false
  if (line === '等你确认…') return true
  return /中[….…]+$/.test(line)
}

export function companionExecutingSpeech(activity?: string): string {
  const cleaned = (activity ?? '').replace(/中[….…]+$/u, '').trim()
  return cleaned ? `${cleaned}。` : '正在执行。'
}

export const COMPANION_BROWSER_MCP_SPEECH = '浏览器没就绪。请到设置里安装 Playwright MCP，这次没有点到页面。'

export function companionBrowserUnreadySpeech(text?: string): string {
  return /BROWSER_MCP_NOT_READY|Playwright MCP 未就绪/.test(text ?? '') ? COMPANION_BROWSER_MCP_SPEECH : ''
}

export function companionCannotExecuteSpeech(reason?: string): string {
  const browser = companionBrowserUnreadySpeech(reason)
  if (browser) return browser
  const why = (reason ?? '')
    .replace(/^无法执行[。.]?\s*/u, '')
    .replace(/^出错了[，,]\s*/u, '')
    .trim()
  return why ? `无法执行。${why}` : '无法执行。'
}

export function companionTaskCompleteSpeech(summary?: string): string {
  const line = stripTaskDonePhrases(summary ?? '').trim()
  if (!line) return '好。'
  if (/[。！？.!?]$/.test(line)) return line
  return `${line}。`
}

/** Tool-loop closeout: empty/lead-in is process, not “完成了”. */
export function companionToolCloseoutSpeech(summary?: string): string {
  const browser = companionBrowserUnreadySpeech(summary)
  if (browser) return browser
  const line = stripTaskDonePhrases(summary ?? '').trim()
  if (!line) return '还在处理。'
  return companionTaskCompleteSpeech(line)
}

/**
 * MiniCPM-o SSE (and other token streams) often arrive as 1–2 character
 * deltas. The subtitle must keep the whole utterance, never the last crumb.
 *
 * ChatBridge turns should call `companionCaptionFromStream` instead — that
 * buffer is already cumulative and must not be re-accumulated here.
 */
export function accumulateSpeakableCaption(previous: string, incoming: string): string {
  const next = stripTaskDonePhrases(incoming)
  const prev = stripTaskDonePhrases(previous)
  if (!next) return prev
  if (!prev) return next
  if (next === prev) return next
  if (next.startsWith(prev)) return next
  if (prev.startsWith(next)) return prev
  if (next.includes(prev) && next.length > prev.length) return next
  if (prev.includes(next)) return prev
  return `${prev}${next}`
}

/** Live chat caption: strip machine phrases and drop nudge-loop replays. */
export function companionCaptionFromStream(assistantText: string): string {
  return collapseAdjacentRepeatedClauses(collapseRepeatedCaptionBlocks(stripTaskDonePhrases(assistantText)))
}

function clauseKey(text: string): string {
  const compact = compactSpeech(text)
  if (!compact) return ''
  if (/帮你查一下|我来查一下|马上帮你查|马上查一下/.test(compact)) return 'lookup-leadin'
  return compact
}

/** Collapse consecutive identical clauses, including「好，我帮你查一下」variants. */
export function collapseAdjacentRepeatedClauses(text: string): string {
  const parts = text.split(/(?<=[。！？!?])/)
  if (parts.length <= 1) return text
  const out: string[] = []
  let lastKey = ''
  for (const part of parts) {
    const key = clauseKey(part)
    if (key && key === lastKey) continue
    out.push(part)
    if (key) lastKey = key
  }
  return out.join('')
}

/** Collapse exact consecutive repeats (companion nudge loops replay whole answers). */
export function collapseRepeatedCaptionBlocks(text: string): string {
  let out = text.trim()
  if (!out) return ''
  for (;;) {
    let collapsed = false
    for (let repeats = 2; repeats <= 12; repeats++) {
      if (out.length % repeats !== 0) continue
      const unit = out.length / repeats
      if (unit < 8) continue
      const block = out.slice(0, unit)
      if (block.repeat(repeats) === out) {
        out = block
        collapsed = true
        break
      }
    }
    if (!collapsed) break
  }
  return out
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
  /** True while TTS plays, the model thinks, or tools run — mic captions must not commit. */
  assistantBusy?: boolean
  /** True in the post-playback room-echo window. */
  echoGuardActive?: boolean
}): boolean {
  if (input.assistantBusy || input.state === 'speaking' || input.state === 'thinking') return false
  if (!input.text.trim()) return false
  if (looksLikeInjectedLeadIn(input.text)) return false
  if (looksLikeOmniPersonaCaption(input.text)) return false
  if (looksLikeOmniUnavailable(input.text)) return false
  if (looksLikePlaybackEcho(input.text, input.lastSpoken)) return false
  if (input.lastAssistant && looksLikePlaybackEcho(input.text, input.lastAssistant)) return false
  if (input.echoGuardActive && compactSpeech(input.text).length < 6 && looksIncompleteUtterance(input.text)) return false
  return true
}

/** Real user speech during a busy turn — queue for after this reply, do not drop. */
export function shouldQueueBusyUserTranscript(input: {
  state: 'idle' | 'listening' | 'thinking' | 'speaking'
  text: string
  lastSpoken: string
  lastAssistant: string
  assistantBusy?: boolean
}): boolean {
  if (!(input.assistantBusy || input.state === 'speaking' || input.state === 'thinking')) return false
  if (!input.text.trim()) return false
  if (looksLikeOmniPersonaCaption(input.text)) return false
  if (looksLikeOmniUnavailable(input.text)) return false
  if (looksLikePlaybackEcho(input.text, input.lastSpoken)) return false
  if (input.lastAssistant && looksLikePlaybackEcho(input.text, input.lastAssistant)) return false
  return looksLikeBargeInSpeech(input.text, input.lastSpoken)
}

/** Settings/clone labels must never become a dialogue round. */
export function looksLikeOmniPersonaCaption(text: string): boolean {
  const compact = text.replace(/\s+/g, '')
  return compact.startsWith('人生：') || compact.startsWith('人生:') || compact.includes('月汐/人生')
}

/** MiniCPM-o missing-runtime notices are install hints, never a spoken turn. */
export function looksLikeOmniUnavailable(text: string): boolean {
  const compact = text.replace(/\s+/g, '')
  return /OMNI_UNAVAILABLE|OMNI-00[12]|llama-omni-server|MiniCPM-o启动失败|请先在设置里下载MiniCPM-o|未找到llama-omni-server|推理进程未能展开/.test(compact)
}

export function isOmniUnavailableNotice(code?: string, message?: string): boolean {
  if (code === 'OMNI_UNAVAILABLE' || code === 'OMNI-001' || code === 'OMNI-002') return true
  return !!message && looksLikeOmniUnavailable(message)
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
  text = correctAsrText(text)
  text = repairOpenCommandTranscript(text)
  return stripOralFillers(text)
}

/** Levenshtein distance for short zh crumbs — bounded so echo checks stay cheap. */
function shortSpeechEditDistance(a: string, b: string): number {
  const rows = a.length + 1
  const cols = b.length + 1
  const matrix = Array.from({ length: rows }, () => Array<number>(cols).fill(0))
  for (let i = 0; i < rows; i++) matrix[i]![0] = i
  for (let j = 0; j < cols; j++) matrix[0]![j] = j
  for (let i = 1; i < rows; i++) {
    for (let j = 1; j < cols; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1
      matrix[i]![j] = Math.min(matrix[i - 1]![j]! + 1, matrix[i]![j - 1]! + 1, matrix[i - 1]![j - 1]! + cost)
    }
  }
  return matrix[a.length]![b.length]!
}

/** Fuzzy match for 2–6 char misheard TTS tails like 「谢你见」≈「谢谢你」. */
function looksLikeShortPlaybackEcho(heard: string, spoken: string): boolean {
  if (heard.length > 6 || heard.length < 2 || spoken.length < 2) return false
  const minRun = Math.min(heard.length, Math.max(3, Math.ceil(heard.length * 0.6)))
  for (let run = heard.length; run >= minRun; run--) {
    for (let i = 0; i <= heard.length - run; i++) {
      if (spoken.includes(heard.slice(i, i + run))) return true
    }
  }
  if (heard.length > 4) return false
  for (let i = 0; i <= spoken.length - 2; i++) {
    for (let len = Math.max(2, heard.length - 1); len <= Math.min(heard.length + 1, spoken.length - i); len++) {
      const slice = spoken.slice(i, i + len)
      if (shortSpeechEditDistance(heard, slice) <= 1) return true
    }
  }
  return false
}

/**
 * True when the recognizer almost certainly heard our own TTS playback
 * (speaker → microphone loop) rather than a new user utterance.
 */
export function looksLikePlaybackEcho(heard: string, spoken: string): boolean {
  const a = compactSpeech(heard)
  const b = compactSpeech(spoken)
  if (!a || !b) return false
  // Echo is almost always the last line bleeding back, not a crumb from rounds ago.
  const recent = b.slice(-Math.max(a.length + 48, 160))
  // Laptop speaker bleed often comes back as a short fragment of the last line.
  if (a.length >= 2 && recent.includes(a)) return true
  if (recent.length >= 4 && recent.length <= a.length + 2 && a.includes(recent)) return true
  const window = Math.min(6, a.length)
  if (window >= 4) {
    for (let i = 0; i <= a.length - window; i++) {
      if (recent.includes(a.slice(i, i + window))) return true
    }
  }
  return looksLikeShortPlaybackEcho(a, recent)
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
  if (isFirst) {
    const comma = /^(?:[\s\S]*?[，,])/.exec(pending)
    if (comma) {
      const lead = comma[0]
      const body = lead.replace(/[，,\s]/g, '')
      if (Array.from(body).length >= 8 && !looksLikeIncompleteFieldWait(lead)) {
        return { text: lead, consumed: lead.length }
      }
    }
  }
  if (!force) return null
  const trimmed = pending.trim()
  if (isFirst && COMPLETE_SHORT_UTTERANCE.test(trimmed)) {
    return { text: pending, consumed: pending.length }
  }
  const minChars = isFirst ? FIRST_FORCE_SPEAK_CHARS : FOLLOW_SPEAK_CHARS
  if (Array.from(pending).length < minChars) return null
  return { text: pending, consumed: pending.length }
}
