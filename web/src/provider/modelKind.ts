import type { ModelDTO, ProviderDTO } from '../generated/bridge'
import { VOLC_ASR_RESOURCE_ID, VOLC_DEFAULT_VOICE_ID, VOLC_TTS_RESOURCE_ID, isVolcSpeakerId, isVolcTtsResourceId, nextUnusedVolcSpeaker } from '../session/companion/volcVoices'

export type ModelKind = 'llm' | 'vision' | 'image' | 'video' | 'voice'
export type PersistKind = 'llm' | 'vision' | 'image' | 'video' | 'asr' | 'tts'
export type VoiceRole = 'asr' | 'tts'

export const MODEL_KINDS: readonly ModelKind[] = ['llm', 'vision', 'image', 'video', 'voice']
export const VOICE_ROLES: readonly VoiceRole[] = ['asr', 'tts']

export const MODEL_KIND_LABELS: Record<ModelKind, string> = {
  llm: 'LLM',
  vision: '视觉模型',
  image: '生图模型',
  video: '生视频模型',
  voice: '语音模型',
}

export const VOICE_ROLE_LABELS: Record<VoiceRole, string> = {
  asr: '听写',
  tts: '朗读',
}

/** Stored kind. Leftover voice is listen (asr). */
export function persistKind(model: Pick<ModelDTO, 'kind'> | { kind?: string }): PersistKind {
  const raw = typeof model.kind === 'string' ? model.kind : ''
  if (raw === 'tts') return 'tts'
  if (raw === 'asr' || raw === 'voice') return 'asr'
  if (raw === 'vision' || raw === 'image' || raw === 'video') return raw
  return 'llm'
}

export function modelKind(model: Pick<ModelDTO, 'kind'> | { kind?: string }): ModelKind {
  const stored = persistKind(model)
  if (stored === 'asr' || stored === 'tts') return 'voice'
  return stored
}

export function kindLabel(model: Pick<ModelDTO, 'kind'> | { kind?: string }): string {
  const stored = persistKind(model)
  if (stored === 'asr' || stored === 'tts') return VOICE_ROLE_LABELS[stored]
  return MODEL_KIND_LABELS[stored]
}

export function nextVoiceRole(models: readonly { kind?: string }[]): VoiceRole {
  return models.some(m => persistKind(m) === 'asr') ? 'tts' : 'asr'
}

export function blankVoiceModels(): ModelDTO[] {
  return [
    { modelId: VOLC_ASR_RESOURCE_ID, displayName: '豆包流式语音识别 2.0', isDefault: true, kind: 'asr', kindDefault: true },
    { modelId: VOLC_TTS_RESOURCE_ID, displayName: '豆包语音合成 2.0', isDefault: false, kind: 'tts', kindDefault: true },
  ]
}

export function extraVoiceModel(models: readonly { modelId?: string; kind?: string; kindDefault?: boolean }[]): ModelDTO {
  if (nextVoiceRole(models) === 'asr') {
    return {
      modelId: VOLC_ASR_RESOURCE_ID,
      displayName: '豆包流式语音识别 2.0',
      isDefault: false,
      kind: 'asr',
      kindDefault: !models.some(m => persistKind(m) === 'asr' && m.kindDefault),
    }
  }
  if (!models.some(m => persistKind(m) === 'tts')) {
    return {
      modelId: VOLC_TTS_RESOURCE_ID,
      displayName: '豆包语音合成 2.0',
      isDefault: false,
      kind: 'tts',
      kindDefault: true,
    }
  }
  const next = nextUnusedVolcSpeaker(models.map(m => m.modelId ?? ''))
  return {
    modelId: next?.id ?? '',
    displayName: next?.name ?? '',
    isDefault: false,
    kind: 'tts',
    kindDefault: !models.some(m => persistKind(m) === 'tts' && m.kindDefault),
  }
}

/** Meeting / wake / 月伴开麦 only. TTS speakers must never become the listen model. */
export function isAsrVoiceModel(model: { modelId?: string; kind?: string }): boolean {
  if (persistKind(model) !== 'asr') return false
  const id = (model.modelId ?? '').toLowerCase()
  if (!id) return false
  if (id.includes('tts') || id.includes('uranus') || id.includes('bigtts') || id.startsWith('saturn_')) return false
  return true
}

