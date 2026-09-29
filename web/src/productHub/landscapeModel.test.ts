import { expect, it } from 'vitest'
import { compareLandscape, landscapeAxisLabel, parseCompetitorInput, type LandscapeDraft } from './landscapeModel'

it('parses competitor names and compares against Lunitide on four axes', () => {
  expect(parseCompetitorInput('Cursor, Copilot，未知助手')).toEqual(['Cursor', 'Copilot', '未知助手'])
  const rows = compareLandscape(['Cursor', '未知助手'])
  expect(rows[0].name).toContain('Lunitide')
  expect(rows[0].cells.hub.score).toBe('strong')
  expect(rows.some(row => row.name === 'Cursor' && row.cells.assets.score === 'strong')).toBe(true)
  expect(rows.some(row => row.name === '未知助手' && row.cells.local.score === 'unknown')).toBe(true)
})

it('labels the four axes and shapes collected drafts', () => {
  expect(landscapeAxisLabel('local', true)).toBe('本机优先')
  expect(landscapeAxisLabel('media', false)).toBe('Media verify')
  expect(landscapeAxisLabel('hub', true)).toBe('知识自描述')
  expect(landscapeAxisLabel('bogus', true)).toBe('bogus')
  const draft: LandscapeDraft = {
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    name: 'Cursor',
    axis: 'assets',
    quote: 'Rules, MCP and skills are the public path.',
    url: 'https://cursor.com/changelog',
    date: '2026-09-29',
    status: 'draft',
  }
  expect(draft.status).toBe('draft')
  expect(landscapeAxisLabel(draft.axis, true)).toBe('技能 / MCP')
})
