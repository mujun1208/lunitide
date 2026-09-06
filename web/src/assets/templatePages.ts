import type { TemplateBridge } from '../bridge/client'
import type { TemplateListPayload, TemplateListResult } from '../generated/bridge'

// Selection lists must traverse the same server filter, including later pages.
// A repeated cursor is an error, rather than an unbounded request loop.
export async function listTemplatePages(templates: TemplateBridge, filter: TemplateListPayload): Promise<TemplateListResult> {
  const items: TemplateListResult['items'] = []
  const cursors = new Set<string>()
  let cursor: string | undefined
  do {
    const page = await templates.list({ ...filter, ...(cursor ? { cursor } : {}) })
    items.push(...page.items)
    cursor = page.nextCursor || undefined
    if (cursor && cursors.has(cursor)) throw new Error('模版分页状态异常，请刷新重试')
    if (cursor) cursors.add(cursor)
  } while (cursor)
  return { items }
}
