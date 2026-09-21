export type NotesTable = { caption: string; headers: string[]; rows: string[][] }

export type NotesDiagram = { caption: string; code: string }

export type NotesSection = {
  kind: 'background' | 'topic' | 'decision' | 'open' | 'summary'
  heading: string
  paragraphs: string[]
  bullets: string[]
  /** The thinking record the backend writes under `### 思考`, kept apart from
   * the transcript facts in `bullets` so the two can be styled differently. */
  reasoning: string[]
  table?: NotesTable
  diagram?: NotesDiagram
}

export type NotesAction = { owner: string; task: string; due: string }

export type NotesDoc = {
  attendees: string[]
  sections: NotesSection[]
  actions: NotesAction[]
}

const ATTENDEE_LINE = /^(?:参会|与会|出席)[人]?[：:]\s*(.+)$/m
const HEADING = /^##\s+(.+)$/
const SUBHEADING = /^###\s+(.+)$/
const BULLET = /^(?:[-*•□]|[0-9]+[.)])\s+(.+)$/
const NUMBERED_CN = /^(?:[一二三四五六七八九十]+、|\d+[、.．]\s*)(.+)$/
const LABELED = /^(背景|讨论要点|结论|决议|待办|未决|待确认)[：:]\s*(.*)$/

function looksLikeRawJSON(s: string): boolean {
  const t = s.trim()
  if (t.startsWith('{') && t.endsWith('}')) return true
  if (t.startsWith('[') && t.endsWith(']')) return true
  if (/^\{[\s\S]*"title"\s*:/.test(t)) return true
  if (/^\{[\s\S]*"topics"\s*:/.test(t)) return true
  return false
}

export function parseMeetingNotesDoc(summary: string, actions: string): NotesDoc {
  const attendees: string[] = []
  let text = (summary || '').replace(/\r\n/g, '\n').trim()
  if (looksLikeRawJSON(text)) {
    text = '（会议纪要解析中，数据格式异常，请稍后刷新或手动编辑。）'
  }
  const att = text.match(ATTENDEE_LINE)
  if (att?.[1]) {
    attendees.push(...splitNames(att[1]))
    text = text.replace(att[0], '').trim()
  }
  const sections = text ? splitSections(text) : []
  return { attendees, sections, actions: parseActionLines(actions) }
}

export function parseActionLines(raw: string): NotesAction[] {
  const out: NotesAction[] = []
  for (const line of (raw || '').replace(/\r\n/g, '\n').split('\n')) {
    const item = parseActionLine(line)
    if (item) out.push(item)
  }
  return out
}

function parseActionLine(raw: string): NotesAction | undefined {
  let line = raw.trim().replace(/^[□■☐☑✓✔]\s*/, '')
  line = line.replace(/^[-*•]\s*/, '')
  if (!line) return undefined
  let due = ''
  const dueMatch = line.match(/[（(]截止[：:]\s*([^）)]+)[）)]\s*$/)
  if (dueMatch) {
    due = dueMatch[1].trim()
    line = line.slice(0, dueMatch.index).trim()
  }
  const owned = line.match(/^(.{1,16}?)[：:]\s*(.+)$/)
  if (owned && !/[。；;]/.test(owned[1]!) && !LABELED.test(`${owned[1]}：`)) {
    return { owner: owned[1]!.trim(), task: owned[2]!.trim(), due }
  }
  return { owner: '', task: line, due }
}

function splitNames(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of raw.split(/[、，,;；/／|]+/)) {
    const name = part.trim()
    if (!name || seen.has(name)) continue
    seen.add(name)
    out.push(name)
  }
  return out
}

