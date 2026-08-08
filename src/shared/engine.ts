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
  onEngineStatus: (listener: (status: EngineStatus) => void) => () => void
}
