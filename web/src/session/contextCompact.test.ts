import { expect, it } from 'vitest'
import { compactPreviewDescription, compactUsageLabel, contextNeedsCompact } from './contextCompact'

it('treats 70% usage as the compact chip threshold', () => {
  expect(contextNeedsCompact(undefined)).toBe(false)
  expect(contextNeedsCompact(0.69)).toBe(false)
  expect(contextNeedsCompact(0.7)).toBe(true)
  expect(contextNeedsCompact(1)).toBe(true)
})

it('labels the compact chip with a rounded percent', () => {
  expect(compactUsageLabel(0.82, true)).toBe('压缩 82%')
  expect(compactUsageLabel(0.82, false)).toBe('Compact 82%')
})

it('prefers the human summary for the compact confirm copy', () => {
  expect(compactPreviewDescription('keep this', 'raw json')).toBe('keep this')
  expect(compactPreviewDescription('  ', 'raw')).toBe('raw')
  expect(compactPreviewDescription()).toMatch(/摘要/)
})
