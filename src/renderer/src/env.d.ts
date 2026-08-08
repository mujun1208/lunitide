import type { LunitideApi } from '../../shared/engine'

declare global {
  interface Window {
    lunitide: LunitideApi
  }
}

export {}
