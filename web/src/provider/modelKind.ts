import type { ModelDTO, ProviderDTO } from '../generated/bridge'

export type ModelKind = 'llm' | 'vision' | 'image' | 'video'

export const MODEL_KINDS: readonly ModelKind[] = ['llm', 'vision', 'image', 'video']

export const MODEL_KIND_LABELS: Record<ModelKind, string> = {
  llm: 'LLM',
  vision: '视觉模型',
  image: '生图模型',
  video: '生视频模型',
}

export function modelKind(model: Pick<ModelDTO, 'kind'> | { kind?: string }): ModelKind {
  const raw = typeof model.kind === 'string' ? model.kind : ''
  if (raw === 'vision' || raw === 'image' || raw === 'video') return raw
  return 'llm'
}

export function llmReadyProviders(items: readonly ProviderDTO[]): ProviderDTO[] {
  const out: ProviderDTO[] = []
  for (const item of items) {
    if (item.status !== 'enabled' || item.credentialState !== 'configured') continue
    const models = item.models.filter(m => modelKind(m) === 'llm')
    if (!models.length) continue
    out.push({ ...item, models })
  }
  return out
}

export function pickDefaultLLM(items: readonly ProviderDTO[]): { provider: ProviderDTO; modelId: string } | undefined {
  const ready = llmReadyProviders(items)
  for (const p of ready) {
    const marked = p.models.find(m => m.kindDefault)
    if (marked) return { provider: p, modelId: marked.modelId }
  }
  const first = ready[0]
  if (!first) return undefined
  const modelId = first.models.find(m => m.isDefault)?.modelId ?? first.models[0]?.modelId
  if (!modelId) return undefined
  return { provider: first, modelId }
}
