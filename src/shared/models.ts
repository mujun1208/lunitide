export type ProviderProtocol = 'openai' | 'anthropic'

export interface ProviderConfig {
  id: string
  name: string
  protocol: ProviderProtocol
  baseUrl: string
  models: string[]
  defaultModel: string
  hasApiKey: boolean
  persistent: boolean
  createdAt: string
  updatedAt: string
}

export interface ProviderInput {
  id?: string
  name: string
  protocol: ProviderProtocol
  baseUrl: string
  models: string[]
  defaultModel: string
  apiKey?: string
}

export interface ProviderApiKeyReveal {
  apiKey: string
}

export interface ProviderModelsResult {
  models: string[]
  detail: string
}

export interface ProviderTestResult {
  ok: boolean
  detail: string
  model?: string
  httpStatus?: number
  latencyMs?: number
  checkedAt?: string
}
