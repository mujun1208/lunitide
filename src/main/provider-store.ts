import { isIP } from 'node:net'
import { randomUUID } from 'node:crypto'
import { promises as fs } from 'node:fs'
import { dirname, join } from 'node:path'
import { app, safeStorage } from 'electron'
import type { ProviderConfig, ProviderInput, ProviderProtocol } from '../shared/models'

interface StoredProvider extends Omit<ProviderConfig, 'hasApiKey' | 'persistent'> { encryptedApiKey?: string }
interface ResolvedProvider { protocol: ProviderProtocol; baseUrl: string; model: string; apiKey: string }
const DEFAULT_URLS = { openai: 'https://api.openai.com/v1', anthropic: 'https://api.anthropic.com' } as const
const MAX_FIELD = 500

export class ProviderStore {
  private readonly filePath = join(app.getPath('userData'), 'providers.json')
  private stored = new Map<string, StoredProvider>()
  private sessionKeys = new Map<string, string>()
  private writeQueue: Promise<void> = Promise.resolve()

  async initialize(): Promise<void> {
    try {
      const parsed: unknown = JSON.parse(await fs.readFile(this.filePath, 'utf8'))
      if (!parsed || typeof parsed !== 'object' || !Array.isArray((parsed as { providers?: unknown }).providers)) throw new Error('invalid provider file')
      const version = (parsed as { version?: unknown }).version
      if (![1, 2, '0.1', '0.2', '0.2.1'].includes(version as string | number)) throw new Error('unsupported provider file version')
      const loaded = new Map<string, StoredProvider>()
      for (const candidate of (parsed as { providers: unknown[] }).providers) {
        const provider = this.validateStored(candidate)
        if (!provider) throw new Error('invalid provider entry')
        if (loaded.has(provider.id)) throw new Error('duplicate provider id')
        loaded.set(provider.id, provider)
      }
      // Normalize legacy records in memory only. Automatically rewriting the
      // migration source here could destroy an opaque safeStorage ciphertext
      // before the native Go adoption path has consumed it.
      this.stored = loaded
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') return
      this.stored.clear()
      throw new Error('供应商迁移源无效；为保护原始凭据，文件未被修改', { cause: error })
    }
  }

  list(): ProviderConfig[] { return [...this.stored.values()].map((provider) => this.toPublic(provider)) }

  async save(raw: ProviderInput): Promise<ProviderConfig> {
    const input = validateProviderInput(raw)
    const name = input.name.trim(); const models = normalizeModels(input.models); const defaultModel = input.defaultModel.trim()
    const baseUrl = this.normalizeUrl(input.baseUrl || DEFAULT_URLS[input.protocol])
    const existing = input.id ? this.stored.get(input.id) : undefined
    if (input.id && !existing) throw new Error('供应商配置不存在')
    const originChanged = existing && new URL(existing.baseUrl).origin !== new URL(baseUrl).origin
    const protocolChanged = existing && existing.protocol !== input.protocol
    if ((originChanged || protocolChanged) && !input.apiKey?.trim()) throw new Error('修改协议或服务域名时必须重新输入 API Key')
    const now = new Date().toISOString(); const id = existing?.id ?? randomUUID()
    const nextSessionKeys = new Map(this.sessionKeys)
    const provider: StoredProvider = { id, name, protocol: input.protocol, baseUrl, models, defaultModel, createdAt: existing?.createdAt ?? now, updatedAt: now, encryptedApiKey: existing?.encryptedApiKey }
    if (input.apiKey?.trim()) {
      if (this.canPersistSecurely()) {
        provider.encryptedApiKey = safeStorage.encryptString(JSON.stringify({ version: 1, apiKey: input.apiKey.trim(), origin: new URL(baseUrl).origin, protocol: input.protocol })).toString('base64'); nextSessionKeys.delete(id)
      } else { provider.encryptedApiKey = undefined; nextSessionKeys.set(id, input.apiKey.trim()) }
    }
    const nextStored = new Map(this.stored); nextStored.set(id, provider)
    await this.persist(nextStored); this.stored = nextStored; this.sessionKeys = nextSessionKeys
    return this.toPublic(provider)
  }

  async remove(id: string): Promise<void> {
    if (!isUuid(id)) throw new Error('供应商 ID 无效')
    const nextStored = new Map(this.stored); nextStored.delete(id)
    await this.persist(nextStored); this.stored = nextStored; this.sessionKeys.delete(id)
  }

  revealApiKey(id: string): string {
    if (!isUuid(id)) throw new Error('供应商 ID 无效')
    const provider = this.stored.get(id); if (!provider) throw new Error('供应商配置不存在')
    return this.decryptApiKey(provider)
  }

  resolve(id: string, requestedModel?: string): ResolvedProvider {
    if (!isUuid(id)) throw new Error('供应商 ID 无效')
    const provider = this.stored.get(id); if (!provider) throw new Error('供应商配置不存在')
    const model = requestedModel?.trim() || provider.defaultModel
    if (!provider.models.includes(model)) throw new Error('所选模型不在该供应商的模型列表中')
    return { protocol: provider.protocol, baseUrl: provider.baseUrl, model, apiKey: this.decryptApiKey(provider) }
  }

