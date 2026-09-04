import { OPS_COLLEAGUE_IDS, conversationExpertByNameOrID, isOpsColleague } from './conversationExperts'

export function findInstalledExpert<T extends {expertId: string; catalogItemId?: string; name?: string; state?: string}>(
  items: readonly T[],
  catalogItemId: string,
): T | undefined {
  const key = catalogItemId.trim()
  return items.find(item => (item.catalogItemId ?? '') === key)
}

export function isEnabledMroExpert(items: readonly {catalogItemId?: string; state?: string}[]): boolean {
  return items.some(item => item.catalogItemId === 'mro-expert' && item.state === 'enabled')
}

export function isEnabledOpsWorkbench(items: readonly {catalogItemId?: string; state?: string}[]): boolean {
  return items.some(item => (OPS_COLLEAGUE_IDS as readonly string[]).includes(item.catalogItemId ?? '') && item.state === 'enabled')
}

export function installedOpsExpertIds(
  items: readonly {expertId: string; catalogItemId?: string; state?: string}[],
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const id of OPS_COLLEAGUE_IDS) {
    const hit = findInstalledExpert(items, id)
    if (hit?.state === 'enabled' && hit.expertId) out[id] = hit.expertId
  }
  return out
}

export type WorkbenchRail = 'manuals' | 'fault' | 'due' | 'tools' | 'parts' | 'plan'

export function workbenchRailForCatalog(idOrName: string): WorkbenchRail {
  const hit = conversationExpertByNameOrID(idOrName)
  switch (hit?.id) {
    case 'uas-airworthiness-expert': return 'due'
    case 'tooling-chemical-expert': return 'tools'
    case 'parts-expert': return 'parts'
    case 'mx-planning-expert': return 'plan'
    default: return 'manuals'
  }
}

export function isUASModel(model?: string): boolean {
  const m = (model ?? '').toLowerCase()
  return /uas|evtol|无人机|uav/.test(m)
}

export function askCatalogForRail(rail: string, model?: string): string {
  switch (rail) {
    case 'due':
      return isUASModel(model) ? 'uas-airworthiness-expert' : 'mx-planning-expert'
    case 'tools': return 'tooling-chemical-expert'
    case 'parts': return 'parts-expert'
    case 'plan': return 'mx-planning-expert'
    default: return 'mro-expert'
  }
}

export function resolveAskExpertId(
  rail: string,
  opsExpertIds: Record<string, string>,
  mroExpertId: string,
  model?: string,
): string {
  const catalog = askCatalogForRail(rail, model)
  return opsExpertIds[catalog] || mroExpertId || ''
}

/** Tools + UAS or MRO, max 2 ULIDs for quality-bulletin Ask. */
export function bulletinExpertIds(opsExpertIds: Record<string, string>, mroExpertId: string): string[] {
  const first = opsExpertIds['tooling-chemical-expert'] || mroExpertId
  const second = opsExpertIds['uas-airworthiness-expert'] || opsExpertIds['mro-expert'] || mroExpertId
  const out: string[] = []
  if (first) out.push(first)
  if (second && second !== first && out.length < 2) out.push(second)
  return out
}

export function resolveInstalledExpertIds(
  slugs: readonly string[],
  installed: readonly {expertId: string; catalogItemId?: string; name?: string}[],
): {ids: string[]; missing: string[]} {
  const ids: string[] = []
  const missing: string[] = []
  const seen = new Set<string>()
  for (const raw of slugs) {
    const key = raw.trim()
    if (!key) continue
    const byId = installed.find(item => item.expertId === key)
    const byCatalog = byId ?? findInstalledExpert(installed, key) ?? installed.find(item => (item.name ?? '') === key)
    if (byCatalog?.expertId) {
      if (!seen.has(byCatalog.expertId)) {
        seen.add(byCatalog.expertId)
        ids.push(byCatalog.expertId)
      }
      continue
    }
    missing.push(key)
  }
  return {ids, missing}
}

export { isOpsColleague }
