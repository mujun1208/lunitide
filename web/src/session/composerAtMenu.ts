import { attachmentToken, messageToken } from './composerParser'
import { insertMention } from '../people/peopleMentions'
import type {MessageDTO} from '../generated/bridge'

export type ComposerAtKind = 'attachment' | 'expert' | 'member' | 'message' | 'artifact'

export type ComposerAtItem = {
  kind: ComposerAtKind
  id: string
  label: string
}

export function atMenuPlaceholder(kind: ComposerAtKind): string {
  if (kind === 'attachment') return '附件'
  if (kind === 'expert') return '已挂载专家'
  if (kind === 'message') return '对话消息'
  if (kind === 'artifact') return '本会话产物（引用来源消息）'
  return '同事'
}

export function insertComposerAtPick(draft: string, item: ComposerAtItem): string {
  if (item.kind === 'attachment') {
    return draft.replace(/@[^\s]*$/, attachmentToken(item.id, item.label) + ' ')
  }
  if (item.kind === 'message' || item.kind === 'artifact') {
    return draft.replace(/@[^\s]*$/, messageToken(item.id, item.label) + ' ')
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
export function sessionMessageAtItems(messages:readonly MessageDTO[],sessionId:string):ComposerAtItem[]{
 const recent=messages.filter(m=>m.sessionId===sessionId&&(m.role==='user'||m.role==='assistant')).slice(-100).reverse()
 return recent.flatMap(m=>[
  {kind:'message' as const,id:m.id,label:(m.role==='user'?'我：':'月汐：')+Array.from(m.text.trim().replace(/\s+/g,' ')).slice(0,80).join('')},
  ...(m.artifacts??[]).map(artifact=>({kind:'artifact' as const,id:m.id,label:`产物：${artifact.path}`})),
 ])
}
