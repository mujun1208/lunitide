export type MroSessionScenario = 'manual' | 'fault' | 'checklist' | 'due' | 'tools' | 'parts' | 'plan'

export type MroSessionContext = {
  tailNo: string
  asOf: string
  manualIds: string[]
  pack: 'mro.v1'
  scenario?: MroSessionScenario
  expertCatalogId?: string
  toolId?: string
  lot?: string
  pn?: string
  window?: string
}

export const MRO_CONTEXT_KEY = 'mroContext'

const ULID = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/
const AS_OF = /^\d{4}-\d{2}-\d{2}$/
const SCENARIOS = new Set<MroSessionScenario>(['manual', 'fault', 'checklist', 'due', 'tools', 'parts', 'plan'])

export function parseMroContext(raw: unknown): MroSessionContext | undefined {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined
  const row = raw as Record<string, unknown>
  const tailNo = String(row.tailNo ?? '').trim()
  const asOf = String(row.asOf ?? '').trim()
  if (tailNo.length < 1 || tailNo.length > 32) return undefined
  if (!AS_OF.test(asOf)) return undefined
  const manualIds = Array.isArray(row.manualIds)
    ? row.manualIds.filter((id): id is string => typeof id === 'string' && ULID.test(id))
    : []
  const scenario = SCENARIOS.has(row.scenario as MroSessionScenario) ? row.scenario as MroSessionScenario : 'manual'
  const extra = (key: string) => {
    const v = String(row[key] ?? '').trim()
    return v || undefined
  }
  const ctx: MroSessionContext = { tailNo, asOf, manualIds, pack: 'mro.v1', scenario }
  const expertCatalogId = extra('expertCatalogId')
  const toolId = extra('toolId')
  const lot = extra('lot')
  const pn = extra('pn')
  const windowId = extra('window')
  if (expertCatalogId) ctx.expertCatalogId = expertCatalogId
  if (toolId) ctx.toolId = toolId
  if (lot) ctx.lot = lot
  if (pn) ctx.pn = pn
  if (windowId) ctx.window = windowId
  return ctx
}

export function formatMroContextStrip(ctx: MroSessionContext, zh: boolean): string {
  const labels: Record<MroSessionScenario, [string, string]> = {
    fault: ['排故', 'Fault'],
    checklist: ['检查单', 'Checklist'],
    due: ['到期', 'Due'],
    tools: ['工具化工品', 'Tools'],
    parts: ['航材', 'Parts'],
    plan: ['维修计划', 'Plan'],
    manual: ['手册问答', 'Manual Q&A'],
  }
  const [zhLabel, enLabel] = labels[ctx.scenario ?? 'manual']
  return zh
    ? `机务 · ${ctx.tailNo} · ${ctx.asOf} · 本轮：${zhLabel}`
    : `MRO · ${ctx.tailNo} · ${ctx.asOf} · ${enLabel}`
}

export function mroAskSessionTitle(scenario?: MroSessionScenario, prompt?: string): string {
  if (prompt?.trim()) return prompt.trim().slice(0, 80)
  switch (scenario) {
    case 'fault': return '排故'
    case 'checklist': return '检查单'
    case 'due': return '到期'
    case 'tools': return '工具化工品'
    case 'parts': return '航材'
    case 'plan': return '维修计划'
    default: return '机务手册'
  }
}
