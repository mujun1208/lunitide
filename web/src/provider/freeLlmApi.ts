import { createMutationAttempt, providerBridge, type ProviderBridge } from '../bridge/client'
import type { ModelDTO, ProviderDTO } from '../generated/bridge'
import { normalizeOrigin, originFingerprint } from './fingerprint'

export const FREE_LLM_API_STORAGE_KEY = 'lunitide:freellmapi-hub'
export const FREE_LLM_API_PROVIDER_NAME = 'FreeLLMAPI 免费池'
export const FREE_LLM_API_DEFAULT_ORIGIN = 'http://127.0.0.1:3001'

/** Built-in routing models exposed by FreeLLMAPI (OpenAI-compatible). */
export const FREE_LLM_API_BUILTIN_MODELS: ModelDTO[] = [
  { modelId: 'auto', displayName: '自动路由（推荐）', isDefault: true, contextWindow: 128000 },
  { modelId: 'auto:balanced', displayName: '均衡', isDefault: false, contextWindow: 128000 },
  { modelId: 'auto:fast', displayName: '最快', isDefault: false, contextWindow: 128000 },
  { modelId: 'auto:smart', displayName: '最智能', isDefault: false, contextWindow: 128000 },
  { modelId: 'auto:reliable', displayName: '最可靠', isDefault: false, contextWindow: 128000 },
  { modelId: 'auto:cheap', displayName: '省配额', isDefault: false, contextWindow: 128000 },
]

export type FreeLLMAPIHealth = {
  ok: boolean
  modelCount?: number
  message?: string
  checkedAt: string
}

export type FreeLLMAPIUsageMonth = {
  month: string
  inputTokens: number
  outputTokens: number
  requests: number
}

export type FreeLLMAPIHubConfig = {
  baseUrl: string
  providerId?: string
  preferAutoRouting: boolean
  lastHealth?: FreeLLMAPIHealth
  monthlyUsage?: FreeLLMAPIUsageMonth
}

export function normalizeFreeLLMOrigin(baseUrl: string): string {
  const trimmed = baseUrl.trim() || FREE_LLM_API_DEFAULT_ORIGIN
  try {
    return normalizeOrigin(trimmed.endsWith('/v1') ? trimmed.slice(0, -3) : trimmed)
  } catch {
    return FREE_LLM_API_DEFAULT_ORIGIN
  }
}

export function freeLLMAPIBaseUrl(origin: string): string {
  const clean = normalizeFreeLLMOrigin(origin)
  return `${clean}/v1`
}

export function loadFreeLLMAPIHub(): FreeLLMAPIHubConfig {
  try {
    const raw = localStorage.getItem(FREE_LLM_API_STORAGE_KEY)
    if (!raw) return defaultHubConfig()
    const parsed = JSON.parse(raw) as Partial<FreeLLMAPIHubConfig>
    return {
      baseUrl: parsed.baseUrl?.trim() || FREE_LLM_API_DEFAULT_ORIGIN,
      providerId: parsed.providerId,
      preferAutoRouting: parsed.preferAutoRouting !== false,
      lastHealth: parsed.lastHealth,
      monthlyUsage: parsed.monthlyUsage,
    }
  } catch {
    return defaultHubConfig()
  }
}

export function saveFreeLLMAPIHub(config: FreeLLMAPIHubConfig): void {
  localStorage.setItem(FREE_LLM_API_STORAGE_KEY, JSON.stringify(config))
}

function defaultHubConfig(): FreeLLMAPIHubConfig {
  return { baseUrl: FREE_LLM_API_DEFAULT_ORIGIN, preferAutoRouting: true }
}

export function currentUsageMonth(): string {
  const now = new Date()
  return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
}

export function recordFreeLLMAPIUsage(
  hub: FreeLLMAPIHubConfig,
  usage: { inputTokens: number; outputTokens: number },
): FreeLLMAPIHubConfig {
  const month = currentUsageMonth()
  const prev = hub.monthlyUsage?.month === month
    ? hub.monthlyUsage
    : { month, inputTokens: 0, outputTokens: 0, requests: 0 }
  const next: FreeLLMAPIHubConfig = {
    ...hub,
    monthlyUsage: {
      month,
      inputTokens: prev.inputTokens + usage.inputTokens,
      outputTokens: prev.outputTokens + usage.outputTokens,
      requests: prev.requests + 1,
    },
  }
  saveFreeLLMAPIHub(next)
  return next
}

export function isFreeLLMAPIProvider(provider: ProviderDTO | undefined, hub: FreeLLMAPIHubConfig): boolean {
  if (!provider) return false
  if (hub.providerId && provider.id === hub.providerId) return true
  if (provider.name === FREE_LLM_API_PROVIDER_NAME) return true
  try {
    return normalizeOrigin(provider.baseUrl) === freeLLMAPIBaseUrl(hub.baseUrl)
  } catch {
    return false
  }
}

