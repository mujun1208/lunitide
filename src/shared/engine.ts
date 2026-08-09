import type { UpdateStatus } from './update'
import type { ProviderApiKeyReveal, ProviderConfig, ProviderInput, ProviderModelsResult, ProviderTestResult } from './models'

export type EngineState = 'starting' | 'ready' | 'degraded' | 'restarting' | 'stopped'

export interface EngineStatus {
  state: EngineState
  detail: string
  pid?: number
  restartCount: number
  updatedAt: string
}

export interface LunitideApi {
  getEngineStatus: () => Promise<EngineStatus>
  restartEngine: () => Promise<EngineStatus>
  exportDiagnostics: () => Promise<string | null>
  onEngineStatus: (listener: (status: EngineStatus) => void) => () => void
  getUpdateStatus: () => Promise<UpdateStatus>
  checkForUpdates: () => Promise<UpdateStatus>
  installUpdate: () => Promise<void>
  onUpdateStatus: (listener: (status: UpdateStatus) => void) => () => void
  listProviders: () => Promise<ProviderConfig[]>
  saveProvider: (input: ProviderInput) => Promise<ProviderConfig>
  deleteProvider: (id: string) => Promise<void>
  revealProviderApiKey: (id: string) => Promise<ProviderApiKeyReveal>
  fetchProviderModels: (id: string) => Promise<ProviderModelsResult>
  testProvider: (id: string, model?: string) => Promise<ProviderTestResult>
}