  private decryptApiKey(provider: StoredProvider): string {
    let apiKey = this.sessionKeys.get(provider.id) ?? ''
    if (!apiKey && provider.encryptedApiKey) {
      if (!this.canPersistSecurely()) throw new Error('系统密钥保护当前不可用，请重新输入会话密钥')
      try {
        const decrypted = safeStorage.decryptString(Buffer.from(provider.encryptedApiKey, 'base64'))
        const secret = JSON.parse(decrypted) as { version?: number; apiKey?: string; origin?: string; protocol?: string }
        if (secret.version !== 1 || secret.origin !== new URL(provider.baseUrl).origin || secret.protocol !== provider.protocol || !secret.apiKey) throw new Error('mismatch')
        apiKey = secret.apiKey
      } catch { throw new Error('已保存的 API Key 无法解密，请重新输入并保存') }
    }
    if (!apiKey) throw new Error('请先配置 API Key')
    return apiKey
  }

  private canPersistSecurely(): boolean {
    if (!safeStorage.isEncryptionAvailable()) return false
    return process.platform !== 'linux' || safeStorage.getSelectedStorageBackend() !== 'basic_text'
  }

  private toPublic(provider: StoredProvider): ProviderConfig { return { id: provider.id, name: provider.name, protocol: provider.protocol, baseUrl: provider.baseUrl, models: [...provider.models], defaultModel: provider.defaultModel, hasApiKey: Boolean(provider.encryptedApiKey || this.sessionKeys.has(provider.id)), persistent: Boolean(provider.encryptedApiKey), createdAt: provider.createdAt, updatedAt: provider.updatedAt } }

  private normalizeUrl(value: string): string {
    const url = new URL(value.trim())
    if (url.username || url.password || url.search || url.hash) throw new Error('Base URL 不允许包含凭据、查询参数或片段')
    const local = ['127.0.0.1', 'localhost', '::1'].includes(url.hostname)
    if (url.protocol !== 'https:' && !(url.protocol === 'http:' && local)) throw new Error('Base URL 必须使用 HTTPS；仅本机服务允许 HTTP')
    if (isIP(url.hostname) && !local && isUnsafeIp(url.hostname)) throw new Error('Base URL 不允许使用私网或保留地址')
    return url.toString().replace(/\/$/, '')
  }

  private validateStored(value: unknown): StoredProvider | null {
    if (!value || typeof value !== 'object') return null
    const item = value as Record<string, unknown>
    if (!isUuid(item.id) || typeof item.name !== 'string' || !isProtocol(item.protocol) || typeof item.baseUrl !== 'string' || typeof item.createdAt !== 'string' || typeof item.updatedAt !== 'string') return null
    try {
      const models = normalizeModels(Array.isArray(item.models) ? item.models : [item.model])
      const defaultModel = typeof item.defaultModel === 'string' && models.includes(item.defaultModel.trim()) ? item.defaultModel.trim() : models[0]
      return { id: item.id, name: item.name.slice(0, MAX_FIELD), protocol: item.protocol, baseUrl: this.normalizeUrl(item.baseUrl), models, defaultModel, createdAt: item.createdAt, updatedAt: item.updatedAt, encryptedApiKey: typeof item.encryptedApiKey === 'string' ? item.encryptedApiKey : undefined }
    } catch { return null }
  }

  private persist(providers: Map<string, StoredProvider>): Promise<void> {
    const write = async (): Promise<void> => { await fs.mkdir(dirname(this.filePath), { recursive: true }); const temporary = `${this.filePath}.${randomUUID()}.tmp`; await fs.writeFile(temporary, JSON.stringify({ version: 2, providers: [...providers.values()] }, null, 2), { encoding: 'utf8', mode: 0o600 }); await fs.rename(temporary, this.filePath) }
    const next = this.writeQueue.then(write, write); this.writeQueue = next.catch(() => undefined); return next
  }
}

export function validateProviderInput(value: unknown): ProviderInput {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('供应商配置格式无效')
  const input = value as Record<string, unknown>; const allowed = new Set(['id', 'name', 'protocol', 'baseUrl', 'models', 'defaultModel', 'apiKey'])
  if (Object.keys(input).some((key) => !allowed.has(key)) || !isProtocol(input.protocol) || typeof input.name !== 'string' || typeof input.baseUrl !== 'string' || !Array.isArray(input.models) || typeof input.defaultModel !== 'string' || (input.id !== undefined && !isUuid(input.id)) || (input.apiKey !== undefined && typeof input.apiKey !== 'string')) throw new Error('供应商配置格式无效')
  const models = normalizeModels(input.models)
  for (const value of [input.name, input.baseUrl, input.defaultModel, input.apiKey ?? '', ...models]) if ((value as string).length > 4096) throw new Error('供应商配置字段过长')
  if (!input.name.trim() || !input.defaultModel.trim() || !models.includes(input.defaultModel.trim())) throw new Error('供应商名称、模型和默认模型不能为空')
  return input as unknown as ProviderInput
}
function normalizeModels(value: unknown[]): string[] {
  if (value.length < 1 || value.length > 50 || value.some((model) => typeof model !== 'string')) throw new Error('请配置 1 至 50 个模型')
  const models = value.map((model) => (model as string).trim())
  if (models.some((model) => !model || model.length > MAX_FIELD) || new Set(models).size !== models.length) throw new Error('模型名称不能为空或重复')
  return models
}
function isProtocol(value: unknown): value is ProviderProtocol { return value === 'openai' || value === 'anthropic' }
function isUuid(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value) }
function isUnsafeIp(value: string): boolean { if (value.includes(':')) return true; const [a, b] = value.split('.').map(Number); return a === 0 || a === 10 || a === 127 || a >= 224 || (a === 169 && b === 254) || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 100 && b >= 64 && b <= 127) }
