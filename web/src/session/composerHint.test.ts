import { describe, expect, it } from 'vitest'
import { MESSAGE_MAX_CHARACTERS } from './messageLimits'
import { composerSizeHint, compactCount, operationCompactSummary, tokenUsageCompactLine } from './composerHint'

describe('composerHint', () => {
  it('abbreviates large token counts and keeps small counts exact', () => {
    expect(compactCount(12930)).toBe('1.3万')
    expect(compactCount(8704)).toBe('8704')
    expect(compactCount(21634)).toBe('2.2万')
    expect(compactCount(40)).toBe('40')
    expect(tokenUsageCompactLine({ inputTokens: 12930, outputTokens: 8704 })).toBe('1.3万/8704')
  })

  it('keeps only the urgent tool plus a remainder count', () => {
    const label = (state: string) => ({ running: '执行中', failed: '失败', succeeded: '已完成' }[state] ?? state)
    expect(operationCompactSummary([
      { toolName: 'office.generate', state: 'failed' },
      { toolName: 'skill.try', state: 'succeeded' },
      { toolName: 'media.play', state: 'running' },
    ], label)).toBe('media.play 执行中 · +2')
    expect(operationCompactSummary([{ toolName: 'skill.try', state: 'succeeded' }], label)).toBe('skill.try 已完成')
  })

  it('hides ordinary size text and only surfaces limit pressure or a reference mark', () => {
    expect(composerSizeHint({
      characters: 12, bytes: 36, outgoingCharacters: 12, outgoingBytes: 36, zh: true,
    })).toBe('')
    expect(composerSizeHint({
      characters: 12, bytes: 36, outgoingCharacters: 80, outgoingBytes: 160, zh: true,
    })).toBe('+引用')
    expect(composerSizeHint({
      characters: Math.ceil(MESSAGE_MAX_CHARACTERS * 0.85),
      bytes: 100,
      outgoingCharacters: Math.ceil(MESSAGE_MAX_CHARACTERS * 0.85),
      outgoingBytes: 100,
      zh: true,
    })).toMatch(/字$/)
  })
})
