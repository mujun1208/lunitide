export type TurnMention = { kind: 'expert' | 'skill' | 'member'; id: string; name: string }

const isSpace = (ch: string) => /\s/.test(ch)

export function parseTurnMentions(text: string): TurnMention[] {
  const out: TurnMention[] = []
  const seen = new Set<string>()
  const add = (kind: TurnMention['kind'], id: string, name: string) => {
    const next: TurnMention = { kind, id: id.trim(), name: name.trim() }
    if (!next.kind || (!next.id && !next.name)) return
    const key = `${next.kind}\0${next.id}\0${next.name}`
    if (seen.has(key)) return
    seen.add(key)
    out.push(next)
  }
  scanPrefixedRefs(text, '[引用专家 ', 'expert', add)
  scanPrefixedRefs(text, '[引用技能 ', 'skill', add)
  for (const name of scanAtNames(text)) add('member', '', name)
  return out
}

function scanPrefixedRefs(text: string, prefix: string, kind: TurnMention['kind'], add: (kind: TurnMention['kind'], id: string, name: string) => void) {
  let rest = text
  for (;;) {
    const i = rest.indexOf(prefix)
    if (i < 0) return
    rest = rest.slice(i + prefix.length)
    const bar = rest.indexOf('|')
    const end = rest.indexOf(']')
    if (bar < 0 || end < 0 || bar >= end) continue
    add(kind, rest.slice(bar + 1, end), rest.slice(0, bar))
    rest = rest.slice(end + 1)
  }
}

function scanAtNames(text: string): string[] {
  const names: string[] = []
  const runes = [...text]
  for (let i = 0; i < runes.length; i++) {
    if (runes[i] !== '@') continue
    if (i > 0 && !isSpace(runes[i - 1]!)) continue
    let j = i + 1
    while (j < runes.length && !isSpace(runes[j]!) && runes[j] !== '@') j++
    if (j === i + 1) continue
    names.push(runes.slice(i + 1, j).join(''))
    i = j - 1
  }
  return names
}

export function extractExpertRefNames(text: string): string[] {
  const names: string[] = []
  const seen = new Set<string>()
  for (const m of parseTurnMentions(text)) {
    if (m.kind !== 'expert' || !m.name || seen.has(m.name)) continue
    seen.add(m.name)
    names.push(m.name)
  }
  return names
}
