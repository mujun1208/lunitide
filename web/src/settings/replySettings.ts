export type ReplyStyle = 'default' | 'assistant' | 'support' | 'teacher' | 'npc'
export type StructuredTemplate = 'off' | 'event' | 'form' | 'kv'

export const DEFAULT_REPLY_STYLE: ReplyStyle = 'default'
export const DEFAULT_STRUCTURED_TEMPLATE: StructuredTemplate = 'off'

export const REPLY_STYLES: readonly ReplyStyle[] = ['default', 'assistant', 'support', 'teacher', 'npc']
export const STRUCTURED_TEMPLATES: readonly StructuredTemplate[] = ['off', 'event', 'form', 'kv']

export const REPLY_STYLE_OPTIONS: { value: ReplyStyle; label: string; desc: string }[] = [
  { value: 'default', label: '默认', desc: '月汐本色，不叠口吻' },
  { value: 'assistant', label: '助手', desc: '先结论，再依据' },
  { value: 'support', label: '客服', desc: '先确认诉求再给步骤' },
  { value: 'teacher', label: '老师', desc: '讲解加例子' },
  { value: 'npc', label: '轻角色', desc: '短句沉浸，仍是月汐' },
]

export const STRUCTURED_TEMPLATE_OPTIONS: { value: StructuredTemplate; label: string; desc: string }[] = [
  { value: 'off', label: '关闭', desc: '按普通对话回答' },
  { value: 'event', label: '提取事件', desc: '日程 JSON' },
  { value: 'form', label: '生成表单', desc: '标签+字段' },
  { value: 'kv', label: '键值摘要', desc: '成对总结' },
]

function isReplyStyle(v: unknown): v is ReplyStyle {
  return typeof v === 'string' && (REPLY_STYLES as readonly string[]).includes(v)
}

function isStructuredTemplate(v: unknown): v is StructuredTemplate {
  return typeof v === 'string' && (STRUCTURED_TEMPLATES as readonly string[]).includes(v)
}

const GENERAL_KEY = 'lunitide:general'
const GENERAL_EVENT = 'lunitide:general'

export function loadReplySettings(): { replyStyle: ReplyStyle; structuredTemplate: StructuredTemplate } {
  try {
    const raw = localStorage.getItem(GENERAL_KEY)
    if (raw) {
      const g = JSON.parse(raw) as { replyStyle?: unknown; structuredTemplate?: unknown }
      return {
        replyStyle: isReplyStyle(g.replyStyle) ? g.replyStyle : DEFAULT_REPLY_STYLE,
        structuredTemplate: isStructuredTemplate(g.structuredTemplate) ? g.structuredTemplate : DEFAULT_STRUCTURED_TEMPLATE,
      }
    }
  } catch { /* ignore */ }
  return { replyStyle: DEFAULT_REPLY_STYLE, structuredTemplate: DEFAULT_STRUCTURED_TEMPLATE }
}

export function saveReplySettings(patch: { replyStyle?: ReplyStyle; structuredTemplate?: StructuredTemplate }): void {
  const current = loadReplySettings()
  const next = { ...current, ...patch }
  try {
    const raw = localStorage.getItem(GENERAL_KEY)
    const base = raw ? JSON.parse(raw) as Record<string, unknown> : {}
    localStorage.setItem(GENERAL_KEY, JSON.stringify({ ...base, ...next }))
    window.dispatchEvent(new Event(GENERAL_EVENT))
  } catch { /* ignore */ }
}

export function subscribeReplySettings(listener: () => void): () => void {
  window.addEventListener(GENERAL_EVENT, listener)
  window.addEventListener('storage', listener)
  return () => {
    window.removeEventListener(GENERAL_EVENT, listener)
    window.removeEventListener('storage', listener)
  }
}

/** Fields to spread into chat.start. Companion turns stay lean. */
export function chatStartReplyFields(companion: boolean): { replyStyle?: ReplyStyle; structuredTemplate?: StructuredTemplate } {
  if (companion) return {}
  const s = loadReplySettings()
  const out: { replyStyle?: ReplyStyle; structuredTemplate?: StructuredTemplate } = {}
  if (s.replyStyle !== 'default') out.replyStyle = s.replyStyle
  if (s.structuredTemplate !== 'off') out.structuredTemplate = s.structuredTemplate
  return out
}

export function replyStyleLabel(value: ReplyStyle): string {
  return REPLY_STYLE_OPTIONS.find(o => o.value === value)?.label ?? '默认'
}

export function structuredTemplateLabel(value: StructuredTemplate): string {
  return STRUCTURED_TEMPLATE_OPTIONS.find(o => o.value === value)?.label ?? '关闭'
}
