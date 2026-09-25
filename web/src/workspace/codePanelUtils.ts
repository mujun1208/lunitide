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

export function codeStatusLabel(problemCount: number | null): string {
  if (problemCount == null) return '未检查'
  if (problemCount === 0) return '✓ 0 问题'
  return `${problemCount} 问题`
}

function sourceIdents(content: string): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  const re = /[A-Za-z_][A-Za-z0-9_]*/g
  for (const match of content.matchAll(re)) {
    const name = match[0]
    if (!seen.has(name)) {
      seen.add(name)
      out.push(name)
    }
  }
  return out
}

export function suggestSourceLine(content: string, lineIndex: number): string {
  const lines = content.split('\n')
  if (lineIndex < 0 || lineIndex >= lines.length) return ''
  const line = lines[lineIndex]
  const match = /([A-Za-z_][A-Za-z0-9_]*)\s*$/.exec(line)
  if (!match) return ''
  const token = match[1]
  if (token.length < 2) return ''
  const without = lines.slice()
  const start = match.index ?? line.length - token.length
  without[lineIndex] = line.slice(0, start) + line.slice(start + token.length)
  const idents = sourceIdents(without.join('\n'))
  if (idents.includes(token)) return ''
  let hit = ''
  for (const name of idents) {
    if (!name.startsWith(token)) continue
    if (hit && hit !== name) return ''
    hit = name
  }
  if (!hit) return ''
  return line.slice(0, start) + hit + line.slice(start + token.length)
}

export function latestFileDiff(summaries: string[]): string {
  for (let i = summaries.length - 1; i >= 0; i -= 1) {
    const text = summaries[i] ?? ''
    if (text.includes('\n--- ') && text.includes('\n+++ ')) return text
  }
  return ''
}

export function acceptSourceLine(content: string, lineIndex: number, line: string): string {
  const lines = content.split('\n')
  if (lineIndex < 0 || lineIndex >= lines.length) return content
  lines[lineIndex] = line
  return lines.join('\n')
}
