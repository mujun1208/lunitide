export const PLACEHOLDER_CHAT_TITLES = new Set(['新对话', 'New chat'])
export const COMPANION_CHAT_TITLES = new Set(['月伴对话', 'Companion talk'])

export function isPlaceholderChatTitle(title: string): boolean {
  return PLACEHOLDER_CHAT_TITLES.has(title.trim())
}

export function isRenameableChatTitle(title: string): boolean {
  const t = title.trim()
  return PLACEHOLDER_CHAT_TITLES.has(t) || COMPANION_CHAT_TITLES.has(t)
}

export function titleFromFirstTurn(text: string, max = 80): string {
  const t = text.replace(/\s+/g, ' ').trim()
  if (!t) return ''
  const runes = [...t]
  return runes.length <= max ? t : runes.slice(0, max).join('')
}