export type MroRailLeaf = 'manuals' | 'fault' | 'due' | 'tools' | 'parts' | 'plan' | 'checklist' | 'audit' | 'fleet'

export type MroRailGroupId = 'manuals' | 'mx' | 'parts' | 'tools-chem' | 'utils'

export type MroRailGroup = {
  id: MroRailGroupId
  label: string
  labelEn: string
  rails: readonly MroRailLeaf[]
}

export const MRO_RAIL_GROUPS: readonly MroRailGroup[] = [
  {id: 'manuals', label: '手册', labelEn: 'Manuals', rails: ['manuals']},
  {id: 'mx', label: '机务维修', labelEn: 'Maintenance', rails: ['fault', 'due']},
  {id: 'parts', label: '航材', labelEn: 'Parts', rails: ['parts', 'plan']},
  {id: 'tools-chem', label: '工具化工品', labelEn: 'Tools & chemicals', rails: ['tools']},
  {id: 'utils', label: '工具', labelEn: 'Utilities', rails: ['checklist', 'audit', 'fleet']},
]

export const MRO_RAIL_LEAF_LABELS: Record<MroRailLeaf, {zh: string; en: string}> = {
  manuals: {zh: '手册', en: 'Manuals'},
  fault: {zh: '排故', en: 'Fault'},
  due: {zh: '到期', en: 'Due'},
  tools: {zh: '工具化工品', en: 'Tools'},
  parts: {zh: '航材', en: 'Parts'},
  plan: {zh: '计划', en: 'Plan'},
  checklist: {zh: '检查单', en: 'Checklist'},
  audit: {zh: '审计', en: 'Audit'},
  fleet: {zh: '机队', en: 'Fleet'},
}

export const MRO_RAIL_OPEN_KEY = 'lunitide:mro-rail-open'

export function groupIdForRail(rail: string): MroRailGroupId | undefined {
  return MRO_RAIL_GROUPS.find(group => (group.rails as readonly string[]).includes(rail))?.id
}

export function readRailOpen(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(MRO_RAIL_OPEN_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const out: Record<string, boolean> = {}
    for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
      if (typeof value === 'boolean') out[key] = value
    }
    return out
  } catch {
    return {}
  }
}

export function writeRailOpen(open: Record<string, boolean>): void {
  try {
    localStorage.setItem(MRO_RAIL_OPEN_KEY, JSON.stringify(open))
  } catch { /* ignore quota */ }
}

/** Current rail's group is always expanded. Other groups stay open until the user collapses them. */
export function railGroupExpanded(groupId: string, currentRail: string, open: Record<string, boolean>): boolean {
  if (groupIdForRail(currentRail) === groupId) return true
  return open[groupId] !== false
}
