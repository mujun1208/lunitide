import type { ModelDTO, ProviderDTO } from '../generated/bridge'

export type ModelKind = 'llm' | 'vision' | 'image' | 'video' | 'voice'

export const MODEL_KINDS: readonly ModelKind[] = ['llm', 'vision', 'image', 'video', 'voice']

export const MODEL_KIND_LABELS: Record<ModelKind, string> = {
  llm: 'LLM',
  vision: '视觉模型',
  image: '生图模型',
  video: '生视频模型',
  voice: '语音模型',
}

export function modelKind(model: Pick<ModelDTO, 'kind'> | { kind?: string }): ModelKind {
  const raw = typeof model.kind === 'string' ? model.kind : ''
  if (raw === 'vision' || raw === 'image' || raw === 'video' || raw === 'voice') return raw
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

export function preferredLLMOf(provider: ProviderDTO): { providerId: string; modelId: string } | undefined {
  if (provider.status !== 'enabled' || provider.credentialState !== 'configured') return undefined
  const llms = provider.models.filter(m => modelKind(m) === 'llm')
  const marked = llms.find(m => m.kindDefault) ?? llms.find(m => m.isDefault) ?? llms[0]
  if (!marked?.modelId) return undefined
  return { providerId: provider.id, modelId: marked.modelId }
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

export function voiceReadyProviders(items: readonly ProviderDTO[]): ProviderDTO[] {
  const out: ProviderDTO[] = []
  for (const item of items) {
    if (item.protocol !== 'volc_speech') continue
    if (item.status !== 'enabled' || item.credentialState !== 'configured') continue
    const models = item.models.filter(m => modelKind(m) === 'voice')
    if (!models.length) continue
    out.push({ ...item, models })
  }
  return out
}

export function pickDefaultVoice(items: readonly ProviderDTO[]): { provider: ProviderDTO; modelId: string } | undefined {
  const ready = voiceReadyProviders(items)
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
