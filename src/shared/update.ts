export type UpdateState =
  | 'idle'
  | 'checking'
  | 'available'
  | 'not-available'
  | 'downloading'
  | 'downloaded'
  | 'error'
  | 'unavailable'

export interface UpdateStatus {
  state: UpdateState
  currentVersion: string
  availableVersion?: string
  percent?: number
  detail: string
}
