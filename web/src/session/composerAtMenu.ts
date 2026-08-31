import { attachmentToken } from './composerParser'
import { insertMention } from '../people/peopleMentions'

export type ComposerAtKind = 'attachment' | 'expert' | 'member'

export type ComposerAtItem = {
  kind: ComposerAtKind
  id: string
  label: string
}

export function atMenuPlaceholder(kind: ComposerAtKind): string {
  if (kind === 'attachment') return '附件'
  if (kind === 'expert') return '已挂载专家'
  return '同事'
}

export function insertComposerAtPick(draft: string, item: ComposerAtItem): string {
  if (item.kind === 'attachment') {
    return draft.replace(/@[^\s]*$/, attachmentToken(item.id, item.label) + ' ')
  }
  if (item.kind === 'expert') {
    return draft.replace(/@[^\s]*$/, `[引用专家 ${item.label}|${item.id}] `)
  }
  return insertMention(draft, item.label)
}

export function filterComposerAtItems(items: ComposerAtItem[], query: string): ComposerAtItem[] {
  const q = query.trim().toLowerCase()
  return items.filter(item => !q || item.label.toLowerCase().includes(q) || item.id.toLowerCase().includes(q))
}

export function atQuery(draft: string): string {
  return (/@[^\s]*$/.exec(draft)?.[0].slice(1) ?? '')
}