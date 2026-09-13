export type BoardStats = {
  total: number
  added: number
  modified: number
  unchanged: number
  removed: number
  done: number
  pending: number
  inProgress: number
  returned: number
  needsReprocess: number
}

export function formatBoardStats(s: BoardStats): string {
  return `共 ${s.total} 条 · 新增 ${s.added} · 变更 ${s.modified} · 未改 ${s.unchanged} · 已做 ${s.done} · 未做 ${s.total - s.done} · 待再处理 ${s.needsReprocess}`
}

export function summarizeBoardItems(items: Array<{
  status?: string
  changeKind?: string
  needsReprocess?: boolean
  testReturn?: unknown
}>): BoardStats {
  const s: BoardStats = {
    total: 0, added: 0, modified: 0, unchanged: 0, removed: 0,
    done: 0, pending: 0, inProgress: 0, returned: 0, needsReprocess: 0,
  }
  for (const item of items) {
    if (item.changeKind === 'added') s.added++
    else if (item.changeKind === 'modified') s.modified++
    else if (item.changeKind === 'removed') s.removed++
    else if (item.changeKind === 'unchanged') s.unchanged++
    if (item.changeKind === 'removed') continue
    s.total++
    if (item.status === 'dev_done' || item.status === 'test_pass') s.done++
    else if (item.status === 'pending') s.pending++
    else if (item.status === 'in_progress') s.inProgress++
    if (item.needsReprocess) s.needsReprocess++
    if (item.testReturn && item.status !== 'dev_done' && item.status !== 'test_pass') s.returned++
  }
  return s
}
