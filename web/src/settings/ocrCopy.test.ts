import { expect, it } from 'vitest'
import { windowsProbeCanRepair, windowsProbeHint } from './ocrCopy'

it('offers one-click repair for recoverable Windows OCR states', () => {
  expect(windowsProbeCanRepair('sample_failed')).toBe(true)
  expect(windowsProbeCanRepair('language_unavailable')).toBe(true)
  expect(windowsProbeCanRepair('initialization_failed')).toBe(true)
  expect(windowsProbeCanRepair('timed_out')).toBe(true)
  expect(windowsProbeCanRepair('unsupported_os')).toBe(false)
  expect(windowsProbeCanRepair('ready')).toBe(false)
  expect(windowsProbeHint('sample_failed')).toMatch(/自检读图失败/)
})