export async function probeFreeLLMAPI(baseUrl: string, apiKey: string): Promise<FreeLLMAPIHealth> {
  const checkedAt = new Date().toISOString()
  const origin = normalizeFreeLLMOrigin(baseUrl)
  if (!apiKey.trim()) {
    return { ok: false, message: '请输入 FreeLLMAPI 统一密钥（Dashboard → Keys）', checkedAt }
  }
  try {
    const res = await fetch(`${origin}/v1/models`, {
      headers: { Authorization: `Bearer ${apiKey.trim()}` },
    })
    if (!res.ok) {
      const body = await res.text().catch(() => '')
      return {
        ok: false,
        message: body ? `HTTP ${res.status}` : `无法连接（HTTP ${res.status}）`,
        checkedAt,
      }
    }
    const payload = await res.json() as { data?: unknown[] }
    const modelCount = Array.isArray(payload.data) ? payload.data.length : undefined
    return {
      ok: true,
      modelCount,
      message: modelCount ? `已连接 · ${modelCount} 个模型端点` : '已连接',
      checkedAt,
    }
  } catch (e) {
    return {
      ok: false,
      message: e instanceof Error
        ? `${e.message} · 请确认 FreeLLMAPI 已在本地运行（默认 ${FREE_LLM_API_DEFAULT_ORIGIN}）`
        : '连接失败',
      checkedAt,
    }
  }
}

export async function fetchFreeLLMAPIRemoteModels(baseUrl: string, apiKey: string): Promise<ModelDTO[]> {
  const origin = normalizeFreeLLMOrigin(baseUrl)
  const res = await fetch(`${origin}/v1/models`, {
    headers: { Authorization: `Bearer ${apiKey.trim()}` },
  })
  if (!res.ok) throw new Error(`模型列表不可用（HTTP ${res.status}）`)
  const payload = await res.json() as { data?: Array<{ id?: string }> }
  const remote = (payload.data ?? [])
    .map(item => item.id?.trim())
    .filter((id): id is string => !!id && id.length <= 200 && /^[\x21-\x7E]+$/.test(id))
  const merged = new Map<string, ModelDTO>()
  for (const model of FREE_LLM_API_BUILTIN_MODELS) merged.set(model.modelId, { ...model })
  for (const id of remote.slice(0, 44)) {
    if (merged.has(id)) continue
    merged.set(id, { modelId: id, displayName: id, isDefault: false, contextWindow: 128000 })
  }
  const models = [...merged.values()]
  if (!models.some(m => m.isDefault)) models[0] = { ...models[0]!, isDefault: true }
  return models.slice(0, 50)
}

export async function installFreeLLMAPIProvider(
  baseUrl: string,
  apiKey: string,
  bridge: ProviderBridge = providerBridge,
  existingProviderId?: string,
): Promise<{ provider: ProviderDTO; hub: FreeLLMAPIHubConfig }> {
  const origin = normalizeFreeLLMOrigin(baseUrl)
  const base = freeLLMAPIBaseUrl(origin)
  const models = await fetchFreeLLMAPIRemoteModels(origin, apiKey)
  const health = await probeFreeLLMAPI(origin, apiKey)
  if (!health.ok) throw new Error(health.message ?? 'FreeLLMAPI 不可用')

  if (existingProviderId) {
    const current = await bridge.get({ id: existingProviderId })
    const payload = {
      id: current.id,
      name: FREE_LLM_API_PROVIDER_NAME,
      protocol: 'openai_compatible' as const,
      baseUrl: base,
      models,
      status: 'enabled' as const,
      expectedVersion: current.version,
    }
    let credentialSubmissionId: string | undefined
    if (apiKey.trim()) {
      credentialSubmissionId = (await bridge.submitCredential({
        scope: { providerId: current.id },
        request: payload,
        credential: apiKey.trim(),
      })).credentialSubmissionId
    }
    const attempt = createMutationAttempt('provider.update', { ...payload, ...(credentialSubmissionId ? { credentialSubmissionId } : {}) })
    const saved = await bridge.update(attempt.payload, { attempt })
    const hub: FreeLLMAPIHubConfig = {
      baseUrl: origin,
      providerId: saved.id,
      preferAutoRouting: true,
      lastHealth: health,
      monthlyUsage: loadFreeLLMAPIHub().monthlyUsage,
    }
    saveFreeLLMAPIHub(hub)
    return { provider: saved, hub }
  }

  const createPayload = {
    name: FREE_LLM_API_PROVIDER_NAME,
    protocol: 'openai_compatible' as const,
    baseUrl: base,
    models,
    status: 'enabled' as const,
  }
  const fingerprint = await originFingerprint(createPayload.protocol, createPayload.baseUrl)
  const credentialSubmissionId = (await bridge.submitCredential({
    scope: { draftFingerprint: fingerprint.fingerprint },
    protocol: createPayload.protocol,
    origin: normalizeOrigin(createPayload.baseUrl),
    request: createPayload,
    credential: apiKey.trim(),
  })).credentialSubmissionId
  const attempt = createMutationAttempt('provider.create', { ...createPayload, credentialSubmissionId })
  const saved = await bridge.create(attempt.payload, { attempt })
  const hub: FreeLLMAPIHubConfig = {
    baseUrl: origin,
    providerId: saved.id,
    preferAutoRouting: true,
    lastHealth: health,
    monthlyUsage: loadFreeLLMAPIHub().monthlyUsage,
  }
  saveFreeLLMAPIHub(hub)
  return { provider: saved, hub }
}

export function freeLLMAPIDashboardUrl(baseUrl: string): string {
  const origin = normalizeFreeLLMOrigin(baseUrl)
  if (origin.includes('127.0.0.1:3001') || origin.includes('localhost:3001')) {
    return 'http://127.0.0.1:5173'
  }
  return origin
}
