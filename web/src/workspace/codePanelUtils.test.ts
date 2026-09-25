import { describe, expect, it } from 'vitest'
import { acceptSourceLine, codeStatusLabel, latestFileDiff, suggestSourceLine } from './codePanelUtils'

describe('code panel status', () => {
  it('does not invent a clean problems count', () => {
    expect(codeStatusLabel(null)).toBe('未检查')
    expect(codeStatusLabel(2)).toBe('2 问题')
    expect(codeStatusLabel(0)).toBe('✓ 0 问题')
  })
})

describe('single-line completion', () => {
  it('completes one unique name and writes that line', () => {
    const src = 'package p\n\nfunc Total(count int) int {\n\treturn cou\n}\n'
    expect(suggestSourceLine(src, 3)).toBe('\treturn count')
    expect(acceptSourceLine(src, 3, '\treturn count')).toContain('\treturn count\n')
  })

  it('does not guess when two names share the prefix', () => {
    const src = 'package p\nfunc F(count, cost int) int {\n\treturn c\n}\n'
    expect(suggestSourceLine(src, 2)).toBe('')
  })

  it('shows both file diffs from an edit result', () => {
    const text = 'edited 2 files\n--- a.txt\n+++ a.txt\n-alpha\n+ALPHA\n--- b.txt\n+++ b.txt\n-beta\n+BETA\n'
    expect(latestFileDiff(['plain', text])).toContain('--- a.txt')
    expect(latestFileDiff(['plain', text])).toContain('--- b.txt')
    expect(latestFileDiff(['plain'])).toBe('')
  })

  it('leaves a finished name alone', () => {
    const src = 'package p\n\nfunc Total(count int) int {\n\treturn cou\n}\n'
    expect(suggestSourceLine(src, 0)).toBe('')
  })
})
