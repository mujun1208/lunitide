export type WidgetKind = 'timer' | 'checklist' | 'metric' | 'table' | 'bar' | 'progress' | 'todo' | 'checkin'

export type WidgetState = {
  seconds?: number
  running?: boolean
  updatedAt?: string
  value?: string
  unit?: string
  percent?: number
  rows?: string
  checked?: string
  items?: string
}

export type WidgetSpec = {
  id: string
  kind: WidgetKind
  title?: string
  state?: WidgetState
}

const kinds = new Set<WidgetKind>(['timer', 'checklist', 'metric', 'table', 'bar', 'progress', 'todo', 'checkin'])

export function isRegisteredWidget(kind: string): kind is WidgetKind {
  return kinds.has(kind as WidgetKind)
}

export function rejectHostMessage(kind: string): boolean {
  return kind === 'host.message' || kind === 'html' || kind === 'script' || kind === 'javascript'
}

export function splitWidgetList(raw?: string): string[] {
  return (raw || '').split(/[,，]/).map(item => item.trim()).filter(Boolean)
}
