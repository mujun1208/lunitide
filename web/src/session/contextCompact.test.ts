import { expect, it } from 'vitest'
import { compactPreviewDescription, compactUsageLabel, contextNeedsCompact, contextUsagePercent } from './contextCompact'

it('treats 70% usage as the compact chip threshold', () => {
  expect(contextNeedsCompact(undefined)).toBe(false)
  expect(contextNeedsCompact(0.69)).toBe(false)
  expect(contextNeedsCompact(0.7)).toBe(true)
  expect(contextNeedsCompact(1)).toBe(true)
})

it('clamps and rounds the always-on usage percent', () => {
  expect(contextUsagePercent(0)).toBe(0)
  expect(contextUsagePercent(0.024)).toBe(2)
  expect(contextUsagePercent(0.695)).toBe(70)
  expect(contextUsagePercent(1)).toBe(100)
  expect(contextUsagePercent(1.4)).toBe(100)
  expect(contextUsagePercent(-0.2)).toBe(0)
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