export function isTtsVoiceModel(model: { kind?: string }): boolean {
  return persistKind(model) === 'tts'
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

const COMPANION_FLASH_RE = /(?:flash|air|lite|mini|haiku)/i
const COMPANION_REALTIME_RE = /realtime(?:-preview)?|(?:^|[^A-Za-z0-9])live(?:[^A-Za-z0-9]|$)/i

export function isCompanionFlashModelId(modelId: string): boolean {
  const id = modelId.trim()
  if (!id) return false
  if (isTalkRealtimeModelId(id)) return false
  return COMPANION_FLASH_RE.test(id)
}

/** Contract T0-2: id or display name matches realtime / live / realtime-preview. */
export function isTalkRealtimeModelId(modelId: string, displayName = ''): boolean {
  const blob = `${modelId} ${displayName}`.trim()
  return blob !== '' && COMPANION_REALTIME_RE.test(blob)
}

/** First listed openai_compatible realtime/live model. Does not invent ids. */
export function pickTalkRealtimeModel(items: readonly ProviderDTO[]): { providerId: string; modelId: string } | undefined {
  for (const provider of items) {
    if (provider.protocol !== 'openai_compatible') continue
    if (provider.status !== 'enabled' || provider.credentialState !== 'configured') continue
    const hit = provider.models.find(model => model.modelId && isTalkRealtimeModelId(model.modelId, model.displayName))
    if (hit?.modelId) return { providerId: provider.id, modelId: hit.modelId }
  }
  return undefined
}

/** Same provider only. Honor the model already picked on home/session; flash only when current is empty or missing. */
export function pickCompanionFlashModel(
  items: readonly ProviderDTO[],
  providerId: string,
  currentModelId: string,
): { providerId: string; modelId: string } {
  const fallback = { providerId, modelId: currentModelId }
  const provider = items.find(item => item.id === providerId)
  if (!provider) return fallback
  const llms = provider.models.filter(m => modelKind(m) === 'llm' && m.modelId)
  if (currentModelId && llms.some(m => m.modelId === currentModelId)) {
    return fallback
  }
  const flash = llms.find(m => isCompanionFlashModelId(m.modelId))
  if (flash?.modelId) return { providerId, modelId: flash.modelId }
  return fallback
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

function volcReady(item: ProviderDTO): boolean {
  return item.protocol === 'volc_speech' && item.status === 'enabled' && item.credentialState === 'configured'
}

export function voiceReadyProviders(items: readonly ProviderDTO[]): ProviderDTO[] {
  const out: ProviderDTO[] = []
  for (const item of items) {
    if (!volcReady(item)) continue
    const models = item.models.filter(isAsrVoiceModel)
    if (!models.length) continue
    out.push({ ...item, models })
  }
  return out
}

export function ttsReadyProviders(items: readonly ProviderDTO[]): ProviderDTO[] {
  const out: ProviderDTO[] = []
  for (const item of items) {
    if (!volcReady(item)) continue
    const models = item.models.filter(isTtsVoiceModel)
    if (!models.length) continue
    out.push({ ...item, models })
  }
  return out
}

export function hasConfiguredVolcTts(items: readonly ProviderDTO[]): boolean {
  return ttsReadyProviders(items).length > 0
}

function preferAsrModelId(models: readonly { modelId?: string; kindDefault?: boolean; isDefault?: boolean }[]): string | undefined {
  const ids = models.map(m => m.modelId).filter((id): id is string => Boolean(id))
  const seed = ids.find(id => /seed-?asr/i.test(id))
  if (seed) return seed
  const asr = ids.find(id => /asr/i.test(id))
  if (asr) return asr
  const marked = models.find(m => m.kindDefault)?.modelId ?? models.find(m => m.isDefault)?.modelId
  return marked ?? ids[0]
}

export function pickDefaultVoice(items: readonly ProviderDTO[]): { provider: ProviderDTO; modelId: string } | undefined {
  const ready = voiceReadyProviders(items)
  for (const p of ready) {
    const marked = p.models.find(m => m.kindDefault && isAsrVoiceModel(m))
    if (marked?.modelId) return { provider: p, modelId: marked.modelId }
  }
  const first = ready[0]
  if (!first) return undefined
  const modelId = preferAsrModelId(first.models)
  if (!modelId) return undefined
  return { provider: first, modelId }
}

function preferTtsSpeakId(models: readonly { modelId?: string; kind?: string; kindDefault?: boolean; isDefault?: boolean }[]): string | undefined {
  const ttsModels = models.filter(m => isTtsVoiceModel(m) && m.modelId)
  const speakers = ttsModels.filter(m => isVolcSpeakerId(m.modelId ?? ''))
  const markedSpeaker = speakers.find(m => m.kindDefault) ?? speakers.find(m => m.isDefault)
  if (markedSpeaker?.modelId) return markedSpeaker.modelId
  if (speakers[0]?.modelId) return speakers[0].modelId
  const marked = ttsModels.find(m => m.kindDefault) ?? ttsModels.find(m => m.isDefault) ?? ttsModels[0]
  if (!marked?.modelId) return undefined
  if (isVolcTtsResourceId(marked.modelId)) return VOLC_DEFAULT_VOICE_ID
  return marked.modelId
}

export function pickDefaultTTS(items: readonly ProviderDTO[]): { provider: ProviderDTO; modelId: string } | undefined {
  const ready = ttsReadyProviders(items)
  for (const p of ready) {
    const modelId = preferTtsSpeakId(p.models)
    if (modelId) return { provider: p, modelId }
  }
  return undefined
}
