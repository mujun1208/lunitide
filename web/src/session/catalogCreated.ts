export type CatalogKind = 'skill' | 'expert' | 'plugin'

export function parseCreatedArtifactId(summary: string): string | undefined {
  const pack = /id=(pack-[A-Za-z0-9][-A-Za-z0-9._]{0,62})/.exec(summary)
  if (pack?.[1]) return pack[1]
  const ulid = /id=([0-9A-HJKMNP-TV-Z]{26})/i.exec(summary)
  if (ulid?.[1]) return ulid[1]
  const expert = /"expertId"\s*:\s*"([0-9A-HJKMNP-TV-Z]{26})"/i.exec(summary)
  return expert?.[1]
}

export function excerptFromMessages(items: Array<{ role: string; content?: string; text?: string }>, limit = 1800): string {
  const lines: string[] = []
  for (const item of items) {
    const body = (item.content ?? item.text ?? '').trim()
    if (!body) continue
    const who = item.role === 'assistant' ? '助手' : item.role === 'user' ? '用户' : item.role
    lines.push(`【${who}】\n${body}`)
  }
  const joined = lines.join('\n\n').trim()
  if (joined.length <= limit) return joined
  return joined.slice(joined.length - limit)
}

export function saveAsSkillPrompt(excerpt: string): string {
  return `把刚才这段对话整理成一个可复用技能。先列出六段画像让我确认，再调用 skill.create。不要自动发布。\n\n【对话摘录】\n${excerpt.trim() || '（无摘录，请根据本会话上下文整理）'}`
}
