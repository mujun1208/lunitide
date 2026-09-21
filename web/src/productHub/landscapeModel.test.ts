import { expect, it } from 'vitest'
import { compareLandscape, parseCompetitorInput } from './landscapeModel'

it('parses competitor names and compares against Lunitide on four axes', () => {
  expect(parseCompetitorInput('Cursor, Copilot，未知助手')).toEqual(['Cursor', 'Copilot', '未知助手'])
  const rows = compareLandscape(['Cursor', '未知助手'])
  expect(rows[0].name).toContain('Lunitide')
  expect(rows[0].cells.hub.score).toBe('strong')
  expect(rows.some(row => row.name === 'Cursor' && row.cells.assets.score === 'strong')).toBe(true)
  expect(rows.some(row => row.name === '未知助手' && row.cells.local.score === 'unknown')).toBe(true)
})
