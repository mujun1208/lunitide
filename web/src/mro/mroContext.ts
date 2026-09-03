export type MroSessionContext = {
  tailNo: string
  asOf: string
  manualIds: string[]
  pack: 'mro.v1'
  scenario?: 'manual' | 'fault' | 'checklist'
}

export const MRO_CONTEXT_KEY = 'mroContext'

const ULID = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/
const AS_OF = /^\d{4}-\d{2}-\d{2}$/

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
  const scenario = row.scenario === 'fault' || row.scenario === 'checklist' ? row.scenario : 'manual'
  return { tailNo, asOf, manualIds, pack: 'mro.v1', scenario }
}

export function formatMroContextStrip(ctx: MroSessionContext, zh: boolean): string {
  const turn = ctx.scenario === 'fault'
    ? (zh ? '排故' : 'Fault')
    : ctx.scenario === 'checklist'
      ? (zh ? '检查单' : 'Checklist')
      : (zh ? '手册问答' : 'Manual Q&A')
  return zh
    ? `机务 · ${ctx.tailNo} · ${ctx.asOf} · 本轮：${turn}`
    : `MRO · ${ctx.tailNo} · ${ctx.asOf} · ${turn}`
}
