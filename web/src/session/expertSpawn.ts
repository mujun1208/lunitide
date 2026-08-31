import {CONVERSATION_EXPERTS} from '../expert/conversationExperts'

const EXPERT_REF = /\[引用专家 [^\]|\r\n]+\|([0-7][0-9A-HJKMNP-TV-Z]{25})\]/g
const COUNCIL_MAX = 8

export const CONVERSATION_SPECIALIST_NAMES = new Set<string>(CONVERSATION_EXPERTS.map(item => item.name))

export function extractExpertRefIDs(text: string): string[] {
  const ids: string[] = []
  const seen = new Set<string>()
  EXPERT_REF.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = EXPERT_REF.exec(text))) {
    const id = match[1]
    if (!id || seen.has(id)) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
}

function uniqueCap(ids: readonly string[], cap = COUNCIL_MAX): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const id of ids) {
    if (!id || seen.has(id)) continue
    seen.add(id)
    out.push(id)
    if (out.length >= cap) break
  }
  return out
}

/** Same spawn filter as the engine: turn chips win; one mount stays; two+ mounts need chips. */
export function selectedTurnExpertIDs(mounted: readonly string[], ...turnTexts: string[]): string[] {
  for (const text of turnTexts) {
    const refs = extractExpertRefIDs(text)
    if (refs.length) return uniqueCap(refs)
  }
  if (mounted.length === 1) return uniqueCap(mounted)
  return []
}

export function spawnedExpertsMatchSelection(selectedIds: readonly string[], spawnedIds: readonly string[]): boolean {
  const allowed = new Set(selectedIds)
  return spawnedIds.every(id => allowed.has(id))
}

export function pmRethinkSpawnsConversationSpecialists(selectedNames: readonly string[], spawnedNames: readonly string[]): boolean {
  const selected = new Set(selectedNames)
  return spawnedNames.some(name => CONVERSATION_SPECIALIST_NAMES.has(name) && !selected.has(name))
}