function splitSections(text: string): NotesSection[] {
  if (/^##\s+/m.test(text)) return splitByHeading(text)
  const labeled = splitLabeled(text)
  if (labeled.length > 1) return labeled
  const numbered = splitNumbered(text)
  if (numbered.length > 1) return numbered
  return [sectionFromBody('summary', '会议摘要', text)]
}

function splitByHeading(text: string): NotesSection[] {
  const lines = text.split('\n')
  const sections: NotesSection[] = []
  let heading = ''
  let body: string[] = []
  const flush = () => {
    const raw = body.join('\n').trim()
    if (!heading && !raw) return
    sections.push(sectionFromBody(...classifyHeading(heading || '会议摘要'), raw))
  }
  for (const line of lines) {
    const match = line.match(HEADING)
    if (match) {
      flush()
      heading = match[1]!.trim()
      body = []
      continue
    }
    body.push(line)
  }
  flush()
  return sections
}

function splitLabeled(text: string): NotesSection[] {
  const lines = text.split('\n')
  const sections: NotesSection[] = []
  let heading = ''
  let body: string[] = []
  const flush = () => {
    const raw = body.join('\n').trim()
    if (!heading && !raw) return
    sections.push(sectionFromBody(...classifyHeading(heading || '会议摘要'), raw))
  }
  for (const line of lines) {
    const match = line.match(LABELED)
    if (match) {
      flush()
      heading = match[1]!
      body = match[2] ? [match[2]] : []
      continue
    }
    body.push(line)
  }
  flush()
  return sections
}

function splitNumbered(text: string): NotesSection[] {
  const lines = text.split('\n')
  const sections: NotesSection[] = []
  let heading = ''
  let body: string[] = []
  let saw = 0
  const flush = () => {
    const raw = body.join('\n').trim()
    if (!heading && !raw) return
    sections.push(sectionFromBody(heading ? 'topic' : 'summary', heading || '会议摘要', raw))
  }
  for (const line of lines) {
    const match = line.match(NUMBERED_CN)
    if (match && line.trim().length <= 40) {
      flush()
      heading = match[1]!.trim()
      body = []
      saw += 1
      continue
    }
    body.push(line)
  }
  flush()
  return saw >= 2 ? sections : [sectionFromBody('summary', '会议摘要', text)]
}

function classifyHeading(raw: string): [NotesSection['kind'], string] {
  const heading = raw.replace(/^议题[：:]\s*/, '').trim()
  if (/^背景/.test(raw)) return ['background', heading || '背景']
  if (/^(决议|结论)/.test(raw)) return ['decision', heading || '决议']
  if (/^(未决|待确认)/.test(raw)) return ['open', heading || '未决']
  if (/^会议摘要$/.test(raw)) return ['summary', heading]
  return ['topic', heading || '讨论要点']
}

const REASONING_HEADING = /^思考/
const FENCE = /^```(\w*)\s*$/

function sectionFromBody(kind: NotesSection['kind'], heading: string, raw: string): NotesSection {
  const lines = raw.replace(/\r\n/g, '\n').split('\n')
  const paragraphs: string[] = []
  const bullets: string[] = []
  const reasoning: string[] = []
  let table: NotesTable | undefined
  let diagram: NotesDiagram | undefined
  let caption = ''
  let inReasoning = false
  let fence: string[] | undefined
  let fenceLang = ''
  let para: string[] = []
  const flushPara = () => {
    const text = para.join('\n').trim()
    para = []
    if (!text) return
    if (inReasoning) reasoning.push(text)
    else paragraphs.push(text)
  }
  let tableLines: string[] = []
  const flushTable = () => {
    if (tableLines.length === 0) return
    const parsed = parseMarkdownTable(tableLines, caption)
    if (parsed) table = parsed
    else para.push(...tableLines)
    tableLines = []
    caption = ''
  }
  for (const line of lines) {
    const trimmed = line.trim()
    const fenceEdge = trimmed.match(FENCE)
    if (fenceEdge) {
      if (fence) {
        const code = fence.join('\n').trim()
        if (fenceLang === 'mermaid' && code) diagram = { caption, code }
        else if (code) paragraphs.push(code)
        fence = undefined
        caption = ''
        continue
      }
      flushTable()
      flushPara()
      fence = []
      fenceLang = fenceEdge[1]!.toLowerCase()
      continue
    }
    if (fence) {
      fence.push(line)
      continue
    }
    const sub = trimmed.match(SUBHEADING)
    if (sub) {
      flushTable()
      flushPara()
      const label = sub[1]!.trim()
      // `### 思考` opens the thinking record; any other subheading captions the
      // table or diagram that follows it and closes the thinking record.
      inReasoning = REASONING_HEADING.test(label)
      caption = inReasoning ? '' : label
      continue
    }
    if (trimmed.startsWith('|')) {
      flushPara()
      tableLines.push(trimmed)
      continue
    }
    if (tableLines.length) flushTable()
    const bullet = trimmed.match(BULLET)
    if (bullet) {
      flushPara()
      if (inReasoning) reasoning.push(bullet[1]!.trim())
      else bullets.push(bullet[1]!.trim())
      continue
    }
    if (!trimmed) {
      flushPara()
      continue
    }
    para.push(trimmed)
  }
  if (fence?.length) paragraphs.push(fence.join('\n').trim())
  flushTable()
  flushPara()
  return { kind, heading, paragraphs, bullets, reasoning, table, diagram }
}

function parseMarkdownTable(lines: string[], caption: string): NotesTable | undefined {
  if (lines.length < 2) return undefined
  const headers = splitPipeRow(lines[0]!)
  if (headers.length === 0) return undefined
  let rows = lines.slice(1)
  if (rows[0] && isAlignRow(rows[0])) rows = rows.slice(1)
  return {
    caption,
    headers,
    rows: rows.map(row => {
      const cells = splitPipeRow(row)
      return headers.map((_, i) => cells[i] ?? '')
    }),
  }
}

function splitPipeRow(line: string): string[] {
  return line.replace(/^\||\|$/g, '').split('|').map(cell => cell.trim().replace(/\\\|/g, '|'))
}

function isAlignRow(line: string): boolean {
  return splitPipeRow(line).every(cell => /^:?-{3,}:?$/.test(cell.replace(/\s/g, '')) || cell === '---')
}

const AVATAR_TONES = [
  'color-mix(in srgb, var(--tide1) 28%, var(--bg3))',
  'color-mix(in srgb, var(--tide2) 28%, var(--bg3))',
  'color-mix(in srgb, var(--tide3) 28%, var(--bg3))',
  'color-mix(in srgb, var(--glow) 24%, var(--bg3))',
]

export function attendeeTone(name: string): string {
  let hash = 0
  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) >>> 0
  return AVATAR_TONES[hash % AVATAR_TONES.length]!
}

export function attendeeInitial(name: string): string {
  const chars = Array.from(name.trim())
  return chars[0] || '?'
}
