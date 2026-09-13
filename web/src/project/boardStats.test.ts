import { expect, it } from 'vitest'
import { formatBoardStats, summarizeBoardItems } from './boardStats'

it('uses the locked factory stats copy', () => {
  const stats = summarizeBoardItems([
    { status: 'dev_done', changeKind: 'unchanged' },
    { status: 'pending', changeKind: 'added', needsReprocess: true },
    { status: 'in_progress', changeKind: 'modified', needsReprocess: true },
    { status: 'pending', changeKind: 'removed', needsReprocess: true },
  ])
  expect(formatBoardStats(stats)).toBe('共 3 条 · 新增 1 · 变更 1 · 未改 1 · 已做 1 · 未做 2 · 待再处理 2')
})
