export type AssetTemplateOption = { id: string; name: string; updatedAt?: string }

export type PhaseAssetBinding = {
  key: string
  title: string
  templateId: string
  name: string
}

export function pickPhaseAssetBindings(
  docs: Array<{ key: string; title: string }>,
  items: Array<{ documentType: string; templateId?: string }>,
  templatesByDoc: Map<string, AssetTemplateOption[]> | Record<string, AssetTemplateOption[]>,
): PhaseAssetBinding[] {
  const lookup = (key: string): AssetTemplateOption[] => {
    if (templatesByDoc instanceof Map) return templatesByDoc.get(key) ?? []
    return templatesByDoc[key] ?? []
  }
  const byType = new Map(items.map(item => [item.documentType, item]))
  const out: PhaseAssetBinding[] = []
  for (const doc of docs) {
    if (byType.get(doc.key)?.templateId) continue
    const pick = [...lookup(doc.key)].sort((a, b) => (b.updatedAt ?? '').localeCompare(a.updatedAt ?? ''))[0]
    if (!pick) continue
    out.push({ key: doc.key, title: doc.title, templateId: pick.id, name: pick.name })
  }
  return out
}
