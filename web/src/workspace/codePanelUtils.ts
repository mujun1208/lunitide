import type { WorkspaceToolActivity } from './Workspace'

export type CodeFileStatus = 'modified' | 'added' | 'deleted'

export type TaskFileEntry = {
  path: string
  status: CodeFileStatus
  toolName: string
  summary?: string
}

const CHANGE_TOOL = /write|edit|patch|fs\.|workspace\./i

export function isChangeTool(name: string): boolean {
  return CHANGE_TOOL.test(name)
}

export function extractTaskFiles(activities: WorkspaceToolActivity[]): TaskFileEntry[] {
  const map = new Map<string, TaskFileEntry>()
  for (const activity of activities) {
    if (!isChangeTool(activity.name)) continue
    const path = activity.artifact?.path?.trim()
    if (!path) continue
    const status: CodeFileStatus =
      /delete|remove/i.test(activity.name) || /delete|remove/i.test(activity.summary ?? '')
        ? 'deleted'
        : /create|add|write/i.test(activity.name) || /create|add|new file/i.test(activity.summary ?? '')
          ? 'added'
          : 'modified'
    map.set(path, { path, status, toolName: activity.name, summary: activity.summary })
  }
  return [...map.values()].sort((a, b) => a.path.localeCompare(b.path))
}

export function languageFromPath(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    ts: 'TypeScript',
    tsx: 'TSX',
    js: 'JavaScript',
    jsx: 'JSX',
    go: 'Go',
    py: 'Python',
    rs: 'Rust',
    json: 'JSON',
    md: 'Markdown',
    css: 'CSS',
    html: 'HTML',
    sql: 'SQL',
    yaml: 'YAML',
    yml: 'YAML',
  }
  return map[ext] ?? (ext ? ext.toUpperCase() : 'Plain Text')
}

export function statusBadge(status: CodeFileStatus): string {
  if (status === 'added') return 'A'
  if (status === 'deleted') return 'D'
  return 'M'
}
