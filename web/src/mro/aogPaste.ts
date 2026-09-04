export type AogDraft = { tailNo: string; pn: string; qty: string; note: string }

const TAIL = /\b([A-Z]-\d{1,8})\b/i
const PN = /(?:^|[\s：:])(?:pn|件号)\s*[:：]?\s*([A-Z0-9][A-Z0-9._/-]{1,31})/i
const QTY = /(?:qty|数量)\s*[:：]?\s*(\d+(?:\.\d+)?)/i

// parseAogPaste mirrors the Go engine extractor so the intake dialog can
// preview tail/PN/qty before the human confirms a draft write.
export function parseAogPaste(text: string): AogDraft {
  const draft: AogDraft = { tailNo: '', pn: '', qty: '', note: '' }
  const notes: string[] = []
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    const lower = line.toLowerCase()
    if (lower.startsWith('tail:') || line.startsWith('机尾:')) {
      draft.tailNo = line.slice(line.indexOf(':') + 1).trim()
    } else if (lower.startsWith('pn:') || line.startsWith('件号:')) {
      draft.pn = line.slice(line.indexOf(':') + 1).trim()
    } else if (lower.startsWith('qty:') || line.startsWith('数量:')) {
      draft.qty = line.slice(line.indexOf(':') + 1).trim()
    } else {
      notes.push(line)
    }
  }
  const blob = text.trim()
  if (!draft.tailNo) {
    const m = blob.match(TAIL)
    if (m?.[1]) draft.tailNo = m[1].toUpperCase()
  }
  if (!draft.pn) {
    const m = blob.match(PN)
    if (m?.[1]) draft.pn = m[1]
  }
  if (!draft.qty) {
    const m = blob.match(QTY)
    if (m?.[1]) draft.qty = m[1]
  }
  draft.note = notes.join(' ') || blob.split(/\s+/).filter(Boolean).join(' ')
  return draft
}
