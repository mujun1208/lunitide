import { expect, it } from 'vitest'
import { formatMroContextStrip, parseMroContext } from './mroContext'

it('parses a legal tail and date', () => {
  const ctx = parseMroContext({ tailNo: 'B-0000', asOf: '2026-09-03' })
  expect(ctx).toEqual({
    tailNo: 'B-0000',
    asOf: '2026-09-03',
    manualIds: [],
    pack: 'mro.v1',
    scenario: 'manual',
  })
})

it('rejects a missing date', () => {
  expect(parseMroContext({ tailNo: 'B-0000' })).toBeUndefined()
  expect(parseMroContext({ tailNo: 'B-0000', asOf: '' })).toBeUndefined()
  expect(parseMroContext({ tailNo: 'B-0000', asOf: '09-03' })).toBeUndefined()
})

it('formats the session micro-strip', () => {
  const ctx = parseMroContext({ tailNo: 'B-0000', asOf: '2026-09-03' })!
  expect(formatMroContextStrip(ctx, true)).toBe('机务 · B-0000 · 2026-09-03 · 本轮：手册问答')
})
