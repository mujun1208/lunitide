export const PLACEHOLDER_CHAT_TITLES = new Set(['新对话', 'New chat'])
export const COMPANION_CHAT_TITLES = new Set(['月伴对话', 'Companion talk'])

export function isPlaceholderChatTitle(title: string): boolean {
  return PLACEHOLDER_CHAT_TITLES.has(title.trim())
}

/** The persistent 月伴 singleton keeps this stable title so it is always
 *  re-findable and never splinters into per-visit dialogues. */
export function isCompanionChatTitle(title: string): boolean {
  return COMPANION_CHAT_TITLES.has(title.trim())
}

/**
 * Only 新对话/New chat placeholders get auto-renamed to the first sentence.
 * 月伴对话 is deliberately excluded now that companion talk is one long-lived
 * singleton dialogue — renaming it to a first utterance would hide the single
 * home the user returns to and break re-discovery by title.
 */
export function isRenameableChatTitle(title: string): boolean {
  return PLACEHOLDER_CHAT_TITLES.has(title.trim())
}

/** Sidebar/search label. Bound colleague sessions used to store「同事 · 专家」. */
export function displaySessionTitle(title: string): string {
  const trimmed = title.trim()
  if (trimmed.startsWith('同事 · ')) return trimmed.slice('同事 · '.length).trim() || trimmed
  if (trimmed.startsWith('同事·')) return trimmed.slice('同事·'.length).trim() || trimmed
  return trimmed
}

/** Bound colleague threads belong in 同事聊天, not the ordinary 对话 list. */
export function isColleagueChatTitle(title: string): boolean {
  const trimmed = title.trim()
  if (trimmed === '同事对话') return true
  return trimmed.startsWith('同事 · ') || trimmed.startsWith('同事·')
}

export function titleFromFirstTurn(text: string, max = 80): string {
  const t = text.replace(/\s+/g, ' ').trim()
  if (!t) return ''
  const runes = [...t]
  return runes.length <= max ? t : runes.slice(0, max).join('')
}